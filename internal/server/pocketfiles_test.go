package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/abjelosevic88/pocket-homelab-office/internal/config"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// fakeFiles is a Pocket Core that mints pcx_ tokens and a Pocket Files that
// only takes the newest token of a person as the WebDAV password.
type fakeFiles struct {
	mu        sync.Mutex
	expiresIn int
	refuse    string            // a LinkError reason Core answers with
	issued    map[string]string // token → email
	minted    map[string]int    // email → tokens minted
	seen      []string          // email of every WebDAV request
	core, dav *httptest.Server
}

func newFakeFiles(t *testing.T) *fakeFiles {
	f := &fakeFiles{expiresIn: 300, issued: map[string]string{}, minted: map[string]int{}}
	f.dav = httptest.NewServer(http.HandlerFunc(f.serveDAV))
	t.Cleanup(f.dav.Close)
	f.core = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/links/token" {
			t.Errorf("unexpected Core call: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var in struct {
			User   string   `json:"user"`
			App    string   `json:"app"`
			Scopes []string `json:"scopes"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.App != "files" || strings.Join(in.Scopes, " ") != "files:read files:write" {
			t.Errorf("wrong link asked for: %+v", in)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.refuse != "" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "link is off", "reason": f.refuse})
			return
		}
		f.minted[in.User]++
		tok := fmt.Sprintf("pcx_%s_%d", strings.Split(in.User, "@")[0], f.minted[in.User])
		f.issued[tok] = in.User
		json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_in": f.expiresIn, "scope": "files:read files:write", "app": "files", "url": f.dav.URL, "public_url": "https://files.test"})
	}))
	t.Cleanup(f.core.Close)
	return f
}

func (f *fakeFiles) serveDAV(w http.ResponseWriter, r *http.Request) {
	_, pass, ok := r.BasicAuth()
	f.mu.Lock()
	email := f.issued[pass]
	newest := ok && email != "" && pass == fmt.Sprintf("pcx_%s_%d", strings.Split(email, "@")[0], f.minted[email])
	if newest {
		f.seen = append(f.seen, email)
	}
	f.mu.Unlock()
	if !newest {
		http.Error(w, "token unknown or not the newest", http.StatusUnauthorized)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/dav/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case "PROPFIND":
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		file := `<d:response><d:href>/dav/Report.docx</d:href><d:propstat><d:prop><d:getetag>"7-1"</d:getetag><d:getcontentlength>7</d:getcontentlength><d:resourcetype/></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`
		if r.URL.Path == "/dav/Report.docx" {
			fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`+file+`</d:multistatus>`)
			return
		}
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+file+`</d:multistatus>`)
	case http.MethodPut:
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "not here", http.StatusMethodNotAllowed)
	}
}

func (f *fakeFiles) count(email string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.minted[email]
}

func filesTestServer(t *testing.T, coreURL string) (*Server, *store.Store) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "office.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{DataDir: dir, FilesDir: filepath.Join(dir, "files"), AuthMode: "none", BaseURL: "https://office.test", CoreURL: coreURL}
	return New(cfg, st, log, fstest.MapFS{"index.html": {Data: []byte("test")}}), st
}

func filesPlace(t *testing.T, s *Server, st *store.Store, email, folder string) *store.Source {
	t.Helper()
	ctx := context.Background()
	u := &store.User{Email: email, Name: email, Role: "user"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	src := &store.Source{UserID: u.ID}
	if err := s.applySourceInput(src, sourceInput{Kind: "files", Folder: folder, URL: "https://elsewhere.test", Secret: "typed"}, true); err != nil {
		t.Fatal(err)
	}
	if src.Kind != "files" || !src.ViaCore || src.URL != "" || src.Secret != "" {
		t.Fatalf("a Pocket Files place keeps nothing typed in: %+v", src)
	}
	if err := st.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	return src
}

// The place asks Core for a token for its owner, sends it as the WebDAV
// password, reuses it while it is young and asks again when it is not.
func TestPocketFilesPlace(t *testing.T) {
	ctx := context.Background()
	f := newFakeFiles(t)
	s, st := filesTestServer(t, f.core.URL)
	alice := filesPlace(t, s, st, "alice@example.com", "")
	bob := filesPlace(t, s, st, "bob@example.com", "")

	b, err := s.backendFor(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if b.Kind() != "files" || b.Describe() != "Pocket Files" {
		t.Fatalf("kind %q, describe %q", b.Kind(), b.Describe())
	}
	es, err := b.ListDir(ctx, "")
	if err != nil || len(es) != 1 || es[0].Path != "Report.docx" || es[0].ETag != "7-1" {
		t.Fatalf("list: %v %+v", err, es)
	}
	if _, err := b.Write(ctx, "Report.docx", []byte("changed"), "7-1"); err != nil {
		t.Fatal(err)
	}
	if f.count("alice@example.com") != 1 || f.count("bob@example.com") != 0 {
		t.Fatalf("a young token must be reused: minted %d", f.count("alice@example.com"))
	}

	// Tokens with less than a minute left are not reused: every request gets
	// a new one (one more was asked for when the place was connected).
	f.mu.Lock()
	f.expiresIn = 30
	f.mu.Unlock()
	b, err = s.backendFor(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := b.Stat(ctx, "Report.docx"); err != nil {
			t.Fatal(err)
		}
	}
	if f.count("bob@example.com") != 4 {
		t.Fatalf("each request must ask Core again: minted %d", f.count("bob@example.com"))
	}
	f.mu.Lock()
	seen := append([]string(nil), f.seen...)
	f.mu.Unlock()
	for _, who := range seen[len(seen)-3:] {
		if who != "bob@example.com" {
			t.Fatalf("Bob's place acted as %s", who)
		}
	}

	// Every person's home has the same address, so the person keeps documents apart.
	ab, _ := s.backendFor(ctx, alice)
	if ab.Canonical("Report.docx") == b.Canonical("Report.docx") {
		t.Fatal("two people's files must not be one document")
	}
	sub, err := storage.NewPocketFiles(f.dav.URL+"/dav", "alice@example.com", "Work", nil)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Canonical("Report.docx") != ab.Canonical("Work/Report.docx") {
		t.Fatalf("one file through two of a person's places is one document: %q", sub.Canonical("Report.docx"))
	}
}

func TestPocketFilesLinkOff(t *testing.T) {
	f := newFakeFiles(t)
	s, st := filesTestServer(t, f.core.URL)
	src := filesPlace(t, s, st, "alice@example.com", "Office")
	f.mu.Lock()
	f.refuse = "off_person"
	f.mu.Unlock()
	_, err := s.backendFor(context.Background(), src)
	var nc errNotConnected
	if !errors.As(err, &nc) || !strings.Contains(nc.Error(), "Office → Pocket Files link off") {
		t.Fatalf("want the person's switch named, got %v", err)
	}
}

func TestPocketFilesNeedsCore(t *testing.T) {
	s, _ := filesTestServer(t, "")
	if err := s.applySourceInput(&store.Source{}, sourceInput{Kind: "files"}, true); err == nil {
		t.Fatal("without Pocket Core there is no Pocket Files place")
	}
	if _, err := s.backendFor(context.Background(), &store.Source{Kind: "files", ViaCore: true}); err == nil {
		t.Fatal("without Pocket Core a Pocket Files place does not connect")
	}
}

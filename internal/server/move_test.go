package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/config"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// moveOffice is Office behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveOffice(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "files:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"office","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "office.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{DataDir: dir, FilesDir: filepath.Join(dir, "files"), AuthMode: "local", BaseURL: "https://office.test", CoreURL: coreSrv.URL, MaxUploadMB: 10}
	s := New(cfg, st, log, fstest.MapFS{"index.html": {Data: []byte("test")}})
	for _, who := range []string{"alice", "bob"} {
		if err := st.CreateUser(context.Background(), &store.User{Email: who + "@example.com", Name: who, Role: "member"}); err != nil {
			t.Fatal(err)
		}
	}
	return s, s.Router()
}

func myFilesOf(t *testing.T, s *Server, email string) storage.Backend {
	t.Helper()
	u, err := s.store.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.myFiles(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func officeCall(h http.Handler, tok, method, path, ctype string, body []byte) (int, []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func zipNames(t *testing.T, b []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return out
}

func TestMoveLinkScopes(t *testing.T) {
	_, h := moveOffice(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.zip", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/sources", 403},
		{exp, "GET", "/api/v1/users", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.zip", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/sources", 403},
		{imp, "POST", "/api/v1/drafts", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.zip", 403},
	} {
		if code, body := officeCall(h, c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := officeCall(h, exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 1 || len(p.Imports) != 1 || p.Imports[0].Form != "file" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's My files to Bob: the zip adds once, a different file of
// the same name comes in beside Bob's, and nothing crosses between them.
func TestMoveMyFilesAddOnce(t *testing.T) {
	s, h := moveOffice(t)
	ctx := context.Background()
	alice, bob := myFilesOf(t, s, "alice@example.com"), myFilesOf(t, s, "bob@example.com")
	_, _ = alice.Create(ctx, "Report.docx", []byte("alice's report"))
	_, _ = alice.Create(ctx, "Budget/2026.xlsx", []byte("alice's budget"))
	_, _ = bob.Create(ctx, "Report.docx", []byte("bob's own report"))

	code, export := officeCall(h, "pcx_alice_export", "GET", "/api/v1/export.zip", "", nil)
	if code != 200 || strings.Join(zipNames(t, export), ",") != "Budget/2026.xlsx,Report.docx" {
		t.Fatalf("alice's export: %d %v", code, zipNames(t, export))
	}
	if _, b := officeCall(h, "pcx_bob_export", "GET", "/api/v1/export.zip", "", nil); strings.Join(zipNames(t, b), ",") != "Report.docx" {
		t.Fatalf("bob's export: %v", zipNames(t, b))
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "my-files.zip")
	_, _ = fw.Write(export)
	_ = mw.Close()
	for run, want := range []map[string]int{
		{"imported": 2, "skipped": 0, "renamed": 1, "failed": 0},
		{"imported": 0, "skipped": 2, "renamed": 0, "failed": 0},
	} {
		code, body := officeCall(h, "pcx_bob_import", "POST", "/api/v1/import", mw.FormDataContentType(), buf.Bytes())
		var got map[string]int
		_ = json.Unmarshal(body, &got)
		if code != 200 || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("import run %d: %d %s", run+1, code, body)
		}
	}
	if b, _, _ := bob.Read(ctx, "Report.docx"); string(b) != "bob's own report" {
		t.Fatalf("bob's report was replaced: %q", b)
	}
	if b, _, _ := bob.Read(ctx, "Report (2).docx"); string(b) != "alice's report" {
		t.Fatalf("alice's report beside bob's: %q", b)
	}
	if b, _, _ := bob.Read(ctx, "Budget/2026.xlsx"); string(b) != "alice's budget" {
		t.Fatalf("budget: %q", b)
	}
	if _, b := officeCall(h, "pcx_alice_export", "GET", "/api/v1/export.zip", "", nil); len(zipNames(t, b)) != 2 {
		t.Fatalf("alice's files changed: %v", zipNames(t, b))
	}
}

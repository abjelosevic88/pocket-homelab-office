package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/config"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// capOffice is Office behind a fake Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|open> and remembers the
// manifest Office registers with. Alice and Bob each have an API token.
type capOffice struct {
	s        *Server
	h        http.Handler
	tokens   map[string]string // email → po_ token
	users    map[string]int64  // email → id
	mu       sync.Mutex
	manifest map[string]any
	calls    []string // every path Core was asked for
}

func newCapOffice(t *testing.T) *capOffice {
	t.Helper()
	o := &capOffice{tokens: map[string]string{}, users: map[string]int64{}}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "files:read", "open": "office:open"}
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		o.mu.Lock()
		o.calls = append(o.calls, r.URL.Path)
		o.mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			o.mu.Lock()
			o.manifest = m
			o.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			email := parts[1] + "@example.com"
			fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"office","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":9,"slug":"search","name":"Pocket Search"}}`, scopes[parts[2]], "sub-"+parts[1], email, parts[1])
		default:
			io.WriteString(w, `{}`)
		}
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
	o.s = New(cfg, st, log, fstest.MapFS{"index.html": {Data: []byte("test")}})
	o.h = o.s.Router()
	for _, who := range []string{"alice", "bob"} {
		u := &store.User{Email: who + "@example.com", Name: who, Role: "member"}
		if err := st.CreateUser(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		tok, _, err := st.CreateAPIToken(context.Background(), u.ID, "test")
		if err != nil {
			t.Fatal(err)
		}
		o.tokens[u.Email], o.users[u.Email] = tok, u.ID
	}
	return o
}

// file puts a document into a person's My files, last changed at when, and
// returns the id of My files.
func (o *capOffice) file(t *testing.T, email, p string, when time.Time) int64 {
	t.Helper()
	uid := o.users[email]
	src, err := o.s.store.BuiltinSource(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	b, err := o.s.myFiles(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create(context.Background(), p, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(o.s.cfg.FilesDir, fmt.Sprint(uid), filepath.FromSlash(p)), when, when); err != nil {
		t.Fatal(err)
	}
	return src.ID
}

// opened records that a person opened a file at when.
func (o *capOffice) opened(t *testing.T, email string, sourceID int64, p string, when time.Time) {
	t.Helper()
	ctx, uid := context.Background(), o.users[email]
	if err := o.s.store.Touch(ctx, uid, sourceID, p); err != nil {
		t.Fatal(err)
	}
	if _, err := o.s.store.DB.ExecContext(ctx, `UPDATE recent SET opened_at = ? WHERE user_id = ? AND source_id = ? AND path = ?`,
		when.UTC().Format("2006-01-02T15:04:05.000Z"), uid, sourceID, p); err != nil {
		t.Fatal(err)
	}
}

// get calls the app with a token (a person's po_ token or a pcx_ link token).
func (o *capOffice) get(t *testing.T, tok, path string) (int, map[string]any) {
	t.Helper()
	code, body := officeCall(o.h, tok, "GET", path, "", nil)
	var out map[string]any
	_ = json.Unmarshal(body, &out)
	return code, out
}

// rows are the items of a capability answer.
func rows(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

func titles(list []map[string]any) string {
	var out []string
	for _, it := range list {
		out = append(out, it["title"].(string))
	}
	return strings.Join(out, " | ")
}

func (o *capOffice) search(t *testing.T, tok, q string) []map[string]any {
	t.Helper()
	code, out := o.get(t, tok, core.SearchPath+"?q="+url.QueryEscape(q))
	if code != 200 {
		t.Fatalf("search %q: %d %v", q, code, out)
	}
	return rows(t, out)
}

// Search finds documents by name: the ones opened lately first, then the rest
// of the places newest first, each once, with where it is and when it changed.
func TestPocketSearch(t *testing.T) {
	o := newCapOffice(t)
	alice, bob := o.tokens["alice@example.com"], o.tokens["bob@example.com"]
	now := time.Now()
	my := o.file(t, "alice@example.com", "Money/Budget 2026.xlsx", now.Add(-time.Hour))
	o.file(t, "alice@example.com", "Budget notes.docx", now.Add(-72*time.Hour))
	o.file(t, "alice@example.com", "Talks/Budget talk.pptx", now.Add(-48*time.Hour))
	o.file(t, "alice@example.com", "Budget backup.zip", now) // Office does not open it
	o.file(t, "alice@example.com", "Holiday.docx", now)
	o.opened(t, "alice@example.com", my, "Budget notes.docx", now.Add(-10*time.Minute))
	bobs := o.file(t, "bob@example.com", "Budget of Bob.docx", now)
	o.opened(t, "bob@example.com", bobs, "Budget of Bob.docx", now)

	got := o.search(t, alice, "budget")
	if titles(got) != "Budget notes.docx | Budget 2026.xlsx | Budget talk.pptx" {
		t.Fatalf("order: %s", titles(got))
	}
	want := []struct{ id, kind, url, snippet string }{
		{fmt.Sprintf("%d:Budget notes.docx", my), "document", fmt.Sprintf("/edit/%d?path=Budget+notes.docx", my), "My files · opened today"},
		{fmt.Sprintf("%d:Money/Budget 2026.xlsx", my), "spreadsheet", fmt.Sprintf("/edit/%d?path=Money%%2FBudget+2026.xlsx", my), "My files · Money · edited today"},
		{fmt.Sprintf("%d:Talks/Budget talk.pptx", my), "presentation", fmt.Sprintf("/edit/%d?path=Talks%%2FBudget+talk.pptx", my), "My files · Talks · edited " + dayWord(now.Add(-48*time.Hour), now)},
	}
	for i, w := range want {
		it := got[i]
		if it["id"] != w.id || it["kind"] != w.kind || it["url"] != w.url || it["snippet"] != w.snippet || it["date"] == "" {
			t.Errorf("item %d: %v, want %+v", i, it, w)
		}
	}

	// the limit, a single character and words without a letter or digit
	if code, out := o.get(t, alice, core.SearchPath+"?q=budget&limit=1"); code != 200 || titles(rows(t, out)) != "Budget notes.docx" {
		t.Fatalf("limit 1: %d %v", code, out)
	}
	for _, q := range []string{"b", "??", "**", ".."} {
		if got := o.search(t, alice, q); len(got) != 0 {
			t.Errorf("%q found %s", q, titles(got))
		}
	}
	// another person's documents never appear
	if got := titles(o.search(t, bob, "budget")); got != "Budget of Bob.docx" {
		t.Fatalf("bob: %s", got)
	}
	if got := titles(o.search(t, alice, "bob")); got != "" {
		t.Fatalf("alice found bob's: %s", got)
	}
}

// A place that does not answer leaves the rest of the answer as it is, and a
// slow one does not hold Pocket Search up past the walk's time.
func TestPocketSearchPlaces(t *testing.T) {
	o := newCapOffice(t)
	alice := o.tokens["alice@example.com"]
	o.file(t, "alice@example.com", "Plan.docx", time.Now())

	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	done := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(done) })
	for _, addr := range []string{dead.URL, slow.URL} {
		if err := o.s.store.CreateSource(context.Background(), &store.Source{UserID: o.users["alice@example.com"], Kind: "webdav", Name: "Away", URL: addr, Username: "a", Secret: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	old := pocketSearchWalk
	pocketSearchWalk = 300 * time.Millisecond
	t.Cleanup(func() { pocketSearchWalk = old })

	start := time.Now()
	got := o.search(t, alice, "plan")
	if titles(got) != "Plan.docx" {
		t.Fatalf("got %s", titles(got))
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the search waited %s for a slow place", d)
	}
}

// Pocket Documents and Pocket Files places are left to those apps: the search
// never reads them and leaves out the recent files in them, while a WebDAV
// place Office reaches itself is searched. GET /search still reads them all.
func TestPocketSearchSkipsOtherApps(t *testing.T) {
	o := newCapOffice(t)
	ctx, uid, alice := context.Background(), o.users["alice@example.com"], o.tokens["alice@example.com"]
	now := time.Now()
	my := o.file(t, "alice@example.com", "Plan.docx", now)

	var mu sync.Mutex
	docsAsked := 0
	docs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		docsAsked++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"count":1,"results":[{"id":4,"title":"Plan in Documents","original_name":"plan.docx","modified_at":%q}]}`, now.UTC().Format(time.RFC3339))
	}))
	t.Cleanup(docs.Close)
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			http.Error(w, "not here", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:"><d:response><d:href>/dav/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`+
			`<d:response><d:href>/dav/Plan%20on%20WebDAV.docx</d:href><d:propstat><d:prop><d:getetag>"1"</d:getetag><d:getcontentlength>1</d:getcontentlength><d:resourcetype/></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response></d:multistatus>`)
	}))
	t.Cleanup(dav.Close)
	place := func(src *store.Source) int64 {
		src.UserID = uid
		if err := o.s.store.CreateSource(ctx, src); err != nil {
			t.Fatal(err)
		}
		return src.ID
	}
	docsID := place(&store.Source{Kind: "documents", Name: "Archive", URL: docs.URL, Secret: "pd_token"})
	filesID := place(&store.Source{Kind: "files", Name: "Pocket Files", ViaCore: true})
	place(&store.Source{Kind: "webdav", Name: "Server", URL: dav.URL + "/dav", Username: "a", Secret: "b"})
	o.opened(t, "alice@example.com", my, "Plan.docx", now)
	o.opened(t, "alice@example.com", docsID, "4/Plan in Documents.docx", now)
	o.opened(t, "alice@example.com", filesID, "Work/Plan in Files.docx", now)
	o.mu.Lock()
	o.calls = nil
	o.mu.Unlock()

	got := o.search(t, alice, "plan")
	if titles(got) != "Plan.docx | Plan on WebDAV.docx" {
		t.Fatalf("search: %s", titles(got))
	}
	mu.Lock()
	asked := docsAsked
	mu.Unlock()
	o.mu.Lock()
	calls := strings.Join(o.calls, " ")
	o.mu.Unlock()
	if asked != 0 || strings.Contains(calls, "/api/v1/links/token") {
		t.Fatalf("the search reached into Pocket Documents (%d requests) or Pocket Files (Core calls: %s)", asked, calls)
	}

	// GET /search is as it was: every place, Pocket Documents included
	code, body := officeCall(o.h, alice, "GET", "/api/v1/search?q=plan", "", nil)
	if code != 200 || !strings.Contains(string(body), "Plan in Documents.docx") || !strings.Contains(string(body), "Plan on WebDAV.docx") {
		t.Fatalf("GET /search: %d %s", code, body)
	}
}

// The widget is the documents opened last, newest first, with the day they
// were opened; nothing at all for someone who opened nothing.
func TestPocketWidget(t *testing.T) {
	o := newCapOffice(t)
	alice, bob := o.tokens["alice@example.com"], o.tokens["bob@example.com"]
	now := time.Now()
	var my int64
	for i := range 7 {
		p := fmt.Sprintf("Doc %d.docx", i)
		my = o.file(t, "alice@example.com", p, now)
		o.opened(t, "alice@example.com", my, p, now.Add(-time.Duration(i)*24*time.Hour))
	}
	code, out := o.get(t, alice, core.WidgetPath)
	if code != 200 {
		t.Fatalf("widget: %d %v", code, out)
	}
	items := rows(t, out)
	if titles(items) != "Doc 0.docx | Doc 1.docx | Doc 2.docx | Doc 3.docx | Doc 4.docx" {
		t.Fatalf("rows: %s", titles(items))
	}
	if out["line"] != nil || out["metrics"] != nil || out["url"] != "/" {
		t.Fatalf("line %v, metrics %v, url %v", out["line"], out["metrics"], out["url"])
	}
	first, second := items[0], items[1]
	if first["detail"] != "today" || second["detail"] != "yesterday" || first["url"] != fmt.Sprintf("/edit/%d?path=Doc+0.docx", my) || first["id"] != fmt.Sprintf("%d:Doc 0.docx", my) {
		t.Fatalf("first rows: %v %v", first, second)
	}
	if code, out := o.get(t, bob, core.WidgetPath); code != 200 || len(rows(t, out)) != 0 || out["line"] != nil {
		t.Fatalf("bob: %d %v", code, out)
	}
}

func TestDayWord(t *testing.T) {
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	for when, want := range map[time.Time]string{
		now.Add(-time.Hour):                            "today",
		time.Date(2026, 11, 26, 23, 0, 0, 0, time.UTC): "yesterday",
		time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC): "Monday",
		time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC):  "2 Nov",
		time.Date(2025, 11, 2, 12, 0, 0, 0, time.UTC):  "2 Nov 2025",
	} {
		if got := dayWord(when, now); got != want {
			t.Errorf("%s: %q, want %q", when, got, want)
		}
	}
}

// A link token reaches its one route (and /auth/me); the old scopes do not
// open the new routes, and what Office does not declare is not there.
func TestPocketLinkTokens(t *testing.T) {
	o := newCapOffice(t)
	now := time.Now()
	my := o.file(t, "alice@example.com", "Plan.docx", now)
	o.opened(t, "alice@example.com", my, "Plan.docx", now)
	bobs := o.file(t, "bob@example.com", "Plan of Bob.docx", now)
	o.opened(t, "bob@example.com", bobs, "Plan of Bob.docx", now)
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{"pcx_alice_search", "GET", core.SearchPath + "?q=plan", 200},
		{"pcx_alice_widget", "GET", core.WidgetPath, 200},
		{"pcx_alice_search", "GET", "/api/v1/auth/me", 200},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", 200},
		// the other capability's route
		{"pcx_alice_search", "GET", core.WidgetPath, 403},
		{"pcx_alice_widget", "GET", core.SearchPath + "?q=plan", 403},
		// what Office does not declare is not there, and a token for it opens nothing else
		{"pcx_alice_day", "GET", core.DayPath + "2026-11-27", 404},
		{"pcx_alice_day", "GET", core.SearchPath + "?q=plan", 403},
		{"pcx_alice_capture", "POST", core.CapturePath, 404},
		{"pcx_alice_capture", "GET", core.WidgetPath, 403},
		// the rest of the app
		{"pcx_alice_search", "GET", "/api/v1/search?q=plan", 403},
		{"pcx_alice_search", "GET", "/api/v1/recent", 403},
		{"pcx_alice_search", "GET", "/api/v1/sources", 403},
		{"pcx_alice_search", "GET", fmt.Sprintf("/api/v1/sources/%d/file?path=Plan.docx", my), 403},
		{"pcx_alice_search", "POST", fmt.Sprintf("/api/v1/sources/%d/open?path=Plan.docx", my), 403},
		{"pcx_alice_search", "POST", core.SearchPath + "?q=plan", 405},
		{"pcx_alice_widget", "GET", "/api/v1/dashboard", 403},
		{"pcx_alice_widget", "GET", "/api/v1/settings", 403},
		{"pcx_alice_widget", "GET", "/api/v1/export.zip", 403},
		// the old scopes keep what they had and get nothing new
		{"pcx_alice_read", "GET", "/api/v1/search?q=plan", 200},
		{"pcx_alice_read", "GET", core.SearchPath + "?q=plan", 403},
		{"pcx_alice_read", "GET", core.WidgetPath, 403},
		{"pcx_alice_open", "GET", core.SearchPath + "?q=plan", 403},
		{"pcx_alice_open", "GET", core.WidgetPath, 403},
		// not a token Core vouches for
		{"pcx_alice_nothing", "GET", core.SearchPath + "?q=plan", 401},
	} {
		if code, body := officeCall(o.h, c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s with %s: %d, want %d (%s)", c.method, c.path, c.tok, code, c.want, body)
		}
	}

	// through a link the answer is the person's own
	if got := titles(o.search(t, "pcx_alice_search", "plan")); got != "Plan.docx" {
		t.Fatalf("alice through a link: %s", got)
	}
	if got := titles(o.search(t, "pcx_bob_search", "plan")); got != "Plan of Bob.docx" {
		t.Fatalf("bob through a link: %s", got)
	}
	if _, out := o.get(t, "pcx_alice_widget", core.WidgetPath); titles(rows(t, out)) != "Plan.docx" {
		t.Fatalf("alice's widget through a link: %v", out)
	}
}

// Office tells Core its shelf, what it can be asked, and the offers that go with it.
func TestManifestCapabilities(t *testing.T) {
	o := newCapOffice(t)
	o.s.cfg.CoreRegistrationKey = "key"
	if err := o.s.registerWithCore(t.Context()); err != nil {
		t.Fatal(err)
	}
	o.mu.Lock()
	m := o.manifest
	o.mu.Unlock()
	if m["slug"] != "office" || m["category"] != "Files" {
		t.Fatalf("manifest: slug %v, category %v", m["slug"], m["category"])
	}
	j := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	search, _ := m["search"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || j(search["bangs"]) != `["office","doc","sheet"]` || j(search["kinds"]) != `["document","spreadsheet","presentation"]` || search["what"] == "" {
		t.Fatalf("search: %v", m["search"])
	}
	if widget == nil || widget["title"] != "Recent documents" || widget["what"] == "" {
		t.Fatalf("widget: %v", m["widget"])
	}
	if m["day"] != nil || m["capture"] != nil {
		t.Fatalf("declares more than it answers: day %v, capture %v", m["day"], m["capture"])
	}
	offers := map[string]bool{}
	for _, of := range m["offers"].([]any) {
		offers[of.(map[string]any)["scope"].(string)] = true
	}
	for scope, want := range map[string]bool{"files:read": true, "office:open": true, core.ScopeSearch: true, core.ScopeWidget: true, core.ScopeDay: false, core.ScopeCapture: false} {
		if offers[scope] != want {
			t.Errorf("offer %s: %v, want %v (%v)", scope, offers[scope], want, offers)
		}
	}
}

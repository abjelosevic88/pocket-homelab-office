package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/abjelosevic88/pocket-homelab-office/internal/config"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Browsers load the Document Server at BASE_URL/ds; it gets the path without /ds and is told, in
// X-Forwarded-Host, that it lives under /ds on the public https address.
func TestDocServerThroughThisApp(t *testing.T) {
	var gotPath, gotHost, gotProto string
	ds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotHost, gotProto = r.URL.Path, r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto")
		_, _ = io.WriteString(w, "api.js")
	}))
	defer ds.Close()
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(filepath.Join(dir, "office.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := &config.Config{DataDir: dir, FilesDir: filepath.Join(dir, "files"), AuthMode: "none", BaseURL: "https://office.test",
		OnlyOfficeURL: "https://office.test/ds", OnlyOfficeInternalURL: ds.URL, DSProxied: true}
	h := New(cfg, st, log, fstest.MapFS{"index.html": {Data: []byte("test")}}).Router()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/ds/web-apps/apps/api/documents/api.js", nil))
	if rec.Code != 200 || rec.Body.String() != "api.js" {
		t.Fatalf("proxied: %d %q", rec.Code, rec.Body)
	}
	if gotPath != "/web-apps/apps/api/documents/api.js" || gotHost != "office.test/ds" || gotProto != "https" {
		t.Fatalf("document server saw path %q, X-Forwarded-Host %q, proto %q", gotPath, gotHost, gotProto)
	}
	// Everything else is still this app.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "api.js") {
		t.Fatalf("healthz went to the document server: %d %q", rec.Code, rec.Body)
	}
}

package server

import (
	"context"
	"encoding/json"
	"fmt"
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

// Both engines must return the metadata used by the editor toolbar, including
// when a requested Collabora file falls back to ONLYOFFICE.
func TestOpenEditorMetadata(t *testing.T) {
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "office.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	co := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<wopi-discovery><net-zone><app name="writer"><action name="edit" ext="docx" urlsrc="https://collabora.test/browser/editor?" /></app></net-zone></wopi-discovery>`)
	}))
	t.Cleanup(co.Close)
	cfg := &config.Config{
		DataDir: dir, FilesDir: filepath.Join(dir, "files"), AuthMode: "none",
		BaseURL: "https://office.test", CallbackURL: "https://office.test",
		OnlyOfficeURL: "https://onlyoffice.test", CollaboraURL: co.URL,
		DefaultEngine: "onlyoffice",
	}
	s := New(cfg, st, log, fstest.MapFS{"index.html": {Data: []byte("test")}})
	u, err := s.firstOrSystemUser(ctx, "admin@localhost", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	src, err := st.BuiltinSource(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	backend, err := s.backendFor(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Example.docx", "Example.pdf", "Example.epub"} {
		if _, err := backend.Create(ctx, name, []byte("metadata test")); err != nil {
			t.Fatal(err)
		}
	}
	router := s.Router()
	for _, tc := range []struct {
		name, engine, want string
		fallback           bool
	}{
		{"Example.docx", "onlyoffice", "onlyoffice", false},
		{"Example.pdf", "onlyoffice", "onlyoffice", false},
		{"Example.docx", "collabora", "collabora", false},
		{"Example.epub", "collabora", "onlyoffice", true},
	} {
		t.Run(tc.name+"/"+tc.engine, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"path": tc.name, "engine": tc.engine})
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/sources/%d/open", src.ID), strings.NewReader(string(payload)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			var got struct {
				Engine         string         `json:"engine"`
				Engines        []string       `json:"engines"`
				EngineNote     string         `json:"engine_note"`
				CollaboraOpens *bool          `json:"collabora_opens"`
				Collab         map[string]any `json:"collab"`
				File           fileView       `json:"file"`
				Exports        []string       `json:"exports"`
				APIJS          string         `json:"api_js"`
				Config         map[string]any `json:"config"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Engine != tc.want {
				t.Errorf("engine = %q, want %q", got.Engine, tc.want)
			}
			if len(got.Engines) != 2 || got.Engines[0] != "onlyoffice" {
				t.Errorf("engines = %v", got.Engines)
			}
			if got.CollaboraOpens == nil || *got.CollaboraOpens != (tc.name == "Example.docx") {
				t.Error("missing or wrong Collabora support metadata")
			}
			if got.Collab == nil {
				t.Error("missing collaboration metadata")
			}
			if got.File.Name != tc.name || len(got.Exports) == 0 {
				t.Error("missing file/export metadata")
			}
			if tc.fallback && got.EngineNote == "" {
				t.Error("missing engine fallback explanation")
			}
			if tc.want == "onlyoffice" && (got.APIJS == "" || got.Config == nil) {
				t.Error("missing ONLYOFFICE configuration")
			}
		})
	}
}

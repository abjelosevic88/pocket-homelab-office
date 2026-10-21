package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
)

func TestDocumentsReadOriginal(t *testing.T) {
	for _, ext := range []string{"pdf", "docx"} {
		t.Run(ext, func(t *testing.T) {
			data := "original " + ext + " bytes"
			downloads := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing authorization")
				}
				switch r.URL.Path {
				case "/api/v1/documents/18":
					json.NewEncoder(w).Encode(map[string]any{"document": pdDocument{ID: 18, Title: "Example", OriginalName: "upload." + ext, Checksum: "original-checksum", Size: int64(len(data))}, "prev": 17, "next": 19})
				case "/api/v1/documents/18/download":
					downloads++
					if r.URL.Query().Get("original") != "true" {
						t.Error("must request the original")
					}
					io.WriteString(w, data)
				default:
					t.Errorf("unexpected endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			backend := &docsBackend{base: srv.URL, token: "test-token", http: srv.Client()}
			path := "18/Example." + ext
			stat, err := backend.Stat(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if stat.Path != path || stat.FileID != "18" || stat.ETag != "original-checksum" {
				t.Fatalf("wrong metadata: %+v", stat)
			}
			got, entry, err := backend.Read(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != data || entry.Path != path || downloads != 1 {
				t.Fatalf("wrong download: entry=%+v, downloads=%d", entry, downloads)
			}
			// The same metadata must preserve the existing outside-change guard.
			_, err = backend.Write(context.Background(), path, []byte("changed"), "stale-checksum")
			if !errors.Is(err, storage.ErrPrecondition) {
				t.Fatalf("write with stale ETag: %v", err)
			}
		})
	}
}

func TestDocumentsRejectInvalidMetadata(t *testing.T) {
	for _, body := range []string{`{}`, `{"document":{"id":0}}`, `{"document":{"id":19}}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/documents/18" {
					t.Errorf("must not download using invalid metadata: %s", r.URL.Path)
				}
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			backend := &docsBackend{base: srv.URL, http: srv.Client()}
			if _, _, err := backend.Read(context.Background(), "18/Example.pdf"); err == nil {
				t.Fatal("accepted invalid document metadata")
			}
		})
	}
}

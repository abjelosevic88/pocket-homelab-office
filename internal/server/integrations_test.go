package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Opening from Pocket Files makes the person's "Pocket Files" place once and then reuses it.
func TestFilesSourceIsMadeOnce(t *testing.T) {
	ctx := context.Background()
	f := newFakeFiles(t)
	s, st := filesTestServer(t, f.core.URL)
	u := &store.User{Email: "alice@example.com", Name: "Alice", Role: "user"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	a, err := s.filesSource(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "files" || !a.ViaCore || a.Name != "Pocket Files" {
		t.Fatalf("made %+v", a)
	}
	b, err := s.filesSource(ctx, u.ID)
	if err != nil || b.ID != a.ID {
		t.Fatalf("second open made another place: %v %+v", err, b)
	}
	list, _ := st.ListSources(ctx, u.ID)
	n := 0
	for _, l := range list {
		if l.Kind == "files" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d Pocket Files places", n)
	}
}

func TestOfficeOpenLinkReachesOnlyTheCheck(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/v1/integrations/files":   true,
		"/api/v1/sources":              false,
		"/api/v1/sources/files/ensure": false,
	} {
		method := "GET"
		if path == "/api/v1/sources/files/ensure" {
			method = "POST"
		}
		if got := linkAllows([]string{"office:open"}, httptest.NewRequest(method, path, nil)); got != want {
			t.Errorf("office:open on %s %s = %v", method, path, got)
		}
	}
}

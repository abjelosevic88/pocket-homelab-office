package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Opening from other Pocket apps. Pocket Files shows "Open in Pocket Office" only when it
// asks here (with its office:open link) and hears that this app can reach the person's
// Pocket Files; the button then loads /open/files?path=… (its WebDAV path) in a frame.

// handleFilesIntegration answers Pocket Files: can this person's documents be opened here?
func (s *Server) handleFilesIntegration(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"ready": false, "open_url": strings.TrimRight(s.cfg.BaseURL, "/") + "/open/files"}
	if !s.core.Configured() {
		out["reason"] = "Pocket Office is not connected to Pocket Core"
		writeJSON(w, http.StatusOK, out)
		return
	}
	email := s.userEmail(r.Context(), me(r))
	if email == "" {
		out["reason"] = "Pocket Office has no email for this person"
		writeJSON(w, http.StatusOK, out)
		return
	}
	if on, why := s.core.LinkOn(r.Context(), email, "files", filesScopes...); !on {
		out["reason"] = "Pocket Office's link to Pocket Files is off (" + why + "): switch it on in Pocket Core → Links"
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["ready"] = true
	writeJSON(w, http.StatusOK, out)
}

// handleEnsureFilesSource returns the person's Pocket Files place (all of it), making one
// the first time a file is opened from Pocket Files.
func (s *Server) handleEnsureFilesSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.filesSource(ctx, me(r))
	if err != nil {
		s.fail(w, err, "Pocket Files")
		return
	}
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) filesSource(ctx context.Context, uid int64) (*store.Source, error) {
	list, err := s.store.ListSources(ctx, uid)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if l := &list[i]; l.Kind == "files" && l.ViaCore && strings.Trim(l.Folder, "/") == "" {
			return l, nil
		}
	}
	src := &store.Source{UserID: uid}
	if err := s.applySourceInput(src, sourceInput{Kind: "files", Name: "Pocket Files", ViaCore: true}, true); err != nil {
		return nil, err
	}
	b, err := s.backendFor(ctx, src)
	if err != nil {
		return nil, err
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := b.Test(tctx); err != nil {
		return nil, err
	}
	if err := s.store.CreateSource(ctx, src); err != nil {
		return nil, err
	}
	go s.reportModes(context.Background())
	return src, nil
}

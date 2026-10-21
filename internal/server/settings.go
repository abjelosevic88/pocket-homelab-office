package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
)

// Box settings live in the database once set; the environment gives the
// first values. Only admins see or change the Document Server part.

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	ds := s.docServer(ctx)
	out := map[string]any{
		"title":      firstNonEmpty(s.store.GetSetting(ctx, "title"), "Pocket Office"),
		"admin":      u.IsAdmin(),
		"core":       s.core.Configured(),
		"core_url":   s.corePublicURL(),
		"base_url":   s.cfg.BaseURL,
		"max_upload": s.cfg.MaxUploadMB,
		"me":         s.store.UserSettings(ctx, u.ID),
		"document_server": map[string]any{
			"configured": ds.Configured(), "url": ds.PublicURL,
		},
	}
	if u.IsAdmin() {
		out["document_server"] = map[string]any{
			"configured": ds.Configured(), "url": ds.PublicURL, "internal_url": ds.InternalURL, "header": ds.Header,
			"secret_set": ds.Secret != "", "callback_url": s.callbackBase(ctx),
			"from_env": map[string]any{"url": s.cfg.OnlyOfficeURL, "internal_url": s.cfg.OnlyOfficeInternalURL, "callback_url": s.cfg.CallbackURL, "secret_set": s.cfg.OnlyOfficeJWTSecret != ""},
		}
		out["folder_roots"] = s.cfg.FolderRoots
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	var in struct {
		Title          *string           `json:"title"`
		Me             map[string]string `json:"me"`
		DocumentServer *struct {
			URL         *string `json:"url"`
			InternalURL *string `json:"internal_url"`
			Header      *string `json:"header"`
			Secret      *string `json:"secret"`
			CallbackURL *string `json:"callback_url"`
		} `json:"document_server"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	for k, v := range in.Me {
		if len(k) > 64 || len(v) > 4096 {
			continue
		}
		_ = s.store.SetUserSetting(ctx, u.ID, k, v)
	}
	if in.Title != nil || in.DocumentServer != nil {
		if !u.IsAdmin() {
			writeErr(w, http.StatusForbidden, "only an admin can change the box settings")
			return
		}
	}
	if in.Title != nil {
		_ = s.store.SetSetting(ctx, "title", strings.TrimSpace(*in.Title))
	}
	if d := in.DocumentServer; d != nil {
		if d.URL != nil {
			v := strings.TrimRight(strings.TrimSpace(*d.URL), "/")
			if v != "" && !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
				writeErr(w, http.StatusBadRequest, "the Document Server address must start with http:// or https://")
				return
			}
			_ = s.store.SetSetting(ctx, "ds_url", v)
		}
		if d.InternalURL != nil {
			_ = s.store.SetSetting(ctx, "ds_internal_url", strings.TrimRight(strings.TrimSpace(*d.InternalURL), "/"))
		}
		if d.Header != nil {
			_ = s.store.SetSetting(ctx, "ds_header", strings.TrimSpace(*d.Header))
		}
		if d.Secret != nil {
			_ = s.store.SetSetting(ctx, "ds_secret", *d.Secret)
		}
		if d.CallbackURL != nil {
			_ = s.store.SetSetting(ctx, "callback_url", strings.TrimRight(strings.TrimSpace(*d.CallbackURL), "/"))
		}
	}
	s.handleGetSettings(w, r)
}

// handleTestDocumentServer checks the health endpoint and that Document
// Server can reach this app (a tiny conversion round trip).
func (s *Server) handleTestDocumentServer(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	ctx := r.Context()
	ds := s.docServer(ctx)
	out := map[string]any{"ok": false, "url": ds.PublicURL, "internal_url": ds.InternalURL, "callback_url": s.callbackBase(ctx)}
	if !ds.Configured() {
		out["error"] = "no address set"
		writeJSON(w, http.StatusOK, out)
		return
	}
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	start := time.Now()
	if err := ds.Health(hctx); err != nil {
		out["error"] = "healthcheck: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["health_ms"] = time.Since(start).Milliseconds()
	// round trip: Document Server converts a blank document it fetches from here
	cb := s.callbackBase(ctx)
	if cb == "" {
		out["error"] = "set BASE_URL or CALLBACK_URL so Document Server can reach this app"
		writeJSON(w, http.StatusOK, out)
		return
	}
	blank, _ := office.Blank("docx")
	b := s.scratchBackend()
	if _, err := b.Write(ctx, "probe.docx", blank, ""); err != nil {
		out["error"] = "writing the probe: " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer func() { _ = b.Delete(context.Background(), "probe.docx") }()
	cctx, cancel2 := context.WithTimeout(ctx, 60*time.Second)
	defer cancel2()
	start = time.Now()
	data, _, err := s.convert(cctx, 0, b, "probe.docx", "pdf")
	if err != nil {
		out["error"] = "Document Server could not fetch and convert a file from " + cb + ": " + err.Error()
		writeJSON(w, http.StatusOK, out)
		return
	}
	out["ok"], out["convert_ms"], out["pdf_bytes"] = true, time.Since(start).Milliseconds(), len(data)
	writeJSON(w, http.StatusOK, out)
}

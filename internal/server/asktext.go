package server

import (
	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"net/http"
)

func (s *Server) handleAskText(w http.ResponseWriter, r *http.Request) {
	_, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	path, err := storage.Clean(r.URL.Query().Get("path"))
	if err != nil || path == "" {
		writeErr(w, 400, "Choose a file")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	meta, err := b.Stat(ctx, path)
	if err != nil {
		s.fail(w, err, "read document metadata")
		return
	}
	if meta.IsDir || meta.Size > 20<<20 {
		writeErr(w, 422, "Choose a document up to 20 MiB")
		return
	}
	data, entry, err := b.Read(ctx, path)
	if err != nil {
		s.fail(w, err, "read document")
		return
	}
	text, err := office.Text(entry.Name, data)
	if err != nil {
		writeErr(w, 422, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"name": entry.Name, "path": path, "text": text, "note": "Extracted text only; formatting, images and live formulas are not interpreted."})
}

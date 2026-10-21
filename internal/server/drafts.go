package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Drafts: a file dropped anywhere opens at once as a working copy in the
// person's Drafts place. Document Server autosaves into it like into any
// other place; "Save to…" asks Document Server for the latest state, copies
// the file into a real place and (unless asked to keep it) removes the draft.

// handleUploadDraft takes dropped files into Drafts and says where they are.
func (s *Server) handleUploadDraft(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	src, err := s.store.DraftsSource(ctx, uid)
	if err != nil {
		s.fail(w, err, "drafts")
		return
	}
	b, err := s.backendFor(ctx, src)
	if err != nil {
		s.fail(w, err, "drafts")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(s.cfg.MaxUploadMB)<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send multipart/form-data with one or more \"file\" fields")
		return
	}
	var saved []fileView
	var failed []map[string]string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "unreadable upload: "+err.Error())
			return
		}
		name := storage.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		if name == "" {
			continue
		}
		if !storage.ValidName(name) {
			failed = append(failed, map[string]string{"file": name, "error": "invalid name"})
			continue
		}
		data, err := io.ReadAll(part)
		if err != nil {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("uploads are limited to %d MB", s.cfg.MaxUploadMB))
			return
		}
		e, err := createUnique(ctx, b, name, data)
		if err != nil {
			failed = append(failed, map[string]string{"file": name, "error": humanErr(err)})
			continue
		}
		_ = s.store.Touch(ctx, uid, src.ID, e.Path)
		saved = append(saved, view(e))
	}
	if saved == nil {
		saved = []fileView{}
	}
	status := http.StatusCreated
	if len(saved) == 0 && len(failed) > 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"source": src.ID, "saved": saved, "failed": failed})
}

// createUnique writes a new file, adding " (2)", " (3)"… when the name is taken.
func createUnique(ctx context.Context, b storage.Backend, p string, data []byte) (storage.Entry, error) {
	ext := office.Ext(p)
	stem := p
	if ext != "" {
		stem = strings.TrimSuffix(p, "."+ext)
	}
	var e storage.Entry
	var err error
	for i := 1; i <= 100; i++ {
		n := p
		if i > 1 {
			n = fmt.Sprintf("%s (%d)", stem, i)
			if ext != "" {
				n += "." + ext
			}
		}
		e, err = b.Create(ctx, n, data)
		if !errors.Is(err, storage.ErrExists) {
			break
		}
	}
	return e, err
}

// handleSaveDraft puts a draft into a real place.
func (s *Server) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	var in struct {
		Path      string `json:"path"`      // the draft
		SourceID  int64  `json:"source_id"` // where to
		Dir       string `json:"dir"`
		Name      string `json:"name"`
		Keep      bool   `json:"keep"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := storage.Clean(in.Path)
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a draft is needed")
		return
	}
	drafts, err := s.store.DraftsSource(ctx, uid)
	if err != nil {
		s.fail(w, err, "drafts")
		return
	}
	db, err := s.backendFor(ctx, drafts)
	if err != nil {
		s.fail(w, err, "drafts")
		return
	}
	dest, err := s.store.GetSource(ctx, uid, in.SourceID)
	if err != nil {
		s.fail(w, err, "destination")
		return
	}
	if dest.Kind == "drafts" {
		writeErr(w, http.StatusBadRequest, "pick a place other than Drafts")
		return
	}
	tb, err := s.backendFor(ctx, dest)
	if err != nil {
		s.fail(w, err, "destination")
		return
	}
	dir, err := storage.Clean(in.Dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that folder cannot be used")
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = storage.Base(p)
	}
	if office.Ext(name) == "" && office.Ext(p) != "" {
		name += "." + office.Ext(p)
	}
	if !storage.ValidName(name) {
		writeErr(w, http.StatusBadRequest, "that name cannot be used")
		return
	}
	// Ask Document Server for the latest state of an open draft, then read it.
	s.flushOpenDocument(ctx, db.Canonical(p))
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, _, err := db.Read(sctx, p)
	if err != nil {
		s.fail(w, err, "reading the draft")
		return
	}
	target := storage.Join(dir, name)
	var e storage.Entry
	if in.Overwrite {
		e, err = tb.Write(sctx, target, data, "")
	} else {
		e, err = tb.Create(sctx, target, data)
	}
	if err != nil {
		s.fail(w, err, "saving to "+firstNonEmpty(dest.Name, defaultSourceName(dest.Kind)))
		return
	}
	s.moveReviewSource(ctx, uid, drafts, db, p, dest, tb, e.Path)
	if !in.Keep {
		s.store.DropKeysFor(ctx, drafts.ID, p)
		s.store.ForgetRecent(ctx, uid, drafts.ID, p)
		if err := db.Delete(sctx, p); err != nil {
			s.log.Warn("draft saved but not removed", "path", p, "err", err)
		}
	}
	_ = s.store.Touch(ctx, uid, dest.ID, e.Path)
	s.publish("office.created", fmt.Sprintf("file:%d:%s", dest.ID, e.Path), map[string]any{"user_email": userFrom(ctx).Email, "name": e.Name, "path": e.Path, "source": dest.Kind, "how": "draft", "url": s.fileURL(dest.ID, e.Path)})
	writeJSON(w, http.StatusCreated, map[string]any{"source": dest.ID, "file": view(e)})
}

// flushOpenDocument asks Document Server to force-save a document that is
// open right now (its command service), and waits briefly for the callback
// to land, so the bytes on disk are what the person sees.
func (s *Server) flushOpenDocument(ctx context.Context, canonical string) {
	k, err := s.store.ActiveKey(ctx, canonical)
	if err != nil {
		return
	}
	ds := s.docServer(ctx)
	if !ds.Configured() {
		return
	}
	body := map[string]any{"c": "forcesave", "key": k.Key, "userdata": "pocket-office"}
	if ds.Secret != "" {
		if tok, err := office.Sign(ds.Secret, body); err == nil {
			body["token"] = tok
		}
	}
	raw, _ := json.Marshal(body)
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodPost, strings.TrimRight(firstNonEmpty(ds.InternalURL, ds.PublicURL), "/")+"/coauthoring/CommandService.ashx", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if ds.Secret != "" {
		if hdr, err := office.Sign(ds.Secret, map[string]any{"payload": body}); err == nil {
			req.Header.Set(firstNonEmpty(ds.Header, "AuthorizationJwt"), "Bearer "+hdr)
		}
	}
	before := ""
	if k.SavedAt != nil {
		before = *k.SavedAt
	}
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		s.log.Debug("force save: command service", "err", err)
		return
	}
	var ans struct {
		Error int `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&ans)
	res.Body.Close()
	// 0 = saving, 4 = nothing changed since the last save, 1 = key unknown (closed already)
	if ans.Error != 0 {
		return
	}
	for i := 0; i < 40; i++ { // up to ~10 s
		time.Sleep(250 * time.Millisecond)
		k2, err := s.store.GetKey(ctx, k.Key)
		if err != nil {
			return
		}
		if k2.SavedAt != nil && *k2.SavedAt != before {
			return
		}
	}
	s.log.Warn("force save did not come back in time; saving what is on disk", "key", k.Key)
}

var _ = store.ErrNotFound

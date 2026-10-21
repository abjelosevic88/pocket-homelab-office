package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// fileView is an entry as the UI sees it: with what can be done with it.
type fileView struct {
	storage.Entry
	Kind  office.Kind `json:"kind,omitempty"`
	Opens bool        `json:"opens"`
	Edit  bool        `json:"edit"`
	Ext   string      `json:"ext,omitempty"`
}

func view(e storage.Entry) fileView {
	v := fileView{Entry: e}
	if e.IsDir {
		return v
	}
	v.Ext = office.Ext(e.Name)
	if f, ok := office.Lookup(e.Name); ok {
		v.Kind, v.Opens, v.Edit = f.Kind, true, f.Edit
	}
	return v
}

func storageCtx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 90*time.Second)
}

func (s *Server) handleListDir(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	dir, err := storage.Clean(r.URL.Query().Get("path"))
	if err != nil {
		s.fail(w, err, "folder")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	es, err := b.ListDir(ctx, dir)
	if err != nil {
		s.fail(w, err, "listing "+firstNonEmpty(dir, "the folder"))
		return
	}
	out := make([]fileView, 0, len(es))
	all := r.URL.Query().Get("all") == "1"
	for _, e := range es {
		v := view(e)
		if !all && !e.IsDir && !v.Opens {
			continue
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": src.ID, "path": dir, "entries": out, "writable": true, "tree": src.Kind != "documents"})
}

// handleDownload streams a file as it is.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	_, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	p, err := storage.Clean(r.URL.Query().Get("path"))
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a file path is needed")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	data, e, err := b.Read(ctx, p)
	if err != nil {
		s.fail(w, err, "reading "+p)
		return
	}
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" {
		disp = "inline"
	}
	w.Header().Set("Content-Type", office.Mime(e.Name))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disp, asciiName(e.Name), url.PathEscape(e.Name)))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
}

func asciiName(n string) string {
	var b strings.Builder
	for _, r := range n {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('_')
		case r < 128 && r >= 32:
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	dir, err := storage.Clean(r.URL.Query().Get("path"))
	if err != nil {
		s.fail(w, err, "folder")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(s.cfg.MaxUploadMB)<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send multipart/form-data with one or more \"file\" fields")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	var saved []fileView
	var failed []map[string]string
	overwrite := r.URL.Query().Get("overwrite") == "1"
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
		p := storage.Join(dir, name)
		var e storage.Entry
		if overwrite {
			e, err = b.Write(ctx, p, data, "")
		} else {
			e, err = b.Create(ctx, p, data)
		}
		if err != nil {
			failed = append(failed, map[string]string{"file": name, "error": humanErr(err)})
			continue
		}
		s.store.DropKeysFor(ctx, src.ID, e.Path)
		saved = append(saved, view(e))
		s.publish("office.created", fmt.Sprintf("file:%d:%s", src.ID, e.Path), map[string]any{"user_email": userFrom(r.Context()).Email, "name": e.Name, "path": e.Path, "source": src.Kind, "how": "upload", "url": s.fileURL(src.ID, e.Path)})
	}
	if saved == nil {
		saved = []fileView{}
	}
	status := http.StatusCreated
	if len(saved) == 0 && len(failed) > 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{"saved": saved, "failed": failed})
}

func humanErr(err error) string {
	switch {
	case errors.Is(err, storage.ErrExists):
		return "a file with that name is already there"
	case errors.Is(err, storage.ErrAuth):
		return "the storage refused the sign-in"
	case errors.Is(err, storage.ErrLocked):
		return "the file is locked"
	case errors.Is(err, storage.ErrPrecondition):
		return "the file changed meanwhile"
	}
	return err.Error()
}

// fileURL is where a file opens in Pocket Office.
func (s *Server) fileURL(sourceID int64, p string) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/edit/%d?path=%s", s.cfg.BaseURL, sourceID, url.QueryEscape(p))
}

// handleNewFile makes a blank document, spreadsheet or presentation.
func (s *Server) handleNewFile(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		Path string `json:"path"` // folder
		Name string `json:"name"`
		Type string `json:"type"` // docx | xlsx | pptx
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	dir, err := storage.Clean(in.Path)
	if err != nil {
		s.fail(w, err, "folder")
		return
	}
	ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(in.Type), "."))
	blank, err := office.Blank(ext)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = map[string]string{"docx": "New document", "xlsx": "New spreadsheet", "pptx": "New presentation"}[ext]
	}
	if office.Ext(name) != ext {
		name += "." + ext
	}
	if !storage.ValidName(name) {
		writeErr(w, http.StatusBadRequest, "that name cannot be used")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	// "New document (2).docx" when the name is taken
	base := strings.TrimSuffix(name, "."+ext)
	var e storage.Entry
	for i := 1; i <= 50; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d).%s", base, i, ext)
		}
		e, err = b.Create(ctx, storage.Join(dir, n), blank)
		if !errors.Is(err, storage.ErrExists) || src.Kind == "documents" {
			break
		}
	}
	if err != nil {
		s.fail(w, err, "creating "+name)
		return
	}
	_ = s.store.Touch(ctx, me(r), src.ID, e.Path)
	s.publish("office.created", fmt.Sprintf("file:%d:%s", src.ID, e.Path), map[string]any{"user_email": userFrom(r.Context()).Email, "name": e.Name, "path": e.Path, "source": src.Kind, "how": "new", "url": s.fileURL(src.ID, e.Path)})
	writeJSON(w, http.StatusCreated, view(e))
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	_, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	dir, err := storage.Clean(in.Path)
	name := strings.TrimSpace(in.Name)
	if err != nil || !storage.ValidName(name) {
		writeErr(w, http.StatusBadRequest, "that folder name cannot be used")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	p := storage.Join(dir, name)
	if _, err := b.Stat(ctx, p); err == nil {
		writeErr(w, http.StatusConflict, "a file or folder with that name is already there")
		return
	}
	if err := b.Mkdir(ctx, p); err != nil {
		s.fail(w, err, "making the folder")
		return
	}
	e, _ := b.Stat(ctx, p)
	if e.Path == "" {
		e = storage.Entry{Path: p, Name: name, IsDir: true, Modified: time.Now().UTC()}
	}
	writeJSON(w, http.StatusCreated, view(e))
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`   // full new path, or
		Name string `json:"name"` // just a new name in the same folder
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	from, err := storage.Clean(in.From)
	if err != nil || from == "" {
		writeErr(w, http.StatusBadRequest, "a file is needed")
		return
	}
	to := in.To
	if to == "" {
		if !storage.ValidName(strings.TrimSpace(in.Name)) {
			writeErr(w, http.StatusBadRequest, "that name cannot be used")
			return
		}
		to = storage.Join(storage.Dir(from), strings.TrimSpace(in.Name))
	}
	to, err = storage.Clean(to)
	if err != nil || to == "" {
		writeErr(w, http.StatusBadRequest, "that destination cannot be used")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	e, err := b.Move(ctx, from, to)
	if err != nil {
		s.fail(w, err, "renaming "+storage.Base(from))
		return
	}
	s.store.DropKeysFor(ctx, src.ID, from)
	s.store.ForgetRecent(ctx, me(r), src.ID, from)
	writeJSON(w, http.StatusOK, view(e))
}

// handleCopy duplicates a file next to itself ("name (copy).docx") or into another folder.
func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	_, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	from, err := storage.Clean(in.From)
	if err != nil || from == "" {
		writeErr(w, http.StatusBadRequest, "a file is needed")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	data, e, err := b.Read(ctx, from)
	if err != nil {
		s.fail(w, err, "reading "+storage.Base(from))
		return
	}
	to, _ := storage.Clean(in.To)
	ext := office.Ext(e.Name)
	stem := strings.TrimSuffix(e.Name, "."+ext)
	var out storage.Entry
	for i := 0; i < 50; i++ {
		p := to
		if p == "" {
			suffix := " (copy)"
			if i > 0 {
				suffix = fmt.Sprintf(" (copy %d)", i+1)
			}
			p = storage.Join(storage.Dir(from), stem+suffix+"."+ext)
		} else if i > 0 {
			p = storage.Join(storage.Dir(to), strings.TrimSuffix(storage.Base(to), "."+ext)+fmt.Sprintf(" (%d).", i+1)+ext)
		}
		out, err = b.Create(ctx, p, data)
		if !errors.Is(err, storage.ErrExists) {
			break
		}
	}
	if err != nil {
		s.fail(w, err, "copying")
		return
	}
	writeJSON(w, http.StatusCreated, view(out))
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		Path string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := storage.Clean(in.Path)
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a file or folder is needed")
		return
	}
	ctx, cancel := storageCtx(r)
	defer cancel()
	if err := b.Delete(ctx, p); err != nil {
		s.fail(w, err, "deleting "+storage.Base(p))
		return
	}
	s.store.DropKeysFor(ctx, src.ID, p)
	s.store.ForgetRecent(ctx, me(r), src.ID, p)
	w.WriteHeader(http.StatusNoContent)
}

// ---- recent / search / dashboard ----

type recentView struct {
	store.Recent
	SourceName string      `json:"source_name"`
	SourceKind string      `json:"source_kind"`
	Kind       office.Kind `json:"kind,omitempty"`
	Ext        string      `json:"ext,omitempty"`
	Edit       bool        `json:"edit"`
}

func (s *Server) recentViews(ctx context.Context, uid int64, limit int) ([]recentView, error) {
	rs, err := s.store.ListRecent(ctx, uid, limit)
	if err != nil {
		return nil, err
	}
	srcs, _ := s.store.ListSources(ctx, uid)
	names := map[int64]store.Source{}
	for _, src := range srcs {
		names[src.ID] = src
	}
	out := make([]recentView, 0, len(rs))
	for _, r := range rs {
		src, ok := names[r.SourceID]
		if !ok {
			continue
		}
		v := recentView{Recent: r, SourceName: firstNonEmpty(src.Name, defaultSourceName(src.Kind)), SourceKind: src.Kind, Ext: office.Ext(r.Name)}
		if f, ok := office.Lookup(r.Name); ok {
			v.Kind, v.Edit = f.Kind, f.Edit
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	out, err := s.recentViews(r.Context(), me(r), 40)
	if err != nil {
		s.fail(w, err, "recent")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleForgetRecent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sid, _ := strconv.ParseInt(q.Get("source"), 10, 64)
	if sid == 0 {
		_, _ = s.store.DB.ExecContext(r.Context(), `DELETE FROM recent WHERE user_id = ?`, me(r))
	} else {
		s.store.ForgetRecent(r.Context(), me(r), sid, q.Get("path"))
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSearch looks for a name across every source (folder trees are
// walked, at most a few thousand entries each).
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len(q) < 2 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	srcs, err := s.store.ListSources(ctx, me(r))
	if err != nil {
		s.fail(w, err, "sources")
		return
	}
	type hit struct {
		fileView
		SourceID   int64  `json:"source_id"`
		SourceName string `json:"source_name"`
		SourceKind string `json:"source_kind"`
	}
	hits := []hit{}
	for _, src := range srcs {
		b, err := s.backendFor(ctx, &src)
		if err != nil {
			continue
		}
		name := firstNonEmpty(src.Name, defaultSourceName(src.Kind))
		if d, ok := b.(*docsBackend); ok {
			docs, err := d.list(ctx, r.URL.Query().Get("q"))
			if err == nil {
				for i := range docs {
					hits = append(hits, hit{fileView: view(docs[i].entry()), SourceID: src.ID, SourceName: name, SourceKind: src.Kind})
					if len(hits) >= 200 {
						break
					}
				}
			}
			continue
		}
		queue, seen := []string{""}, 0
		for len(queue) > 0 && seen < 4000 && ctx.Err() == nil {
			dir := queue[0]
			queue = queue[1:]
			es, err := b.ListDir(ctx, dir)
			if err != nil {
				break
			}
			for _, e := range es {
				seen++
				if e.IsDir {
					queue = append(queue, e.Path)
					continue
				}
				if office.Opens(e.Name) && strings.Contains(strings.ToLower(e.Name), q) {
					hits = append(hits, hit{fileView: view(e), SourceID: src.ID, SourceName: name, SourceKind: src.Kind})
				}
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Modified.After(hits[j].Modified) })
	if len(hits) > 200 {
		hits = hits[:200]
	}
	writeJSON(w, http.StatusOK, hits)
}

// handleDashboard feeds the Homepage widget.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), me(r)
	srcs, _ := s.store.ListSources(ctx, uid)
	recent, _ := s.recentViews(ctx, uid, 5)
	var open int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM doc_keys WHERE user_id = ? AND created_at > ?`, uid, time.Now().Add(-24*time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")).Scan(&open)
	var week int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM recent WHERE user_id = ? AND opened_at > ?`, uid, time.Now().Add(-7*24*time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")).Scan(&week)
	last := ""
	if len(recent) > 0 {
		last = recent[0].Name
	}
	ds := s.docServer(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"sources": len(srcs), "open_now": open, "opened_week": week, "last": last, "recent": recent, "document_server": ds.Configured()})
}

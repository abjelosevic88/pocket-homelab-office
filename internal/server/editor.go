package server

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Opening a file: the browser asks for an editor config, Document Server
// fetches the file from this app with a one-off token in the address, and
// reports edits to the callback; the callback writes the result back into
// the source the file came from.

// docServer is the Document Server as configured (Settings first, then env).
func (s *Server) docServer(ctx context.Context) *office.Server {
	d := &office.Server{
		PublicURL:   strings.TrimRight(firstNonEmpty(s.store.GetSetting(ctx, "ds_url"), s.cfg.OnlyOfficeURL), "/"),
		InternalURL: strings.TrimRight(firstNonEmpty(s.store.GetSetting(ctx, "ds_internal_url"), s.cfg.OnlyOfficeInternalURL), "/"),
		Secret:      firstNonEmpty(s.store.GetSetting(ctx, "ds_secret"), s.cfg.OnlyOfficeJWTSecret),
		Header:      firstNonEmpty(s.store.GetSetting(ctx, "ds_header"), s.cfg.OnlyOfficeJWTHeader),
	}
	if d.InternalURL == "" {
		d.InternalURL = d.PublicURL
	}
	return d
}

// callbackBase is how Document Server reaches this app.
func (s *Server) callbackBase(ctx context.Context) string {
	return strings.TrimRight(firstNonEmpty(s.store.GetSetting(ctx, "callback_url"), s.cfg.CallbackURL, s.cfg.BaseURL), "/")
}

func docKeyFor(canonical, etag string) string {
	sum := sha256.Sum256([]byte(canonical + "\x00" + etag + "\x00" + store.RandomToken(8)))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:20]))
}

// handleOpen returns the editor configuration for a file.
func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		Path string `json:"path"`
		// Mobile asks for Document Server's mobile editors, which the free
		// Community build opens for viewing only; Desktop forces the desktop
		// editor (editable) even on a phone.
		Mobile  bool   `json:"mobile"`
		Desktop bool   `json:"desktop"`
		Dark    bool   `json:"dark"`
		Lang    string `json:"lang"`
		Engine  string `json:"engine"` // onlyoffice | collabora; empty = the person's default
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := storage.Clean(in.Path)
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a file path is needed")
		return
	}
	engine := s.userEngine(ctx, u.ID, in.Engine)
	if engine == "" {
		writeErr(w, http.StatusServiceUnavailable, "no editor is set up yet (Settings → Document Server)")
		return
	}
	ds := s.docServer(ctx)
	cb := s.callbackBase(ctx)
	if cb == "" {
		writeErr(w, http.StatusServiceUnavailable, "set BASE_URL or CALLBACK_URL so Document Server can reach this app")
		return
	}
	f, known := office.Lookup(p)
	if !known {
		writeErr(w, http.StatusBadRequest, "Document Server does not open ."+office.Ext(p)+" files")
		return
	}
	sctx, cancel := storageCtx(r)
	defer cancel()
	e, err := b.Stat(sctx, p)
	if err != nil {
		s.fail(w, err, "opening "+storage.Base(p))
		return
	}
	if e.IsDir {
		writeErr(w, http.StatusBadRequest, "that is a folder")
		return
	}
	canonical := b.Canonical(p)
	k, err := s.store.ActiveKey(ctx, canonical)
	if err != nil || (k != nil && k.ETag != e.ETag && k.SavedAt == nil && time.Since(parseTime(k.CreatedAt)) > 10*time.Minute) {
		// no open session, or the file changed outside while nobody saved from here: a fresh key
		if k != nil {
			_ = s.store.DropKey(ctx, k.Key)
		}
		k = &store.DocKey{Key: docKeyFor(canonical, e.ETag), Canonical: canonical, UserID: u.ID, SourceID: src.ID, Path: p, ETag: e.ETag, FetchToken: store.RandomToken(24)}
		if err := s.store.PutKey(ctx, k); err != nil {
			s.fail(w, err, "document key")
			return
		}
	}
	_ = s.store.Touch(ctx, u.ID, src.ID, p)

	fv := view(e)
	s.decorateReviews(ctx, u.ID, src, b, []fileView{fv})
	common := map[string]any{"key": k.Key, "kind": string(f.Kind), "ext": office.Ext(p), "edit": f.Edit, "convertible": f.Convertible, "editable_as": office.EditableExt(f.Kind), "lossy": f.Lossy,
		"exports": office.Exports(p), "file": fv, "draft": src.Kind == "drafts", "engines": s.engines(ctx), "collab": s.collabInfo(ctx, u.ID)}
	if engine == "collabora" {
		co, err := s.collaboraOpen(ctx, k, e, f, firstNonEmpty(in.Lang, "en"), in.Dark)
		if err != nil {
			// fall back to Document Server when Collabora does not know the type
			if !ds.Configured() {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			engine = "onlyoffice"
		} else {
			mode := "view"
			if co["edit"] == true {
				mode = "edit"
			}
			common["engine"], common["mode"], common["collabora"], common["phone_view"] = "collabora", mode, co, false
			s.publish("office.opened", fmt.Sprintf("file:%d:%s", src.ID, p), map[string]any{"user_email": u.Email, "name": e.Name, "path": p, "source": src.Kind, "mode": mode, "kind": string(f.Kind), "engine": "collabora", "url": s.fileURL(src.ID, p)})
			writeJSON(w, http.StatusOK, common)
			return
		}
	}
	if !ds.Configured() {
		writeErr(w, http.StatusServiceUnavailable, "no Document Server is set up yet (Settings → Document Server)")
		return
	}

	mode := "view"
	if f.Edit {
		mode = "edit"
	}
	docType := string(f.Kind)
	fileURL := fmt.Sprintf("%s/api/v1/oo/file/%s?t=%s", cb, k.Key, k.FetchToken)
	callbackURL := fmt.Sprintf("%s/api/v1/oo/callback/%s", cb, k.Key)
	editorType := "desktop"
	phoneView := false
	if in.Mobile && !in.Desktop {
		// Community Document Server: mobile editors view only, so ask for
		// exactly that (no licence warning); "Edit on this phone" reopens
		// with the desktop editor.
		editorType, phoneView = "mobile", f.Edit
		mode = "view"
	}
	theme := "theme-light"
	if in.Dark {
		theme = "theme-dark"
	}
	lang := firstNonEmpty(in.Lang, "en")
	goBack := map[string]any{"blank": false, "requestClose": true, "text": "Back to files"}
	cfg := map[string]any{
		"type":         editorType,
		"documentType": docType,
		"document": map[string]any{
			"fileType": office.Ext(p), "key": k.Key, "title": e.Name, "url": fileURL,
			"info":        map[string]any{"owner": firstNonEmpty(u.Name, u.Email), "folder": firstNonEmpty(storage.Dir(p), firstNonEmpty(src.Name, defaultSourceName(src.Kind)))},
			"permissions": map[string]any{"edit": f.Edit, "download": true, "print": true, "review": f.Edit, "comment": f.Edit, "fillForms": f.Edit, "copy": true, "modifyFilter": f.Edit, "chat": true},
		},
		"editorConfig": map[string]any{
			"mode": mode, "lang": lang, "callbackUrl": callbackURL,
			"user": map[string]any{"id": "u" + strconv.FormatInt(u.ID, 10), "name": firstNonEmpty(u.Name, u.Email)},
			"customization": map[string]any{
				"autosave": true, "forcesave": true, "compactHeader": false, "feedback": false, "help": false,
				"uiTheme": theme, "goback": goBack, "logo": map[string]any{"image": s.cfg.BaseURL + "/logo-ds.svg", "imageDark": s.cfg.BaseURL + "/logo-ds.svg", "url": s.cfg.BaseURL, "visible": true},
				"customer":  map[string]any{"name": "Pocket Office", "www": s.cfg.BaseURL, "logo": s.cfg.BaseURL + "/favicon.svg"},
				"anonymous": map[string]any{"request": false}, "mobileForceView": phoneView,
			},
			"coEditing": map[string]any{"mode": "fast", "change": true},
		},
		"height": "100%", "width": "100%",
	}
	if ds.Secret != "" {
		tok, err := office.Sign(ds.Secret, cfg)
		if err != nil {
			s.fail(w, err, "signing the editor config")
			return
		}
		cfg["token"] = tok
	}
	s.publish("office.opened", fmt.Sprintf("file:%d:%s", src.ID, p), map[string]any{"user_email": u.Email, "name": e.Name, "path": p, "source": src.Kind, "mode": mode, "kind": docType, "url": s.fileURL(src.ID, p)})
	writeJSON(w, http.StatusOK, map[string]any{
		"api_js": ds.APIScript(), "config": cfg, "key": k.Key, "mode": mode, "kind": docType, "ext": office.Ext(p), "edit": f.Edit, "convertible": f.Convertible, "phone_view": phoneView,
		"editable_as": office.EditableExt(f.Kind), "lossy": f.Lossy, "exports": office.Exports(p), "file": view(e), "draft": src.Kind == "drafts",
	})
}

func parseTime(s string) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.000Z", s)
	return t
}

// handleFetchFile hands Document Server the file for a key.
func (s *Server) handleFetchFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	k, err := s.store.GetKey(ctx, chi.URLParam(r, "key"))
	if err != nil || k.FetchToken == "" || r.URL.Query().Get("t") != k.FetchToken {
		writeErr(w, http.StatusNotFound, "unknown document")
		return
	}
	// A JWT from Document Server is checked when it sends one.
	ds := s.docServer(ctx)
	if ds.Secret != "" {
		if h := r.Header.Get(ds.Header); h != "" {
			if _, err := office.Verify(ds.Secret, strings.TrimPrefix(h, "Bearer ")); err != nil {
				writeErr(w, http.StatusForbidden, "bad token: "+err.Error())
				return
			}
		}
	}
	var b storage.Backend
	if k.SourceID == 0 {
		b = s.scratchBackend()
	} else {
		src, err := s.store.GetSourceAny(ctx, k.SourceID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "source is gone")
			return
		}
		if b, err = s.backendFor(ctx, src); err != nil {
			s.fail(w, err, "source")
			return
		}
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	data, e, err := b.Read(sctx, k.Path)
	if err != nil {
		s.fail(w, err, "reading "+storage.Base(k.Path))
		return
	}
	w.Header().Set("Content-Type", office.Mime(e.Name))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename*=UTF-8''%s`, url.PathEscape(e.Name)))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

type callbackBody struct {
	Key           string `json:"key"`
	Status        int    `json:"status"`
	URL           string `json:"url"`
	FileType      string `json:"filetype"`
	Users         []string
	NotModified   bool   `json:"notmodified"`
	ForceSaveType int    `json:"forcesavetype"`
	Token         string `json:"token"`
	Actions       []struct {
		Type   int    `json:"type"`
		UserID string `json:"userid"`
	} `json:"actions"`
	History json.RawMessage `json:"history"`
}

func (s *Server) saveMutex(key string) *sync.Mutex {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	m, ok := s.saves[key]
	if !ok {
		m = &sync.Mutex{}
		s.saves[key] = m
	}
	return m
}

// handleCallback is where Document Server reports what happened to a
// document: 1 editing, 2 closed with changes (save), 3 save error,
// 4 closed without changes, 6 force save, 7 force save error.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := chi.URLParam(r, "key")
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": 1, "message": "unreadable body"})
		return
	}
	var cb callbackBody
	_ = json.Unmarshal(raw, &cb)
	ds := s.docServer(ctx)
	if ds.Secret != "" {
		verified := false
		if h := r.Header.Get(ds.Header); h != "" {
			if claims, err := office.Verify(ds.Secret, strings.TrimPrefix(h, "Bearer ")); err == nil {
				if payload, ok := claims["payload"]; ok {
					pb, _ := json.Marshal(payload)
					_ = json.Unmarshal(pb, &cb)
				}
				verified = true
			}
		}
		if !verified && cb.Token != "" {
			if claims, err := office.Verify(ds.Secret, cb.Token); err == nil {
				pb, _ := json.Marshal(claims)
				_ = json.Unmarshal(pb, &cb)
				verified = true
			}
		}
		if !verified {
			s.log.Warn("document server callback without a valid token", "key", key)
			writeJSON(w, http.StatusForbidden, map[string]any{"error": 1, "message": "bad token"})
			return
		}
	}
	k, err := s.store.GetKey(ctx, key)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": 1, "message": "unknown key"})
		return
	}
	switch cb.Status {
	case 1, 4:
		if cb.Status == 4 {
			_ = s.store.DropKey(ctx, key)
		}
		writeJSON(w, http.StatusOK, map[string]any{"error": 0})
		return
	case 3, 7:
		s.log.Warn("document server could not save", "key", key, "status", cb.Status, "path", k.Path)
		if cb.Status == 3 {
			_ = s.store.DropKey(ctx, key)
		}
		writeJSON(w, http.StatusOK, map[string]any{"error": 0})
		return
	case 2, 6:
	default:
		writeJSON(w, http.StatusOK, map[string]any{"error": 0})
		return
	}
	if cb.URL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": 1, "message": "no file address in the callback"})
		return
	}
	mu := s.saveMutex(key)
	mu.Lock()
	defer mu.Unlock()
	if err := s.saveFromDS(ctx, ds, k, cb); err != nil {
		s.log.Error("saving a document from document server failed", "key", key, "path", k.Path, "err", err)
		writeJSON(w, http.StatusOK, map[string]any{"error": 1, "message": err.Error()})
		return
	}
	if cb.Status == 2 {
		_ = s.store.DropKey(ctx, key)
	}
	writeJSON(w, http.StatusOK, map[string]any{"error": 0})
}

// saveFromDS downloads the saved document and writes it into the source.
func (s *Server) saveFromDS(ctx context.Context, ds *office.Server, k *store.DocKey, cb callbackBody) error {
	dctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(dctx, http.MethodGet, ds.Rewrite(cb.URL), nil)
	res, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("downloading from document server: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading from document server: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<30))
	if err != nil {
		return err
	}
	src, err := s.store.GetSourceAny(dctx, k.SourceID)
	if err != nil {
		return errors.New("the source is gone")
	}
	b, err := s.backendFor(dctx, src)
	if err != nil {
		return err
	}
	path := k.Path
	// Document Server says what it saved as; a different type is kept beside the original.
	if ft := strings.ToLower(cb.FileType); ft != "" && ft != office.Ext(path) {
		path = strings.TrimSuffix(path, "."+office.Ext(path)) + "." + ft
	}
	var e storage.Entry
	if path == k.Path {
		e, err = b.Write(dctx, path, data, k.ETag)
		if errors.Is(err, storage.ErrPrecondition) {
			// changed outside meanwhile: never overwrite, keep both
			alt := conflictName(path)
			s.log.Warn("file changed outside while open; saving beside it", "path", path, "as", alt)
			e, err = b.Create(dctx, alt, data)
			path = alt
		}
	} else {
		e, err = b.Write(dctx, path, data, "")
	}
	if err != nil {
		return err
	}
	if path == k.Path {
		_ = s.store.KeySaved(dctx, k.Key, e.ETag)
	}
	who := s.userEmail(dctx, k.UserID)
	s.publish("office.saved", fmt.Sprintf("file:%d:%s", k.SourceID, path), map[string]any{"user_email": who, "name": e.Name, "path": path, "source": src.Kind, "size": e.Size, "force": cb.Status == 6, "url": s.fileURL(k.SourceID, path)})
	s.log.Info("document saved", "path", path, "source", src.Kind, "bytes", len(data), "force", cb.Status == 6)
	return nil
}

func conflictName(p string) string {
	ext := office.Ext(p)
	stem := strings.TrimSuffix(p, "."+ext)
	return fmt.Sprintf("%s (conflict %s).%s", stem, time.Now().Format("2006-01-02 15.04"), ext)
}

// handleEditCopy turns a view-only file (doc, xls, ppt, odt…) into its
// editable type beside the original and returns the new file.
func (s *Server) handleEditCopy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		Path string `json:"path"`
		To   string `json:"to"` // target extension; default the family's editable type
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := storage.Clean(in.Path)
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a file path is needed")
		return
	}
	f, known := office.Lookup(p)
	if !known {
		writeErr(w, http.StatusBadRequest, "Document Server does not open that type")
		return
	}
	to := strings.ToLower(strings.TrimPrefix(in.To, "."))
	if to == "" {
		to = office.EditableExt(f.Kind)
	}
	if to == "" || !office.CanExport(p, to) {
		writeErr(w, http.StatusBadRequest, "Document Server cannot turn ."+office.Ext(p)+" into an editable type")
		return
	}
	data, e, err := s.convert(ctx, src.ID, b, p, to)
	if err != nil {
		s.fail(w, err, "converting "+storage.Base(p))
		return
	}
	target := strings.TrimSuffix(p, "."+office.Ext(p)) + "." + to
	var out storage.Entry
	for i := 0; i < 50; i++ {
		t := target
		if i > 0 {
			t = strings.TrimSuffix(target, "."+to) + fmt.Sprintf(" (%d).", i+1) + to
		}
		out, err = b.Create(ctx, t, data)
		if !errors.Is(err, storage.ErrExists) {
			break
		}
	}
	if err != nil {
		s.fail(w, err, "saving the copy")
		return
	}
	_ = e
	_ = s.store.Touch(ctx, me(r), src.ID, out.Path)
	s.publish("office.created", fmt.Sprintf("file:%d:%s", src.ID, out.Path), map[string]any{"user_email": userFrom(ctx).Email, "name": out.Name, "path": out.Path, "source": src.Kind, "how": "converted", "url": s.fileURL(src.ID, out.Path)})
	writeJSON(w, http.StatusCreated, view(out))
}

// convert runs a file through Document Server's conversion service. The
// file is offered to Document Server through a short-lived key; sourceID 0
// means the scratch folder (probes).
func (s *Server) convert(ctx context.Context, sourceID int64, b storage.Backend, p, to string) ([]byte, storage.Entry, error) {
	ds := s.docServer(ctx)
	if !ds.Configured() {
		return nil, storage.Entry{}, errors.New("no Document Server is set up yet")
	}
	cb := s.callbackBase(ctx)
	if cb == "" {
		return nil, storage.Entry{}, errors.New("set BASE_URL or CALLBACK_URL so Document Server can reach this app")
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	e, err := b.Stat(sctx, p)
	if err != nil {
		return nil, storage.Entry{}, err
	}
	u := userFrom(ctx)
	var uid int64
	if u != nil {
		uid = u.ID
	}
	// a throwaway key: it is not a document session, Document Server only fetches once
	k := &store.DocKey{Key: "cv" + docKeyFor(b.Canonical(p), e.ETag+time.Now().String()), Canonical: "convert:" + b.Canonical(p), UserID: uid, SourceID: sourceID, Path: p, ETag: e.ETag, FetchToken: store.RandomToken(24)}
	if err := s.store.PutKey(sctx, k); err != nil {
		return nil, storage.Entry{}, err
	}
	defer func() { _ = s.store.DropKey(context.Background(), k.Key) }()
	fileURL := fmt.Sprintf("%s/api/v1/oo/file/%s?t=%s", cb, k.Key, k.FetchToken)
	data, err := ds.Convert(sctx, fileURL, office.Ext(p), to, k.Key, e.Name)
	if err != nil {
		return nil, storage.Entry{}, err
	}
	return data, e, nil
}

// handleExport downloads a file converted to another type (PDF, docx…).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	p, err := storage.Clean(r.URL.Query().Get("path"))
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a file path is needed")
		return
	}
	to := strings.ToLower(strings.TrimPrefix(r.URL.Query().Get("to"), "."))
	if to == "" {
		to = "pdf"
	}
	if !office.CanExport(p, to) {
		writeErr(w, http.StatusBadRequest, "Document Server does not turn ."+office.Ext(p)+" into ."+to+" (it offers: "+strings.Join(office.Exports(p), ", ")+")")
		return
	}
	data, e, err := s.convert(r.Context(), src.ID, b, p, to)
	if err != nil {
		s.fail(w, err, "converting "+storage.Base(p))
		return
	}
	name := strings.TrimSuffix(e.Name, "."+office.Ext(e.Name)) + "." + to
	w.Header().Set("Content-Type", office.Mime(name))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiName(name), url.PathEscape(name)))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

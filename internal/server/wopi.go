package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// WOPI host for Collabora Online: the document key is the file id and the
// key's fetch token is the access token. Collabora calls CheckFileInfo,
// GetFile and PutFile here; the browser loads the editor from Collabora with
// WOPISrc pointing back at these routes.

func (s *Server) collabora(ctx context.Context) *office.Collabora {
	s.coMu.Lock()
	defer s.coMu.Unlock()
	pub := strings.TrimRight(firstNonEmpty(s.store.GetSetting(ctx, "co_url"), s.cfg.CollaboraURL), "/")
	in := strings.TrimRight(firstNonEmpty(s.store.GetSetting(ctx, "co_internal_url"), s.cfg.CollaboraInternalURL, pub), "/")
	if s.co == nil || s.co.PublicURL != pub || s.co.InternalURL != in {
		s.co = &office.Collabora{PublicURL: pub, InternalURL: in}
	}
	return s.co
}

// boxEngine is the engine the box prefers (Settings, else DEFAULT_ENGINE).
func (s *Server) boxEngine(ctx context.Context) string {
	return firstNonEmpty(s.store.GetSetting(ctx, "default_engine"), s.cfg.DefaultEngine, "onlyoffice")
}

// engines lists what is set up, the box's default first.
func (s *Server) engines(ctx context.Context) []string {
	var out []string
	if s.docServer(ctx).Configured() {
		out = append(out, "onlyoffice")
	}
	if s.collabora(ctx).Configured() {
		out = append(out, "collabora")
	}
	def := s.boxEngine(ctx)
	for i, e := range out {
		if e == def && i > 0 {
			out = append([]string{e}, append(out[:i:i], out[i+1:]...)...)
		}
	}
	return out
}

// userEngine is the person's default engine ("onlyoffice" unless set and available).
func (s *Server) userEngine(ctx context.Context, uid int64, want string) string {
	avail := s.engines(ctx)
	pick := func(e string) bool {
		for _, a := range avail {
			if a == e {
				return true
			}
		}
		return false
	}
	if want != "" && pick(want) {
		return want
	}
	if e := s.store.UserSetting(ctx, uid, "engine"); e != "" && pick(e) {
		return e
	}
	if len(avail) > 0 {
		return avail[0]
	}
	return ""
}

// wopiKey resolves the file id + access token of a WOPI request.
func (s *Server) wopiKey(w http.ResponseWriter, r *http.Request) (*store.DocKey, storage.Backend, *store.Source, bool) {
	ctx := r.Context()
	k, err := s.store.GetKey(ctx, chi.URLParam(r, "key"))
	tok := r.URL.Query().Get("access_token")
	if err != nil || tok == "" || tok != k.FetchToken {
		writeErr(w, http.StatusUnauthorized, "unknown document or token")
		return nil, nil, nil, false
	}
	var b storage.Backend
	var src *store.Source
	if k.SourceID == 0 {
		b = s.scratchBackend()
		src = &store.Source{Kind: "builtin"}
	} else {
		src, err = s.store.GetSourceAny(ctx, k.SourceID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "source is gone")
			return nil, nil, nil, false
		}
		if b, err = s.backendFor(ctx, src); err != nil {
			s.fail(w, err, "source")
			return nil, nil, nil, false
		}
	}
	return k, b, src, true
}

func wopiTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000Z") }

// handleWOPICheckFileInfo describes the file and what the person may do.
func (s *Server) handleWOPICheckFileInfo(w http.ResponseWriter, r *http.Request) {
	k, b, _, ok := s.wopiKey(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	e, err := b.Stat(ctx, k.Path)
	if err != nil {
		s.fail(w, err, "file")
		return
	}
	u, _ := s.store.GetUser(ctx, k.UserID)
	name, uid := "Someone", "u0"
	if u != nil {
		name, uid = firstNonEmpty(u.Name, u.Email), "u"+strconv.FormatInt(u.ID, 10)
	}
	f, _ := office.Lookup(e.Name)
	canWrite := f.Edit
	writeJSON(w, http.StatusOK, map[string]any{
		"BaseFileName": e.Name, "Size": e.Size, "Version": e.ETag, "LastModifiedTime": wopiTime(e.Modified),
		"OwnerId": uid, "UserId": uid, "UserFriendlyName": name,
		"UserCanWrite": canWrite, "UserCanNotWriteRelative": false, "UserCanRename": false,
		"SupportsUpdate": true, "SupportsLocks": false, "SupportsRename": false,
		"PostMessageOrigin": s.cfg.BaseURL, "BreadcrumbDocName": e.Name,
		"HideSaveOption": false, "HideExportOption": false, "HidePrintOption": false, "DisableExport": false, "DisableCopy": false, "DisablePrint": false,
		"EnableOwnerTermination": false, "EnableShare": false, "IsAdminUser": false, "DisableInsertLocalImage": false,
	})
}

func (s *Server) handleWOPIGetFile(w http.ResponseWriter, r *http.Request) {
	k, b, _, ok := s.wopiKey(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	data, e, err := b.Read(ctx, k.Path)
	if err != nil {
		s.fail(w, err, "reading "+storage.Base(k.Path))
		return
	}
	w.Header().Set("Content-Type", office.Mime(e.Name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-WOPI-ItemVersion", e.ETag)
	_, _ = w.Write(data)
}

// handleWOPIPutFile takes a save from Collabora and writes it where the file
// came from. Collabora sends the modification time it last saw; when the
// file changed in storage meanwhile it gets 409 + COOLStatusCode 1010 and
// asks the person whether to overwrite.
func (s *Server) handleWOPIPutFile(w http.ResponseWriter, r *http.Request) {
	k, b, src, ok := s.wopiKey(w, r)
	if !ok {
		return
	}
	if ov := r.Header.Get("X-WOPI-Override"); ov != "" && ov != "PUT" {
		writeErr(w, http.StatusNotImplemented, "unsupported WOPI operation "+ov)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	mu := s.saveMutex(k.Key)
	mu.Lock()
	defer mu.Unlock()
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<30))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "unreadable body")
		return
	}
	if seen := r.Header.Get("X-COOL-WOPI-Timestamp"); seen != "" {
		if cur, err := b.Stat(ctx, k.Path); err == nil && !cur.Modified.IsZero() {
			if t, perr := time.Parse(time.RFC3339Nano, seen); perr == nil && !t.Truncate(time.Second).Equal(cur.Modified.Truncate(time.Second)) {
				s.log.Warn("collabora save: file changed in storage meanwhile", "path", k.Path, "seen", seen, "now", wopiTime(cur.Modified))
				writeJSON(w, http.StatusConflict, map[string]any{"COOLStatusCode": 1010})
				return
			}
		}
	}
	e, err := b.Write(ctx, k.Path, data, "")
	if err != nil {
		s.log.Error("collabora save failed", "path", k.Path, "err", err)
		s.fail(w, err, "saving "+storage.Base(k.Path))
		return
	}
	_ = s.store.KeySaved(ctx, k.Key, e.ETag)
	who := s.userEmail(ctx, k.UserID)
	s.publish("office.saved", fmt.Sprintf("file:%d:%s", k.SourceID, k.Path), map[string]any{"user_email": who, "name": e.Name, "path": k.Path, "source": src.Kind, "size": e.Size, "engine": "collabora", "autosave": r.Header.Get("X-COOL-WOPI-IsAutosave") == "true", "url": s.fileURL(k.SourceID, k.Path)})
	s.log.Info("document saved", "path", k.Path, "source", src.Kind, "bytes", len(data), "engine", "collabora", "exit", r.Header.Get("X-COOL-WOPI-IsExitSave") == "true")
	writeJSON(w, http.StatusOK, map[string]any{"LastModifiedTime": wopiTime(e.Modified)})
}

// handleWOPIFileOps answers lock calls (no locks here) and PutRelative
// ("Save as" / export from Collabora): the new file lands beside the original.
func (s *Server) handleWOPIFileOps(w http.ResponseWriter, r *http.Request) {
	k, b, src, ok := s.wopiKey(w, r)
	if !ok {
		return
	}
	switch r.Header.Get("X-WOPI-Override") {
	case "LOCK", "UNLOCK", "REFRESH_LOCK", "GET_LOCK":
		w.WriteHeader(http.StatusOK)
	case "PUT_RELATIVE":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<30))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "unreadable body")
			return
		}
		name := decodeWOPIName(r.Header.Get("X-WOPI-RelativeTarget"))
		suggested := false
		if name == "" {
			name, suggested = decodeWOPIName(r.Header.Get("X-WOPI-SuggestedTarget")), true
		}
		if strings.HasPrefix(name, ".") { // an extension only: same stem
			name = strings.TrimSuffix(storage.Base(k.Path), "."+office.Ext(k.Path)) + name
		}
		if !storage.ValidName(name) {
			writeErr(w, http.StatusBadRequest, "bad name")
			return
		}
		target := storage.Join(storage.Dir(k.Path), name)
		var e storage.Entry
		if suggested {
			e, err = createUnique(ctx, b, target, data)
		} else if r.Header.Get("X-WOPI-OverwriteRelativeTarget") == "true" {
			e, err = b.Write(ctx, target, data, "")
		} else {
			e, err = b.Create(ctx, target, data)
			if errors.Is(err, storage.ErrExists) {
				w.Header().Set("X-WOPI-ValidRelativeTarget", name)
				writeErr(w, http.StatusConflict, "a file with that name is already there")
				return
			}
		}
		if err != nil {
			s.fail(w, err, "saving "+name)
			return
		}
		// a key so Collabora can open the new file at once
		nk := &store.DocKey{Key: docKeyFor(b.Canonical(e.Path), e.ETag), Canonical: b.Canonical(e.Path), UserID: k.UserID, SourceID: k.SourceID, Path: e.Path, ETag: e.ETag, FetchToken: store.RandomToken(24)}
		_ = s.store.PutKey(ctx, nk)
		_ = s.store.Touch(ctx, k.UserID, k.SourceID, e.Path)
		s.publish("office.created", fmt.Sprintf("file:%d:%s", k.SourceID, e.Path), map[string]any{"user_email": s.userEmail(ctx, k.UserID), "name": e.Name, "path": e.Path, "source": src.Kind, "how": "save-as", "url": s.fileURL(k.SourceID, e.Path)})
		writeJSON(w, http.StatusOK, map[string]any{"Name": e.Name, "Url": s.wopiSrc(ctx, nk.Key) + "?access_token=" + url.QueryEscape(nk.FetchToken), "HostEditUrl": s.fileURL(k.SourceID, e.Path)})
	default:
		writeErr(w, http.StatusNotImplemented, "unsupported WOPI operation")
	}
}

func decodeWOPIName(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	// UTF-7 is what the spec says; Collabora sends plain UTF-8 or percent-encoded
	if u, err := url.PathUnescape(h); err == nil {
		h = u
	}
	return storage.Base(h)
}

// wopiBase is how Collabora reaches this app. Collabora lets only the WOPI
// host's origin embed it, so this is the public address by default; the
// Collabora container must be able to resolve and reach it.
func (s *Server) wopiBase(ctx context.Context) string {
	return strings.TrimRight(firstNonEmpty(s.store.GetSetting(ctx, "wopi_url"), s.cfg.WOPIURL, s.cfg.BaseURL, s.callbackBase(ctx)), "/")
}

// wopiSrc is the file's WOPI address as Collabora reaches it.
func (s *Server) wopiSrc(ctx context.Context, key string) string {
	return s.wopiBase(ctx) + "/wopi/files/" + key
}

// collaboraOpen builds the browser's way into Collabora for a document key.
func (s *Server) collaboraOpen(ctx context.Context, k *store.DocKey, e storage.Entry, f office.Format, lang string, dark bool) (map[string]any, error) {
	co := s.collabora(ctx)
	action, canEdit := co.Action(ctx, office.Ext(e.Name), f.Edit)
	if action == "" {
		return nil, errors.New("collabora does not open ." + office.Ext(e.Name))
	}
	src := s.wopiSrc(ctx, k.Key)
	u, _ := url.Parse(action)
	q := u.Query()
	q.Set("WOPISrc", src)
	if lang != "" {
		q.Set("lang", lang)
	}
	q.Set("closebutton", "1") // Collabora posts UI_Close to us when it is pressed
	_ = dark                  // Collabora keeps its own theme setting per browser
	u.RawQuery = q.Encode()
	return map[string]any{
		"action_url": u.String(), "access_token": k.FetchToken, "access_token_ttl": strconv.FormatInt(time.Now().Add(24*time.Hour).UnixMilli(), 10),
		"wopi_src": src, "edit": canEdit, "origin": co.PublicURL,
	}, nil
}

// decodeJSON is a tiny helper for tests.
func decodeJSON(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }

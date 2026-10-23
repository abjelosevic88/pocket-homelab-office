package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// A source is one place a person's files live. Each person has at least the
// built-in folder; Nextcloud (through Pocket Core or typed in), WebDAV,
// server folders, Pocket Documents and Pocket Files (through Pocket Core) can
// be added in Settings → Sources.

type errNotConnected struct {
	Name string
	Msg  string
}

func (e errNotConnected) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Name + " is not connected"
}

// backendFor builds the storage behind a source. Credentials that come from
// Core are asked for on every request, never kept.
func (s *Server) backendFor(ctx context.Context, src *store.Source) (storage.Backend, error) {
	switch src.Kind {
	case "builtin":
		return storage.NewLocal(filepath.Join(s.cfg.FilesDir, strconv.FormatInt(src.UserID, 10)), "builtin", "My files"), nil
	case "drafts":
		return storage.NewLocal(filepath.Join(s.cfg.DataDir, "drafts", strconv.FormatInt(src.UserID, 10)), "drafts", "Drafts"), nil
	case "folder":
		if !s.folderAllowed(src.Folder) {
			return nil, errors.New("that folder is not one the box offers (FOLDER_ROOTS)")
		}
		return storage.NewLocal(src.Folder, "folder", firstNonEmpty(src.Name, filepath.Base(src.Folder))), nil
	case "webdav":
		if src.URL == "" {
			return nil, errNotConnected{Name: "WebDAV", Msg: "the WebDAV source has no address"}
		}
		user, pass := src.Username, src.Secret
		b, err := storage.NewWebDAV(src.URL, func(context.Context) (string, string, error) { return user, pass, nil }, firstNonEmpty(src.Name, "WebDAV"))
		if err != nil {
			return nil, err
		}
		b.UserAgent = "PocketOffice/" + Version
		return b, nil
	case "nextcloud":
		if src.ViaCore {
			if !s.core.Configured() {
				return nil, errNotConnected{Name: "Nextcloud", Msg: "there is no Pocket Core on this box; type the Nextcloud address and an app password instead"}
			}
			email := s.userEmail(ctx, src.UserID)
			cred, err := s.core.ServiceFor(ctx, email, "nextcloud")
			if err != nil {
				var le *core.LinkError
				if errors.As(err, &le) {
					return nil, errNotConnected{Name: "Nextcloud", Msg: "Nextcloud through Pocket Core: " + firstNonEmpty(linkReason("Nextcloud", le.Reason), le.Message)}
				}
				return nil, fmt.Errorf("Nextcloud through Pocket Core: %w", err)
			}
			user := cred.Fields["USERNAME"]
			pass := firstNonEmpty(cred.Fields["APP_PASSWORD"], cred.Fields["PASSWORD"], cred.Key)
			if user == "" || pass == "" {
				return nil, errNotConnected{Name: "Nextcloud", Msg: "Pocket Core has no Nextcloud username and app password for you (Users → Credentials)"}
			}
			b, err := storage.NewNextcloud(cred.URL, user, src.Folder, func(ctx context.Context) (string, string, error) {
				c, err := s.core.ServiceFor(ctx, email, "nextcloud")
				if err != nil {
					return "", "", err
				}
				return c.Fields["USERNAME"], firstNonEmpty(c.Fields["APP_PASSWORD"], c.Fields["PASSWORD"], c.Key), nil
			})
			if err != nil {
				return nil, err
			}
			b.UserAgent = "PocketOffice/" + Version
			return b, nil
		}
		if src.URL == "" || src.Username == "" {
			return nil, errNotConnected{Name: "Nextcloud", Msg: "the Nextcloud source needs an address and a username"}
		}
		user, pass := src.Username, src.Secret
		b, err := storage.NewNextcloud(src.URL, user, src.Folder, func(context.Context) (string, string, error) { return user, pass, nil })
		if err != nil {
			return nil, err
		}
		b.UserAgent = "PocketOffice/" + Version
		return b, nil
	case "documents":
		return s.documentsBackend(ctx, src)
	case "files":
		return s.filesBackend(ctx, src)
	}
	return nil, fmt.Errorf("unknown source kind %q", src.Kind)
}

func (s *Server) folderAllowed(p string) bool {
	p = filepath.Clean(p)
	for _, root := range s.cfg.FolderRoots {
		if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func (s *Server) userEmail(ctx context.Context, uid int64) string {
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		return u.Email
	}
	return ""
}

// sourceAndBackend loads one of the caller's sources and its storage.
func (s *Server) sourceAndBackend(w http.ResponseWriter, r *http.Request) (*store.Source, storage.Backend, bool) {
	ctx := r.Context()
	src, err := s.store.GetSource(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return nil, nil, false
	}
	b, err := s.backendFor(ctx, src)
	if err != nil {
		s.fail(w, err, "source")
		return nil, nil, false
	}
	return src, b, true
}

// sourceView is what the UI sees of a source.
type sourceView struct {
	store.Source
	Where    string `json:"where"`
	Writable bool   `json:"writable"`
	Tree     bool   `json:"tree"` // has folders (Pocket Documents is flat)
	Error    string `json:"error,omitempty"`
}

func (s *Server) sourceViews(ctx context.Context, uid int64) ([]sourceView, error) {
	if _, err := s.store.DraftsSource(ctx, uid); err != nil { // makes the built-in place too
		return nil, err
	}
	srcs, err := s.store.ListSources(ctx, uid)
	if err != nil {
		return nil, err
	}
	out := make([]sourceView, 0, len(srcs))
	for i := range srcs {
		v := sourceView{Source: srcs[i], Writable: true, Tree: srcs[i].Kind != "documents"}
		switch srcs[i].Kind {
		case "builtin":
			v.Where = "On this box"
		case "drafts":
			v.Where = "Working copies until you save them somewhere"
		case "folder":
			v.Where = srcs[i].Folder
		case "webdav":
			v.Where = srcs[i].URL
		case "nextcloud":
			if srcs[i].ViaCore {
				v.Where = "Nextcloud through Pocket Core"
			} else {
				v.Where = srcs[i].URL
			}
			if srcs[i].Folder != "" {
				v.Where += " · " + srcs[i].Folder
			}
		case "documents":
			if srcs[i].ViaCore {
				v.Where = "Pocket Documents through Pocket Core"
			} else {
				v.Where = srcs[i].URL
			}
		case "files":
			v.Where = "Pocket Files through Pocket Core"
			if srcs[i].Folder != "" {
				v.Where += " · " + srcs[i].Folder
			}
		}
		if v.Name == "" {
			v.Name = defaultSourceName(srcs[i].Kind)
		}
		out = append(out, v)
	}
	return out, nil
}

func defaultSourceName(kind string) string {
	switch kind {
	case "builtin":
		return "My files"
	case "drafts":
		return "Drafts"
	case "folder":
		return "Folder"
	case "webdav":
		return "WebDAV"
	case "nextcloud":
		return "Nextcloud"
	case "documents":
		return "Pocket Documents"
	case "files":
		return "Pocket Files"
	}
	return kind
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	out, err := s.sourceViews(r.Context(), me(r))
	if err != nil {
		s.fail(w, err, "sources")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSourceOptions says which kinds of source can be added here and what
// Core already knows (the person's Nextcloud account, Pocket Documents,
// Pocket Files).
func (s *Server) handleSourceOptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"core": s.coreReady(), "folder_roots": s.cfg.FolderRoots}
	if s.core.Configured() {
		email := userFrom(ctx).Email
		cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		if cred, err := s.core.ServiceForFresh(cctx, email, "nextcloud"); err == nil {
			out["core_nextcloud"] = map[string]any{"url": firstNonEmpty(cred.PublicURL, cred.URL), "user": cred.Fields["USERNAME"], "ok": cred.Fields["USERNAME"] != "" && firstNonEmpty(cred.Fields["APP_PASSWORD"], cred.Fields["PASSWORD"], cred.Key) != ""}
		} else {
			var le *core.LinkError
			msg := err.Error()
			if errors.As(err, &le) {
				msg = firstNonEmpty(linkReason("Nextcloud", le.Reason), le.Message)
			}
			out["core_nextcloud_error"] = msg
		}
		if on, why := s.core.LinkOn(cctx, email, "documents", "documents:read", "documents:write"); on {
			out["core_documents"] = true
		} else {
			out["core_documents_error"] = firstNonEmpty(linkReason("Pocket Documents", why), why)
		}
		if on, why := s.core.LinkOn(cctx, email, "files", filesScopes...); on {
			out["core_files"] = true
		} else {
			out["core_files_error"] = firstNonEmpty(linkReason("Pocket Files", why), why)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type sourceInput struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Secret   string `json:"secret"`
	Folder   string `json:"folder"`
	ViaCore  bool   `json:"via_core"`
	Position *int   `json:"position"`
}

func (s *Server) applySourceInput(src *store.Source, in sourceInput, isNew bool) error {
	if isNew {
		src.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	}
	src.Name = strings.TrimSpace(in.Name)
	switch src.Kind {
	case "builtin", "drafts":
		if isNew {
			return errors.New("that place is already there")
		}
	case "folder":
		p := filepath.Clean(strings.TrimSpace(in.Folder))
		if !filepath.IsAbs(p) || !s.folderAllowed(p) {
			if len(s.cfg.FolderRoots) == 0 {
				return errors.New("this box offers no server folders (FOLDER_ROOTS is empty)")
			}
			return fmt.Errorf("the folder must be under one of: %s", strings.Join(s.cfg.FolderRoots, ", "))
		}
		src.Folder, src.URL, src.Username, src.Secret, src.ViaCore = p, "", "", "", false
	case "webdav":
		u := strings.TrimRight(strings.TrimSpace(in.URL), "/")
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return errors.New("the WebDAV address must start with http:// or https://")
		}
		src.URL, src.Username, src.Folder, src.ViaCore = u, strings.TrimSpace(in.Username), "", false
		if in.Secret != "" {
			src.Secret = in.Secret
		}
	case "nextcloud":
		f, err := storage.Clean(strings.TrimSpace(in.Folder))
		if err != nil {
			return err
		}
		src.Folder, src.ViaCore = f, in.ViaCore
		if in.ViaCore {
			if !s.core.Configured() {
				return errors.New("there is no Pocket Core on this box; type the Nextcloud address and an app password instead")
			}
			src.URL, src.Username, src.Secret = "", "", ""
		} else {
			u := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(in.URL), "/"), "/index.php")
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				return errors.New("the Nextcloud address must start with http:// or https://")
			}
			src.URL, src.Username = u, strings.TrimSpace(in.Username)
			if src.Username == "" {
				return errors.New("your Nextcloud username is needed")
			}
			if in.Secret != "" {
				src.Secret = in.Secret
			}
			if src.Secret == "" {
				return errors.New("an app password is needed (Nextcloud → Settings → Security → Devices & sessions)")
			}
		}
	case "documents":
		src.ViaCore, src.Folder, src.Username = in.ViaCore, "", ""
		if in.ViaCore {
			if !s.core.Configured() {
				return errors.New("there is no Pocket Core on this box; type the Pocket Documents address and an API token instead")
			}
			src.URL, src.Secret = "", ""
		} else {
			u := strings.TrimRight(strings.TrimSpace(in.URL), "/")
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				return errors.New("the Pocket Documents address must start with http:// or https://")
			}
			src.URL = u
			if in.Secret != "" {
				src.Secret = in.Secret
			}
			if src.Secret == "" {
				return errors.New("a Pocket Documents API token is needed (Settings → API tokens there)")
			}
		}
	case "files":
		if !s.core.Configured() {
			return errors.New("there is no Pocket Core on this box; add Pocket Files as a WebDAV place with an app password instead")
		}
		f, err := storage.Clean(strings.TrimSpace(in.Folder))
		if err != nil {
			return err
		}
		src.Folder, src.ViaCore, src.URL, src.Username, src.Secret = f, true, "", "", ""
	default:
		return errors.New("unknown source kind")
	}
	if in.Position != nil {
		src.Position = *in.Position
	}
	return nil
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in sourceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	src := &store.Source{UserID: me(r)}
	if err := s.applySourceInput(src, in, true); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := s.backendFor(ctx, src)
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := b.Test(tctx); err != nil {
		s.fail(w, err, "the source does not answer")
		return
	}
	if err := s.store.CreateSource(ctx, src); err != nil {
		s.fail(w, err, "create source")
		return
	}
	go s.reportModes(context.Background())
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.GetSource(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	var in sourceInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Kind = src.Kind
	if err := s.applySourceInput(src, in, false); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateSource(ctx, src); err != nil {
		s.fail(w, err, "update source")
		return
	}
	go s.reportModes(context.Background())
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	src, err := s.store.GetSource(ctx, me(r), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "source")
		return
	}
	if src.Kind == "builtin" || src.Kind == "drafts" {
		writeErr(w, http.StatusBadRequest, "the built-in places cannot be removed")
		return
	}
	if err := s.store.DeleteSource(ctx, me(r), src.ID); err != nil {
		s.fail(w, err, "delete source")
		return
	}
	go s.reportModes(context.Background())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestSource(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	tctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	start := time.Now()
	if err := b.Test(tctx); err != nil {
		s.fail(w, err, "the source does not answer")
		return
	}
	es, err := b.ListDir(tctx, "")
	if err != nil {
		s.fail(w, err, "listing the folder")
		return
	}
	files, folders := 0, 0
	for _, e := range es {
		if e.IsDir {
			folders++
		} else {
			files++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "where": b.Describe(), "files": files, "folders": folders, "ms": time.Since(start).Milliseconds(), "source": src.ID})
}

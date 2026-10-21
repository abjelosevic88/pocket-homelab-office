// Package server wires the HTTP API, auth, Document Server and the embedded SPA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/crypto/bcrypt"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/config"
	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Version is injected at build time.
var Version = "dev"

// Server holds dependencies.
type Server struct {
	cfg      *config.Config
	store    *store.Store
	log      *slog.Logger
	webFS    fs.FS
	started  time.Time
	requests atomic.Int64
	errors   atomic.Int64

	core    *core.Client
	coreMu  sync.Mutex
	coreOK  bool
	coreErr string

	oidc   *oidcProvider
	oidcMu sync.Mutex

	saveMu sync.Mutex // one Document Server save at a time per key
	saves  map[string]*sync.Mutex

	co   *office.Collabora
	coMu sync.Mutex
}

// New creates a server.
func New(cfg *config.Config, st *store.Store, log *slog.Logger, webFS fs.FS) *Server {
	return &Server{cfg: cfg, store: st, log: log, webFS: webFS, started: time.Now(),
		core:  core.New(cfg.CoreURL, core.Options{DB: st.DB, TokenFile: cfg.DataDir + "/core.token", UserAgent: "pocket-office/" + Version}),
		oidc:  &oidcProvider{issuerURL: cfg.OIDCIssuer},
		saves: map[string]*sync.Mutex{}}
}

// Bootstrap seeds the first admin (if configured) and starts the Core registration loop.
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.core.Start(ctx); err != nil {
		return err
	}
	n, err := s.store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n == 0 && s.cfg.AuthMode == "local" && s.cfg.AdminEmail != "" && s.cfg.AdminPassword != "" {
		if len(s.cfg.AdminPassword) < 8 {
			return errors.New("ADMIN_PASSWORD must be at least 8 characters")
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(s.cfg.AdminPassword), bcrypt.DefaultCost)
		u := &store.User{Email: s.cfg.AdminEmail, Name: s.cfg.AdminName, PasswordHash: string(h), Role: "admin"}
		if err := s.store.CreateUser(ctx, u); err != nil {
			return err
		}
		s.log.Info("admin user created from environment", "email", u.Email)
	}
	_ = s.store.PurgeSessions(ctx)
	s.store.PurgeKeys(ctx)
	if s.core.Configured() {
		go s.coreLoop(ctx)
	}
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = s.store.PurgeSessions(ctx)
				s.store.PurgeKeys(ctx)
			}
		}
	}()
	return nil
}

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(10 * time.Minute))

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}
	r.Post("/api/v1/client-error", s.handleClientError)

	// Document Server talks to these without a session: a per-document
	// token in the file address, a JWT on the callback.
	r.Get("/api/v1/oo/file/{key}", s.handleFetchFile)
	r.Post("/api/v1/oo/callback/{key}", s.handleCallback)
	// Collabora Online talks WOPI; the access token is in the query.
	r.Get("/wopi/files/{key}", s.handleWOPICheckFileInfo)
	r.Get("/wopi/files/{key}/contents", s.handleWOPIGetFile)
	r.Post("/wopi/files/{key}/contents", s.handleWOPIPutFile)
	r.Post("/wopi/files/{key}", s.handleWOPIFileOps)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)
			r.Get("/auth/tokens", s.handleListTokens)
			r.Post("/auth/tokens", s.handleCreateToken)
			r.Delete("/auth/tokens/{id}", s.handleDeleteToken)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/formats", s.handleFormats)
			r.Get("/recent", s.handleRecent)
			r.Delete("/recent", s.handleForgetRecent)
			r.Get("/search", s.handleSearch)

			r.Post("/drafts", s.handleUploadDraft)
			r.Post("/drafts/save", s.handleSaveDraft)

			r.Get("/sources", s.handleListSources)
			r.Post("/sources", s.handleCreateSource)
			r.Get("/sources/options", s.handleSourceOptions)
			r.Put("/sources/{id}", s.handleUpdateSource)
			r.Delete("/sources/{id}", s.handleDeleteSource)
			r.Post("/sources/{id}/test", s.handleTestSource)
			r.Get("/sources/{id}/ls", s.handleListDir)
			r.Get("/sources/{id}/file", s.handleDownload)
			r.Get("/sources/{id}/export", s.handleExport)
			r.Post("/sources/{id}/upload", s.handleUpload)
			r.Post("/sources/{id}/new", s.handleNewFile)
			r.Post("/sources/{id}/mkdir", s.handleMkdir)
			r.Post("/sources/{id}/rename", s.handleRename)
			r.Post("/sources/{id}/copy", s.handleCopy)
			r.Post("/sources/{id}/delete", s.handleDelete)
			r.Post("/sources/{id}/open", s.handleOpen)
			r.Post("/sources/{id}/edit-copy", s.handleEditCopy)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Post("/settings/documentserver/test", s.handleTestDocumentServer)
			r.Post("/settings/collabora/test", s.handleTestCollabora)
			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)
			r.Get("/system", s.handleSystemInfo)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// fail maps storage and store errors onto HTTP statuses with a readable message.
func (s *Server) fail(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, what+": not found")
	case errors.Is(err, storage.ErrExists):
		writeErr(w, http.StatusConflict, what+": a file or folder with that name is already there")
	case errors.Is(err, storage.ErrPrecondition):
		writeErr(w, http.StatusConflict, what+": the file changed somewhere else meanwhile")
	case errors.Is(err, storage.ErrAuth):
		writeErr(w, http.StatusBadGateway, what+": the storage refused the sign-in ("+err.Error()+")")
	case errors.Is(err, storage.ErrLocked):
		writeErr(w, http.StatusLocked, what+": the file is locked by another editor")
	case errors.Is(err, storage.ErrInvalidPath):
		writeErr(w, http.StatusBadRequest, what+": invalid path")
	case errors.Is(err, storage.ErrReadOnly):
		writeErr(w, http.StatusForbidden, what+": "+err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeErr(w, http.StatusGatewayTimeout, what+": the storage did not answer in time")
	default:
		var nc errNotConnected
		if errors.As(err, &nc) {
			writeErr(w, http.StatusBadGateway, nc.Error())
			return
		}
		s.log.Error(what, "err", err)
		writeErr(w, http.StatusInternalServerError, what+": "+err.Error())
	}
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

// ---------- Pocket Core ----------

// coreLoop registers with Core on start (retrying until it answers) and
// re-registers every 30 minutes so version and last-seen stay fresh.
func (s *Server) coreLoop(ctx context.Context) {
	wait := 5 * time.Second
	for {
		err := s.registerWithCore(ctx)
		s.coreMu.Lock()
		s.coreOK, s.coreErr = err == nil, ""
		if err != nil {
			s.coreErr = err.Error()
		}
		s.coreMu.Unlock()
		if err != nil {
			s.log.Warn("pocket core: registration failed, retrying", "err", err, "in", wait.String())
		} else {
			wait = 30 * time.Minute
			s.reportModes(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if err != nil && wait < 5*time.Minute {
			wait *= 2
		}
	}
}

func (s *Server) manifest() core.Manifest {
	m := core.Manifest{
		Slug: "office", Name: "Pocket Office",
		Description: "Word, Excel and PowerPoint files opened and edited in the browser with ONLYOFFICE, from Nextcloud, Pocket Documents, WebDAV or a folder on the box.",
		Icon:        "/favicon.svg", Version: Version,
		URL: s.cfg.CoreAppURL, PublicURL: s.cfg.BaseURL, HealthPath: "/healthz", EnvPrefix: "OFFICE",
		Publishes: []string{"office.opened", "office.saved", "office.created"},
		Offers:    linkOffers,
		Uses:      linkUses,
		Services:  serviceUses,
	}
	if s.cfg.BaseURL != "" {
		m.OIDC = &core.OIDCRequest{RedirectURIs: []string{s.cfg.BaseURL + "/api/v1/auth/oidc/callback"}}
	}
	return m
}

func (s *Server) registerWithCore(ctx context.Context) error {
	reg, err := s.core.Register(ctx, s.cfg.CoreRegistrationKey, s.manifest())
	if err != nil {
		return err
	}
	if reg.Token != "" {
		s.log.Info("pocket core: registered, app token stored", "core", s.cfg.CoreURL)
	} else {
		s.log.Debug("pocket core: re-registered", "core", s.cfg.CoreURL)
	}
	return nil
}

// publish sends an event to Core's bus without holding the request up.
func (s *Server) publish(topic, subject string, payload map[string]any) {
	if !s.core.Configured() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.core.Publish(ctx, topic, subject, payload); err != nil {
			s.log.Debug("pocket core: publish", "topic", topic, "err", err)
		}
	}()
}

func (s *Server) corePublicURL() string {
	if s.cfg.CorePublicURL != "" {
		return s.cfg.CorePublicURL
	}
	if o := s.core.OIDC(); o.Issuer != "" {
		return strings.TrimRight(o.Issuer, "/")
	}
	return s.cfg.CoreURL
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	counts := s.store.Counts(r.Context())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_office_info Build info.\n# TYPE pocket_office_info gauge\npocket_office_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_office_uptime_seconds Process uptime.\n# TYPE pocket_office_uptime_seconds gauge\npocket_office_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_office_http_requests_total HTTP requests served.\n# TYPE pocket_office_http_requests_total counter\npocket_office_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_office_http_errors_total HTTP 5xx responses.\n# TYPE pocket_office_http_errors_total counter\npocket_office_http_errors_total %d\n", s.errors.Load())
	for k, v := range counts {
		fmt.Fprintf(w, "# TYPE pocket_office_rows{table=%q} gauge\npocket_office_rows{table=%q} %d\n", k, k, v)
	}
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	s.coreMu.Lock()
	ok, errStr := s.coreOK, s.coreErr
	s.coreMu.Unlock()
	ds := s.docServer(r.Context())
	co := s.collabora(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "uptime_s": int(time.Since(s.started).Seconds()), "base_url": s.cfg.BaseURL,
		"core":            map[string]any{"configured": s.core.Configured(), "url": s.cfg.CoreURL, "public_url": s.corePublicURL(), "registered": ok, "error": errStr},
		"document_server": map[string]any{"configured": ds.Configured(), "url": ds.PublicURL},
		"collabora":       map[string]any{"configured": co.Configured(), "url": co.PublicURL},
		"engines":         s.engines(r.Context()),
	})
}

// handleClientError logs a crash the web app reports, so a blank page leaves a trace.
func (s *Server) handleClientError(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message string `json:"message"`
		Stack   string `json:"stack"`
		Where   string `json:"where"`
	}
	_ = decode(r, &in)
	if in.Message != "" {
		s.log.Warn("web app crashed", "where", in.Where, "message", in.Message, "stack", in.Stack)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFormats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, office.All())
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/wopi/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

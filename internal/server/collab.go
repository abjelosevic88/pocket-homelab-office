package server

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Pocket Review: any file can be sent for review as a PDF. The review
// remembers a stable source reference, so a file sent again becomes a new
// revision and the file list shows an "in review" badge. Reached through a
// Pocket Core app link (reviews:read, reviews:write) as the person.

var collabScopes = []string{"reviews:read", "reviews:write"}

type collabLink struct {
	base, public, token string
}

type collabHit struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	Revisions   int    `json:"revisions"`
	OpenThreads int    `json:"open_threads"`
	OpenIssues  int    `json:"open_issues"`
	UpdatedAt   string `json:"updated_at"`
	MyRole      string `json:"my_role"`
}

func (s *Server) collabFor(ctx context.Context, uid int64) (*collabLink, error) {
	if !s.core.Configured() {
		return nil, errNotConnected{Name: "Pocket Review", Msg: "there is no Pocket Core on this box"}
	}
	lt, err := s.core.ActAs(ctx, s.userEmail(ctx, uid), "review", collabScopes...)
	if err != nil {
		var le *core.LinkError
		if errors.As(err, &le) {
			return nil, errNotConnected{Name: "Pocket Review", Msg: "Pocket Review through Pocket Core: " + firstNonEmpty(linkReason("Pocket Review", le.Reason), le.Message)}
		}
		return nil, fmt.Errorf("Pocket Review through Pocket Core: %w", err)
	}
	return &collabLink{base: strings.TrimRight(lt.URL, "/"), public: strings.TrimRight(firstNonEmpty(lt.PublicURL, lt.URL), "/"), token: lt.AccessToken}, nil
}

// collabInfo tells the UI whether sending for review is possible for a person.
func (s *Server) collabInfo(ctx context.Context, uid int64) map[string]any {
	if !s.core.Configured() {
		return map[string]any{"available": false}
	}
	cctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	l, err := s.collabFor(cctx, uid)
	if err != nil {
		return map[string]any{"available": false, "error": err.Error()}
	}
	return map[string]any{"available": true, "url": l.public}
}

func (l *collabLink) do(ctx context.Context, method, path string, body io.Reader, ctype string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, l.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+l.token)
	req.Header.Set("Accept", "application/json")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("pocket review: %s", firstNonEmpty(e.Error, http.StatusText(res.StatusCode)))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// sourceRef names a file stably across the apps: a Pocket Documents document
// is "documents:<id>" (the same reference Documents itself uses), a draft is
// its own thing until it is saved, anything else is its canonical address.
func (s *Server) sourceRef(src *store.Source, b storage.Backend, p string) string {
	switch src.Kind {
	case "documents":
		id, _ := splitDocPath(p)
		return "documents:" + strconv.FormatInt(id, 10)
	case "drafts":
		return fmt.Sprintf("office:draft:%d:%s", src.UserID, p)
	}
	sum := sha1.Sum([]byte(b.Canonical(p)))
	return "office:" + hex.EncodeToString(sum[:12])
}

func (s *Server) sourceURL(src *store.Source, b storage.Backend, p string) string {
	if d, ok := b.(*docsBackend); ok {
		return d.Link(p)
	}
	return s.fileURL(src.ID, p)
}

func (s *Server) sourceLabel(src *store.Source, p string) string {
	if src.Kind == "documents" {
		_, name := splitDocPath(p)
		return "Pocket Documents · " + name
	}
	return firstNonEmpty(src.Name, defaultSourceName(src.Kind)) + " · " + p
}

// decorateReviews attaches review badges to the files Pocket Review knows.
func (s *Server) decorateReviews(ctx context.Context, uid int64, src *store.Source, b storage.Backend, files []fileView) {
	if !s.core.Configured() || len(files) == 0 {
		return
	}
	refs := map[string]int{}
	q := url.Values{}
	for i := range files {
		if files[i].IsDir || !files[i].Opens {
			continue
		}
		ref := s.sourceRef(src, b, files[i].Path)
		refs[ref] = i
		q.Add("ref", ref)
		if len(refs) >= 400 {
			break
		}
	}
	if len(refs) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	l, err := s.collabFor(cctx, uid)
	if err != nil {
		return
	}
	var ans struct {
		Reviews map[string]collabHit `json:"reviews"`
	}
	if err := l.do(cctx, http.MethodGet, "/api/v1/reviews/lookup?"+q.Encode(), nil, "", &ans); err != nil {
		s.log.Debug("review lookup", "err", err)
		return
	}
	for ref, hit := range ans.Reviews {
		if i, ok := refs[ref]; ok {
			h := hit
			files[i].Review = &h
		}
	}
}

// handleCollabStatus says whether one file is in review (and whether sending is possible).
func (s *Server) handleCollabStatus(w http.ResponseWriter, r *http.Request) {
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	p, err := storage.Clean(r.URL.Query().Get("path"))
	if err != nil || p == "" {
		writeErr(w, http.StatusBadRequest, "a file path is needed")
		return
	}
	out := s.collabInfo(r.Context(), me(r))
	fv := fileView{Entry: storage.Entry{Path: p, Name: storage.Base(p)}, Opens: office.Opens(p)}
	s.decorateReviews(r.Context(), me(r), src, b, []fileView{fv})
	out["review"] = fv.Review
	out["ref"] = s.sourceRef(src, b, p)
	writeJSON(w, http.StatusOK, out)
}

// toPDF returns the file as PDF: as is, or through Document Server's
// conversion service, else Collabora's convert-to.
func (s *Server) toPDF(ctx context.Context, srcID int64, b storage.Backend, p string) ([]byte, storage.Entry, error) {
	if office.Ext(p) == "pdf" {
		return b.Read(ctx, p)
	}
	if s.docServer(ctx).Configured() && office.CanExport(p, "pdf") {
		return s.convert(ctx, srcID, b, p, "pdf")
	}
	co := s.collabora(ctx)
	if !co.Configured() {
		return nil, storage.Entry{}, errors.New("no engine can turn ." + office.Ext(p) + " into a PDF")
	}
	data, e, err := b.Read(ctx, p)
	if err != nil {
		return nil, storage.Entry{}, err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("data", e.Name)
	_, _ = part.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(firstNonEmpty(co.InternalURL, co.PublicURL), "/")+"/cool/convert-to/pdf", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return nil, storage.Entry{}, fmt.Errorf("collabora convert-to: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, storage.Entry{}, fmt.Errorf("collabora convert-to: HTTP %d", res.StatusCode)
	}
	pdf, err := io.ReadAll(io.LimitReader(res.Body, 1<<30))
	return pdf, e, err
}

// handleCollabSend sends a file for review: a new review, or a new revision
// of the review this file already has.
func (s *Server) handleCollabSend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	src, b, ok := s.sourceAndBackend(w, r)
	if !ok {
		return
	}
	var in struct {
		Path        string `json:"path"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Note        string `json:"note"`
		DueDate     string `json:"due_date"`
		Mode        string `json:"mode"` // "" | new | revision
		Start       bool   `json:"start"`
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
	l, err := s.collabFor(ctx, u.ID)
	if err != nil {
		s.fail(w, err, "review")
		return
	}
	// an open editor may hold newer bytes than storage: ask Document Server first
	s.flushOpenDocument(ctx, b.Canonical(p))
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	pdf, e, err := s.toPDF(cctx, src.ID, b, p)
	if err != nil {
		s.fail(w, err, "making a PDF of "+storage.Base(p))
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSuffix(e.Name, "."+office.Ext(e.Name))
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fields := map[string]string{
		"title": title, "description": strings.TrimSpace(in.Description), "note": strings.TrimSpace(in.Note), "due_date": strings.TrimSpace(in.DueDate), "mode": in.Mode,
		"source_app": "office", "source_ref": s.sourceRef(src, b, p), "source_url": s.sourceURL(src, b, p), "source_label": s.sourceLabel(src, p),
	}
	if in.Start {
		fields["start"] = "1"
	}
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	part, _ := mw.CreateFormFile("file", strings.TrimSuffix(e.Name, "."+office.Ext(e.Name))+".pdf")
	_, _ = part.Write(pdf)
	_ = mw.Close()
	var ans struct {
		Review struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"review"`
		Revision struct {
			Number int `json:"number"`
		} `json:"revision"`
		Created bool   `json:"created"`
		URL     string `json:"url"`
	}
	if err := l.do(cctx, http.MethodPost, "/api/v1/reviews/import", &buf, mw.FormDataContentType(), &ans); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.publish("office.sent_for_review", fmt.Sprintf("file:%d:%s", src.ID, p), map[string]any{"user_email": u.Email, "name": e.Name, "path": p, "source": src.Kind, "review": ans.Review.ID, "revision": ans.Revision.Number, "created": ans.Created, "url": ans.URL})
	writeJSON(w, http.StatusOK, map[string]any{"review": ans.Review, "revision": ans.Revision.Number, "created": ans.Created, "url": firstNonEmpty(ans.URL, l.public+"/reviews/"+strconv.FormatInt(ans.Review.ID, 10))})
}

// moveReviewSource keeps a review pointing at a draft that was just saved
// into a place (new reference, link and label).
func (s *Server) moveReviewSource(ctx context.Context, uid int64, from *store.Source, fb storage.Backend, fromPath string, to *store.Source, tb storage.Backend, toPath string) {
	if !s.core.Configured() {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	l, err := s.collabFor(cctx, uid)
	if err != nil {
		return
	}
	oldRef := s.sourceRef(from, fb, fromPath)
	var ans struct {
		Reviews map[string]collabHit `json:"reviews"`
	}
	if err := l.do(cctx, http.MethodGet, "/api/v1/reviews/lookup?ref="+url.QueryEscape(oldRef), nil, "", &ans); err != nil {
		return
	}
	hit, ok := ans.Reviews[oldRef]
	if !ok {
		return
	}
	body, _ := json.Marshal(map[string]string{"source_ref": s.sourceRef(to, tb, toPath), "source_url": s.sourceURL(to, tb, toPath), "source_label": s.sourceLabel(to, toPath)})
	if err := l.do(cctx, http.MethodPut, "/api/v1/reviews/"+strconv.FormatInt(hit.ID, 10), bytes.NewReader(body), "application/json", nil); err != nil {
		s.log.Debug("review: move source", "err", err)
	}
}

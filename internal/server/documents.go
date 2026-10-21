package server

import (
	"bytes"
	"context"
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

// Pocket Documents as a source: the archive is flat, so it shows as one
// folder of every document Document Server can open. Paths are
// "<id>/<file name>" so names stay readable; opening downloads the original
// (never the OCR'd PDF/A twin) and a save replaces the original through the
// archive's own API, which keeps the previous file as a version and runs
// OCR again.
type docsBackend struct {
	base, public, token, via string
	http                     *http.Client
}

var documentsScopes = []string{"documents:read", "documents:write"}

func (s *Server) documentsBackend(ctx context.Context, src *store.Source) (storage.Backend, error) {
	b := &docsBackend{http: &http.Client{Timeout: 120 * time.Second}}
	if src.ViaCore {
		if !s.core.Configured() {
			return nil, errNotConnected{Name: "Pocket Documents", Msg: "there is no Pocket Core on this box; type the Pocket Documents address and an API token instead"}
		}
		lt, err := s.core.ActAs(ctx, s.userEmail(ctx, src.UserID), "documents", documentsScopes...)
		if err != nil {
			var le *core.LinkError
			if errors.As(err, &le) {
				return nil, errNotConnected{Name: "Pocket Documents", Msg: "Pocket Documents through Pocket Core: " + firstNonEmpty(linkReason("Pocket Documents", le.Reason), le.Message)}
			}
			return nil, fmt.Errorf("Pocket Documents through Pocket Core: %w", err)
		}
		b.base, b.public, b.token, b.via = strings.TrimRight(lt.URL, "/"), strings.TrimRight(firstNonEmpty(lt.PublicURL, lt.URL), "/"), lt.AccessToken, "core"
		return b, nil
	}
	if src.URL == "" || src.Secret == "" {
		return nil, errNotConnected{Name: "Pocket Documents", Msg: "the Pocket Documents source needs an address and an API token"}
	}
	b.base, b.public, b.token, b.via = src.URL, src.URL, src.Secret, "direct"
	return b, nil
}

func (d *docsBackend) Kind() string     { return "documents" }
func (d *docsBackend) Describe() string { return "Pocket Documents" }
func (d *docsBackend) Canonical(p string) string {
	id, _ := splitDocPath(p)
	return "documents:" + d.public + ":" + strconv.FormatInt(id, 10)
}

type pdDocument struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	OriginalName string `json:"original_name"`
	Mime         string `json:"mime"`
	Checksum     string `json:"checksum"`
	Size         int64  `json:"size"`
	ModifiedAt   string `json:"modified_at"`
	AddedAt      string `json:"added_at"`
	Created      string `json:"created"`
	Status       string `json:"status"`
}

func splitDocPath(p string) (int64, string) {
	c, _ := storage.Clean(p)
	id, name, _ := strings.Cut(c, "/")
	n, _ := strconv.ParseInt(id, 10, 64)
	return n, name
}

// name is the file name a document shows as: its title with the original extension.
func (doc *pdDocument) name() string {
	ext := office.Ext(doc.OriginalName)
	base := strings.TrimSpace(doc.Title)
	if base == "" {
		base = strings.TrimSuffix(doc.OriginalName, "."+ext)
	}
	base = strings.NewReplacer("/", "-", "\\", "-").Replace(base)
	if ext == "" {
		return base
	}
	return base + "." + ext
}

func (doc *pdDocument) entry() storage.Entry {
	name := doc.name()
	e := storage.Entry{Path: strconv.FormatInt(doc.ID, 10) + "/" + name, Name: name, Size: doc.Size, ETag: doc.Checksum, FileID: strconv.FormatInt(doc.ID, 10)}
	for _, t := range []string{doc.ModifiedAt, doc.AddedAt} {
		if ts, err := time.Parse(time.RFC3339Nano, t); err == nil {
			e.Modified = ts.UTC()
			break
		}
	}
	return e
}

func (d *docsBackend) do(ctx context.Context, method, path string, body io.Reader, ctype string, out any) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "PocketOffice/"+Version)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		switch res.StatusCode {
		case http.StatusUnauthorized:
			return nil, storage.ErrAuth
		case http.StatusForbidden:
			return nil, fmt.Errorf("%w: Pocket Documents refused: %s", storage.ErrReadOnly, firstNonEmpty(e.Error, "no permission"))
		case http.StatusNotFound:
			return nil, storage.ErrNotFound
		}
		return nil, fmt.Errorf("pocket documents: %s", firstNonEmpty(e.Error, http.StatusText(res.StatusCode)))
	}
	if out != nil {
		defer res.Body.Close()
		return res, json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(out)
	}
	return res, nil
}

func (d *docsBackend) Test(ctx context.Context) error {
	_, err := d.do(ctx, http.MethodGet, "/api/v1/documents?limit=1", nil, "", &struct{}{})
	return err
}

// list returns the documents Document Server can open, newest first.
func (d *docsBackend) list(ctx context.Context, q string) ([]pdDocument, error) {
	var out []pdDocument
	for offset := 0; offset < 5000; offset += 500 {
		v := url.Values{"limit": {"500"}, "offset": {strconv.Itoa(offset)}, "sort": {"-modified"}}
		if q != "" {
			v.Set("q", q)
		}
		var page struct {
			Results []pdDocument `json:"results"`
			Count   int          `json:"count"`
		}
		if _, err := d.do(ctx, http.MethodGet, "/api/v1/documents?"+v.Encode(), nil, "", &page); err != nil {
			return nil, err
		}
		for _, doc := range page.Results {
			if office.Opens(doc.OriginalName) {
				out = append(out, doc)
			}
		}
		if len(page.Results) < 500 {
			break
		}
	}
	return out, nil
}

func (d *docsBackend) ListDir(ctx context.Context, dir string) ([]storage.Entry, error) {
	if c, _ := storage.Clean(dir); c != "" {
		return nil, storage.ErrNotFound
	}
	docs, err := d.list(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]storage.Entry, 0, len(docs))
	for i := range docs {
		out = append(out, docs[i].entry())
	}
	return out, nil
}

func (d *docsBackend) get(ctx context.Context, p string) (*pdDocument, error) {
	id, _ := splitDocPath(p)
	if id == 0 {
		return nil, storage.ErrNotFound
	}
	var doc pdDocument
	if _, err := d.do(ctx, http.MethodGet, "/api/v1/documents/"+strconv.FormatInt(id, 10), nil, "", &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (d *docsBackend) Stat(ctx context.Context, p string) (storage.Entry, error) {
	doc, err := d.get(ctx, p)
	if err != nil {
		return storage.Entry{}, err
	}
	return doc.entry(), nil
}

func (d *docsBackend) Read(ctx context.Context, p string) ([]byte, storage.Entry, error) {
	doc, err := d.get(ctx, p)
	if err != nil {
		return nil, storage.Entry{}, err
	}
	res, err := d.do(ctx, http.MethodGet, "/api/v1/documents/"+strconv.FormatInt(doc.ID, 10)+"/download?original=true", nil, "", nil)
	if err != nil {
		return nil, storage.Entry{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<30))
	if err != nil {
		return nil, storage.Entry{}, err
	}
	return data, doc.entry(), nil
}

// Write replaces the document's file; the archive keeps the old one as a version.
func (d *docsBackend) Write(ctx context.Context, p string, data []byte, ifMatch string) (storage.Entry, error) {
	doc, err := d.get(ctx, p)
	if err != nil {
		return storage.Entry{}, err
	}
	if ifMatch != "" && doc.Checksum != "" && doc.Checksum != ifMatch {
		return storage.Entry{}, storage.ErrPrecondition
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("document", doc.name())
	_, _ = part.Write(data)
	_ = mw.Close()
	var updated pdDocument
	if _, err := d.do(ctx, http.MethodPost, "/api/v1/documents/"+strconv.FormatInt(doc.ID, 10)+"/replace", &buf, mw.FormDataContentType(), &updated); err != nil {
		return storage.Entry{}, err
	}
	if updated.ID == 0 {
		updated = *doc
	}
	return updated.entry(), nil
}

// Create uploads a new document; its path is known once the archive answers.
func (d *docsBackend) Create(ctx context.Context, p string, data []byte) (storage.Entry, error) {
	_, name := splitDocPath(p)
	if name == "" {
		name = storage.Base(p)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", strings.TrimSuffix(name, "."+office.Ext(name)))
	_ = mw.WriteField("source", "office")
	part, _ := mw.CreateFormFile("document", name)
	_, _ = part.Write(data)
	_ = mw.Close()
	var ans any
	if _, err := d.do(ctx, http.MethodPost, "/api/v1/documents", &buf, mw.FormDataContentType(), &ans); err != nil {
		return storage.Entry{}, err
	}
	// the answer is a document, or a list of {file, document}
	var doc pdDocument
	raw, _ := json.Marshal(ans)
	switch v := ans.(type) {
	case map[string]any:
		if inner, ok := v["document"]; ok {
			raw, _ = json.Marshal(inner)
		}
	case []any:
		if len(v) > 0 {
			if m, ok := v[0].(map[string]any); ok {
				if inner, ok := m["document"]; ok {
					raw, _ = json.Marshal(inner)
				} else {
					raw, _ = json.Marshal(m)
				}
			}
		}
	}
	_ = json.Unmarshal(raw, &doc)
	if doc.ID == 0 {
		return storage.Entry{}, errors.New("pocket documents did not return the new document")
	}
	if doc.OriginalName == "" {
		doc.OriginalName = name
	}
	return doc.entry(), nil
}

// Move renames the document (its title); it cannot be moved into folders.
func (d *docsBackend) Move(ctx context.Context, from, to string) (storage.Entry, error) {
	doc, err := d.get(ctx, from)
	if err != nil {
		return storage.Entry{}, err
	}
	name := storage.Base(to)
	title := strings.TrimSuffix(name, "."+office.Ext(name))
	body, _ := json.Marshal(map[string]any{"title": title})
	var updated pdDocument
	if _, err := d.do(ctx, http.MethodPatch, "/api/v1/documents/"+strconv.FormatInt(doc.ID, 10), bytes.NewReader(body), "application/json", &updated); err != nil {
		return storage.Entry{}, err
	}
	if updated.ID == 0 {
		updated = *doc
		updated.Title = title
	}
	return updated.entry(), nil
}

func (d *docsBackend) Delete(ctx context.Context, p string) error {
	doc, err := d.get(ctx, p)
	if err != nil {
		return err
	}
	res, err := d.do(ctx, http.MethodDelete, "/api/v1/documents/"+strconv.FormatInt(doc.ID, 10), nil, "", nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (d *docsBackend) Mkdir(ctx context.Context, p string) error {
	return fmt.Errorf("%w: Pocket Documents has no folders", storage.ErrReadOnly)
}

// Link is the document's page in Pocket Documents.
func (d *docsBackend) Link(p string) string {
	id, _ := splitDocPath(p)
	return d.public + "/documents/" + strconv.FormatInt(id, 10)
}

package office

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
)

// Server is one Document Server as this app reaches it.
type Server struct {
	// PublicURL is what browsers load the editor from.
	PublicURL string
	// InternalURL is how this server calls it (conversion, health); defaults to PublicURL.
	InternalURL string
	Secret      string
	Header      string
	HTTP        *http.Client
}

func (d *Server) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return &http.Client{Timeout: 120 * time.Second}
}

func (d *Server) internal() string {
	if d.InternalURL != "" {
		return d.InternalURL
	}
	return d.PublicURL
}

// Configured reports whether an address is set.
func (d *Server) Configured() bool { return d != nil && d.PublicURL != "" }

// APIScript is the editor script browsers load.
func (d *Server) APIScript() string {
	return d.PublicURL + "/web-apps/apps/api/documents/api.js"
}

// Health asks Document Server's /healthcheck.
func (d *Server) Health(ctx context.Context) error {
	if !d.Configured() {
		return errors.New("no Document Server address")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.internal()+"/healthcheck", nil)
	res, err := d.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
	if res.StatusCode != http.StatusOK || strings.TrimSpace(string(b)) != "true" {
		return fmt.Errorf("healthcheck answered HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// Rewrite turns an address Document Server handed out (its public address)
// into one this server can fetch from, when the two differ.
func (d *Server) Rewrite(u string) string {
	if d.InternalURL != "" && d.PublicURL != "" && d.InternalURL != d.PublicURL && strings.HasPrefix(u, d.PublicURL+"/") {
		return d.InternalURL + strings.TrimPrefix(u, d.PublicURL)
	}
	return u
}

// Convert asks the conversion service to turn the file at url (reachable by
// Document Server) from one type into another and returns the result's
// bytes. key must differ per source file version.
func (d *Server) Convert(ctx context.Context, url, from, to, key, title string) ([]byte, error) {
	if !d.Configured() {
		return nil, errors.New("no Document Server address")
	}
	body := map[string]any{"async": false, "filetype": from, "outputtype": to, "key": key, "title": title, "url": url}
	if d.Secret != "" {
		tok, err := Sign(d.Secret, body)
		if err != nil {
			return nil, err
		}
		body["token"] = tok
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.internal()+"/converter", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if d.Secret != "" {
		hdr, _ := Sign(d.Secret, map[string]any{"payload": body})
		req.Header.Set(d.header(), "Bearer "+hdr)
	}
	res, err := d.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out struct {
		EndConvert bool   `json:"endConvert"`
		FileURL    string `json:"fileUrl"`
		Percent    int    `json:"percent"`
		Error      int    `json:"error"`
	}
	ans, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err := json.Unmarshal(ans, &out); err != nil {
		return nil, fmt.Errorf("conversion service answered HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(ans)))
	}
	if out.Error != 0 {
		return nil, fmt.Errorf("conversion failed: %s", convertError(out.Error))
	}
	if !out.EndConvert || out.FileURL == "" {
		return nil, errors.New("conversion did not finish")
	}
	freq, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.Rewrite(out.FileURL), nil)
	fres, err := d.client().Do(freq)
	if err != nil {
		return nil, err
	}
	defer fres.Body.Close()
	if fres.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching the converted file: HTTP %d", fres.StatusCode)
	}
	return io.ReadAll(io.LimitReader(fres.Body, 1<<30))
}

func (d *Server) header() string {
	if d.Header != "" {
		return d.Header
	}
	return "AuthorizationJwt"
}

func convertError(code int) string {
	switch code {
	case -1:
		return "unknown error"
	case -2:
		return "timeout"
	case -3:
		return "conversion error"
	case -4:
		return "Document Server could not download the file"
	case -5:
		return "the file's password is unknown"
	case -6:
		return "database error"
	case -7:
		return "input error"
	case -8:
		return "invalid token"
	case -9:
		return "the output type cannot be determined"
	case -10:
		return "the file is too large"
	}
	return fmt.Sprintf("code %d", code)
}

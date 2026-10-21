package office

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Collabora Online (CODE) is the second engine: it renders on the server
// and talks WOPI to this app. Its discovery document says which action URL
// opens which file type.
type Collabora struct {
	PublicURL   string
	InternalURL string
	HTTP        *http.Client

	mu       sync.Mutex
	actions  map[string]map[string]string // ext → action name → urlsrc (public)
	fetched  time.Time
	fetchErr error
}

func (c *Collabora) Configured() bool { return c != nil && c.PublicURL != "" }

func (c *Collabora) internal() string {
	if c.InternalURL != "" {
		return c.InternalURL
	}
	return c.PublicURL
}

func (c *Collabora) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

type discovery struct {
	Apps []struct {
		Name    string `xml:"name,attr"`
		Actions []struct {
			Name   string `xml:"name,attr"`
			Ext    string `xml:"ext,attr"`
			URLSrc string `xml:"urlsrc,attr"`
		} `xml:"action"`
	} `xml:"net-zone>app"`
}

// Discover reads hosting/discovery (cached for an hour). Action URLs are
// rewritten to the public address, since the internal address answers with
// itself as the host.
func (c *Collabora) Discover(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.actions != nil && time.Since(c.fetched) < time.Hour {
		return nil
	}
	if c.fetchErr != nil && time.Since(c.fetched) < time.Minute {
		return c.fetchErr
	}
	c.fetched = time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.internal()+"/hosting/discovery", nil)
	res, err := c.client().Do(req)
	if err != nil {
		c.fetchErr = fmt.Errorf("collabora discovery: %w", err)
		return c.fetchErr
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		c.fetchErr = fmt.Errorf("collabora discovery: HTTP %d", res.StatusCode)
		return c.fetchErr
	}
	var d discovery
	if err := xml.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&d); err != nil {
		c.fetchErr = fmt.Errorf("collabora discovery: unreadable: %w", err)
		return c.fetchErr
	}
	pub, _ := url.Parse(c.PublicURL)
	acts := map[string]map[string]string{}
	for _, app := range d.Apps {
		for _, a := range app.Actions {
			if a.Ext == "" || a.URLSrc == "" {
				continue
			}
			u, err := url.Parse(a.URLSrc)
			if err != nil {
				continue
			}
			if pub != nil && pub.Host != "" {
				u.Scheme, u.Host = pub.Scheme, pub.Host
			}
			ext := strings.ToLower(a.Ext)
			if acts[ext] == nil {
				acts[ext] = map[string]string{}
			}
			acts[ext][a.Name] = u.String()
		}
	}
	if len(acts) == 0 {
		c.fetchErr = errors.New("collabora discovery lists no file types")
		return c.fetchErr
	}
	c.actions, c.fetchErr = acts, nil
	return nil
}

// Action returns the editor address for a file type: "edit" when Collabora
// edits it and edit is wanted, else "view"; "" when it does not open the type.
func (c *Collabora) Action(ctx context.Context, ext string, edit bool) (string, bool) {
	if err := c.Discover(ctx); err != nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.actions[strings.ToLower(ext)]
	if m == nil {
		return "", false
	}
	if edit {
		if u := m["edit"]; u != "" {
			return u, true
		}
	}
	if u := m["view"]; u != "" {
		return u, false
	}
	if u := m["edit"]; u != "" {
		return u, false
	}
	return "", false
}

// Opens reports whether Collabora shows the file type at all.
func (c *Collabora) Opens(ctx context.Context, ext string) bool {
	u, _ := c.Action(ctx, ext, false)
	return u != ""
}

// Count is how many file types discovery listed (for the settings test).
func (c *Collabora) Count(ctx context.Context) (int, error) {
	if err := c.Discover(ctx); err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.actions), nil
}

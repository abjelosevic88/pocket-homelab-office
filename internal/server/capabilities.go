package server

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/office"
)

// What Pocket Search and Pocket Hub ask every app (capabilities in links.go;
// docs/capabilities.md in pocket-homelab-core). Both run as the request's
// person and read only their places and their recent files; neither opens a
// document or changes anything.
//
// Pocket Office keeps no list of documents of its own: they live in the
// person's places (My files, Drafts, server folders, Nextcloud, WebDAV,
// Pocket Documents, Pocket Files). The search reads them the way GET /search
// does, through the same places with the same limits, but stops walking in
// time for Pocket Search, which waits 6 seconds for an app, and leaves out
// the Pocket Documents and Pocket Files places: those apps answer Pocket
// Search themselves, and the same file would show twice. The widget reads
// only the recent list kept here.

// pocketSearchWalk is how long the search walks the places; what is found
// by then is the answer (GET /search walks for up to 45 seconds).
var pocketSearchWalk = 4 * time.Second

// searchedElsewhere are the kinds of place whose files Pocket Search finds in
// the app they live in (Pocket Documents, Pocket Files), not here.
var searchedElsewhere = map[string]bool{"documents": true, "files": true}

const widgetDocs = 5 // rows of the widget

// docKind is what a file is, in words: Document Server's editor families
// as people name them. A PDF or a drawing counts as a document.
func docKind(k office.Kind) string {
	switch k {
	case office.Cell:
		return "spreadsheet"
	case office.Slide:
		return "presentation"
	}
	return "document"
}

// editURL is the page that opens a file in the editor (/edit/:id?path= in
// web/src/main.tsx, as Home and ⌘K link to it).
func editURL(sourceID int64, p string) string {
	return "/edit/" + strconv.FormatInt(sourceID, 10) + "?" + url.Values{"path": {p}}.Encode()
}

// docID names a file across a person's places: "3:Reports/Plan.docx". It
// stays until the file is moved or renamed.
func docID(sourceID int64, p string) string { return strconv.FormatInt(sourceID, 10) + ":" + p }

// folderOf is the folder a file is in, as ⌘K shows it; nothing at the top of
// a place and for Pocket Documents, whose paths start with the document's number.
func folderOf(kind, p string) string {
	if kind == "documents" {
		return ""
	}
	if d := path.Dir(p); d != "." && d != "/" {
		return d
	}
	return ""
}

// dots joins the parts of a line that are there: "My files · Reports · edited today".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// hasWord reports whether there is a letter or a digit to look for.
func hasWord(q string) bool {
	return strings.IndexFunc(q, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// dayWord says which day t was, seen from now: "today", "yesterday",
// "Monday" within a week, then "12 Nov" and "12 Nov 2025".
func dayWord(t, now time.Time) string {
	t = t.In(now.Location())
	midnight := func(x time.Time) time.Time {
		return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, now.Location())
	}
	days := int(midnight(now).Sub(midnight(t)).Hours()/24 + 0.5)
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "yesterday"
	case days < 7:
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("2 Jan")
	}
	return t.Format("2 Jan 2006")
}

// openedAt reads a recent file's time ("" or a bad one is the zero time).
func openedAt(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ---- search ----

// pocketSearch answers Pocket Search with the files whose name has the words,
// under the same rule as GET /search (two characters at least). The files
// the person opened lately come first, newest first, from the recent list
// kept here; then what GET /search finds in their places, newest change
// first, as far as the walk got in pocketSearchWalk. A place that does not
// answer is left out, as GET /search leaves it out; Pocket Documents and
// Pocket Files places are never read (searchedElsewhere), recent files in
// them included.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx, uid := r.Context(), me(r)
	lq := strings.ToLower(q)
	if uid == 0 || len(lq) < 2 || !hasWord(q) {
		return nil, nil
	}
	now := time.Now()
	recent, err := s.recentViews(ctx, uid, 60)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]core.SearchItem, 0, limit)
	for _, rv := range recent {
		if searchedElsewhere[rv.SourceKind] || !office.Opens(rv.Name) || !strings.Contains(strings.ToLower(rv.Name), lq) {
			continue
		}
		id := docID(rv.SourceID, rv.Path)
		seen[id] = true
		it := core.SearchItem{ID: id, Title: rv.Name, Kind: docKind(rv.Kind), URL: editURL(rv.SourceID, rv.Path),
			Snippet: dots(rv.SourceName, folderOf(rv.SourceKind, rv.Path))}
		if t := openedAt(rv.OpenedAt); !t.IsZero() {
			it.Snippet = dots(it.Snippet, "opened "+dayWord(t, now))
			it.Date = t.UTC().Format(time.RFC3339)
		}
		out = append(out, it)
		if len(out) == limit {
			return out, nil
		}
	}
	wctx, cancel := context.WithTimeout(ctx, pocketSearchWalk)
	defer cancel()
	hits, err := s.searchNames(wctx, uid, q, searchedElsewhere)
	if err != nil {
		return nil, err
	}
	for _, h := range hits {
		id := docID(h.SourceID, h.Path)
		if seen[id] {
			continue
		}
		seen[id] = true
		it := core.SearchItem{ID: id, Title: h.Name, Kind: docKind(h.Kind), URL: editURL(h.SourceID, h.Path),
			Snippet: dots(h.SourceName, folderOf(h.SourceKind, h.Path))}
		if !h.Modified.IsZero() {
			it.Snippet = dots(it.Snippet, "edited "+dayWord(h.Modified, now))
			it.Date = h.Modified.UTC().Format(time.RFC3339)
		}
		out = append(out, it)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// ---- widget ----

// pocketWidget answers Pocket Hub with the documents the person opened last
// (what Home lists under Recent and GET /dashboard sends), each with the day
// it was opened. Only the newest few rows of the recent list are read; no
// place is asked. Someone who opened nothing yet gets nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	rs, err := s.recentViews(r.Context(), me(r), widgetDocs)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, nil
	}
	now := time.Now()
	out := &core.WidgetData{URL: "/"}
	for _, rv := range rs {
		it := core.WidgetItem{ID: docID(rv.SourceID, rv.Path), Title: rv.Name, URL: editURL(rv.SourceID, rv.Path)}
		if t := openedAt(rv.OpenedAt); !t.IsZero() {
			it.Detail = dayWord(t, now)
		}
		out.Items = append(out.Items, it)
	}
	return out, nil
}

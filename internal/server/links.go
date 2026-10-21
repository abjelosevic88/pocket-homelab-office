package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core): another
// Pocket app may act here for a person with a pcx_ token, only within these
// scopes. Every route not listed in linkRoutes stays closed to it.

var linkOffers = []core.Offer{
	{Scope: "files:read", Name: "See your files", Description: "List the places your files live, browse and search them, and get a link that opens a file in Pocket Office."},
}

// linkUses: Pocket Documents is one of the places files come from.
var linkUses = []core.Use{
	{App: "documents", Scopes: []string{"documents:read", "documents:write"}, Reason: "Open the Word, Excel and PowerPoint files in your Pocket Documents archive, and save your edits back into it"},
}

// serviceUses: Nextcloud through Core, so nobody types an app password.
var serviceUses = []core.ServiceUse{
	{Type: "nextcloud", Reason: "Open and edit the office files in your Nextcloud"},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(sources|recent|search|dashboard|formats)$`), "files:read"},
	{"GET", regexp.MustCompile(`^/api/v1/sources/\d+/(ls|file)$`), "files:read"},
}

var errLinkScope = errors.New("outside the link's scope")

func linkAllows(scopes []string, r *http.Request) bool {
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token and maps it to the person it acts for.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, err
	}
	if !in.Active || !in.Delegated() || in.User == nil || in.User.Email == "" {
		return nil, http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, errLinkScope
	}
	return s.coreIdentityUser(ctx, in)
}

// reportModes tells Core how each person is connected to Nextcloud and
// Pocket Documents here (Core → Links shows "Direct" for typed-in ones).
func (s *Server) reportModes(ctx context.Context) {
	if !s.core.Configured() {
		return
	}
	users, _ := s.store.ListUsers(ctx)
	var modes []core.ConnMode
	for _, u := range users {
		srcs, _ := s.store.ListSources(ctx, u.ID)
		nc, docs := "off", "off"
		for _, src := range srcs {
			switch src.Kind {
			case "nextcloud":
				if src.ViaCore {
					nc = "core"
				} else if nc == "off" {
					nc = "direct"
				}
			case "documents":
				if src.ViaCore {
					docs = "core"
				} else if docs == "off" {
					docs = "direct"
				}
			}
		}
		modes = append(modes, core.ConnMode{User: u.Email, Target: "service:nextcloud", Mode: nc}, core.ConnMode{User: u.Email, Target: "documents", Mode: docs})
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := s.core.ReportModes(c, modes); err != nil {
		s.log.Debug("pocket core: report modes", "err", err)
	}
}

// linkReason puts Core's refusal into words for the person.
func linkReason(target, reason string) string {
	switch reason {
	case "off_person":
		return "you switched the Office → " + target + " link off in Pocket Core (Settings → Apps acting for me)"
	case "off_admin":
		return "the Office → " + target + " link is switched off in Pocket Core (Links)"
	case "target_missing":
		return target + " is not registered with Pocket Core"
	case "not_offered":
		return "this " + target + " version does not accept app links"
	case "not_declared":
		return "Pocket Core has not seen this Pocket Office version yet; it re-registers within a minute of starting"
	case "not_in_audience":
		return "you do not have access to " + target + " in Pocket Core"
	case "unknown_user":
		return "your email is not a person in Pocket Core"
	case "no_account":
		return "Pocket Core has no " + target + " account for you (Users → Credentials)"
	}
	return ""
}

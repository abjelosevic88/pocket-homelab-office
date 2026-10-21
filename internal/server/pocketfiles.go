package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
	"github.com/abjelosevic88/pocket-homelab-office/internal/store"
)

// Pocket Files as a source, through Pocket Core only: its WebDAV root is the
// person's home, and a pcx_ link token for that person is the password.
// Tokens live five minutes, so one is used for a few minutes at most and
// then Core is asked again; a link switched off there stops the place soon
// after.

var filesScopes = []string{"files:read", "files:write"}

// filesTokenReuse is the longest one link token is used for.
const filesTokenReuse = 4 * time.Minute

// filesToken hands out one person's Pocket Files link token.
type filesToken struct {
	s     *Server
	email string

	mu    sync.Mutex
	lt    *core.LinkToken
	until time.Time
}

func (t *filesToken) get(ctx context.Context) (*core.LinkToken, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lt != nil && time.Now().Before(t.until) {
		return t.lt, nil
	}
	lt, err := t.s.core.ActAs(ctx, t.email, "files", filesScopes...)
	if err != nil {
		t.lt = nil
		var le *core.LinkError
		if errors.As(err, &le) {
			return nil, errNotConnected{Name: "Pocket Files", Msg: "Pocket Files through Pocket Core: " + firstNonEmpty(linkReason("Pocket Files", le.Reason), le.Message)}
		}
		return nil, fmt.Errorf("Pocket Files through Pocket Core: %w", err)
	}
	if lt.AccessToken == "" || lt.URL == "" {
		return nil, errNotConnected{Name: "Pocket Files", Msg: "Pocket Files through Pocket Core: Core answered without a token or an address"}
	}
	// A minute's margin before the token runs out, and never longer than filesTokenReuse.
	keep := min(time.Duration(lt.ExpiresIn)*time.Second-time.Minute, filesTokenReuse)
	t.lt, t.until = lt, time.Now().Add(keep)
	return lt, nil
}

// creds is the WebDAV password: any user name, the token as the password.
func (t *filesToken) creds(ctx context.Context) (string, string, error) {
	lt, err := t.get(ctx)
	if err != nil {
		return "", "", err
	}
	return "office", lt.AccessToken, nil
}

func (s *Server) filesBackend(ctx context.Context, src *store.Source) (storage.Backend, error) {
	if !s.core.Configured() {
		return nil, errNotConnected{Name: "Pocket Files", Msg: "there is no Pocket Core on this box; add Pocket Files as a WebDAV place with an app password instead"}
	}
	email := s.userEmail(ctx, src.UserID)
	if email == "" {
		return nil, errNotConnected{Name: "Pocket Files", Msg: "Pocket Files through Pocket Core needs your email"}
	}
	tok := &filesToken{s: s, email: email}
	lt, err := tok.get(ctx)
	if err != nil {
		return nil, err
	}
	b, err := storage.NewPocketFiles(strings.TrimRight(lt.URL, "/")+"/dav", email, src.Folder, tok.creds)
	if err != nil {
		return nil, err
	}
	b.UserAgent = "PocketOffice/" + Version
	return b, nil
}

package store

import (
	"context"
	"strings"
	"time"
)

// Source is one place a person's files live.
type Source struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Kind      string `json:"kind"` // builtin | folder | webdav | nextcloud | documents
	Name      string `json:"name"`
	URL       string `json:"url"`
	Username  string `json:"username"`
	Secret    string `json:"-"`
	SecretSet bool   `json:"secret_set"`
	Folder    string `json:"folder"`
	ViaCore   bool   `json:"via_core"`
	Position  int    `json:"position"`
	CreatedAt string `json:"created_at"`
}

const sourceCols = `id, user_id, kind, name, url, username, secret, folder, via_core, position, created_at`

func scanSource(row interface{ Scan(...any) error }) (*Source, error) {
	var s Source
	var via int
	if err := row.Scan(&s.ID, &s.UserID, &s.Kind, &s.Name, &s.URL, &s.Username, &s.Secret, &s.Folder, &via, &s.Position, &s.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	s.ViaCore = via == 1
	s.SecretSet = s.Secret != ""
	return &s, nil
}

// ListSources returns a person's sources in order.
func (s *Store) ListSources(ctx context.Context, userID int64) ([]Source, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *src)
	}
	return out, rows.Err()
}

// GetSource returns one of a person's sources (ErrNotFound for anyone else's).
func (s *Store) GetSource(ctx context.Context, userID, id int64) (*Source, error) {
	return scanSource(s.DB.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id = ? AND user_id = ?`, id, userID))
}

// GetSourceAny returns a source regardless of owner (for Document Server callbacks).
func (s *Store) GetSourceAny(ctx context.Context, id int64) (*Source, error) {
	return scanSource(s.DB.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id = ?`, id))
}

// CreateSource adds a source at the end of the list.
func (s *Store) CreateSource(ctx context.Context, src *Source) error {
	var pos int
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(position), 0) + 1 FROM sources WHERE user_id = ?`, src.UserID).Scan(&pos)
	src.Position = pos
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sources (user_id, kind, name, url, username, secret, folder, via_core, position) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		src.UserID, src.Kind, src.Name, src.URL, src.Username, src.Secret, src.Folder, b2i(src.ViaCore), pos)
	if err != nil {
		return err
	}
	src.ID, _ = res.LastInsertId()
	src.CreatedAt = Now()
	src.SecretSet = src.Secret != ""
	return nil
}

// UpdateSource saves a source's fields.
func (s *Store) UpdateSource(ctx context.Context, src *Source) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sources SET name = ?, url = ?, username = ?, secret = ?, folder = ?, via_core = ?, position = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		src.Name, src.URL, src.Username, src.Secret, src.Folder, b2i(src.ViaCore), src.Position, Now(), src.ID, src.UserID)
	src.SecretSet = src.Secret != ""
	return err
}

// DeleteSource removes a source and what was opened through it.
func (s *Store) DeleteSource(ctx context.Context, userID, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM sources WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recent WHERE source_id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM doc_keys WHERE source_id = ? AND user_id = ?`, id, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// BuiltinSource returns the person's built-in source, making it when missing.
func (s *Store) BuiltinSource(ctx context.Context, userID int64) (*Source, error) {
	src, err := scanSource(s.DB.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE user_id = ? AND kind = 'builtin' ORDER BY id LIMIT 1`, userID))
	if err == nil {
		return src, nil
	}
	if err != ErrNotFound {
		return nil, err
	}
	src = &Source{UserID: userID, Kind: "builtin", Name: "My files"}
	// the built-in place goes first
	if _, err := s.DB.ExecContext(ctx, `UPDATE sources SET position = position + 1 WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO sources (user_id, kind, name, position) VALUES (?, 'builtin', 'My files', 0)`, userID)
	if err != nil {
		return nil, err
	}
	src.ID, _ = res.LastInsertId()
	src.CreatedAt = Now()
	return src, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- recent ----

// Recent is one lately opened file.
type Recent struct {
	SourceID int64  `json:"source_id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	OpenedAt string `json:"opened_at"`
}

// Touch records that a file was opened now.
func (s *Store) Touch(ctx context.Context, userID, sourceID int64, path string) error {
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO recent (user_id, source_id, path, name, opened_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id, source_id, path) DO UPDATE SET opened_at = excluded.opened_at, name = excluded.name`, userID, sourceID, path, name, Now())
	if err == nil {
		_, _ = s.DB.ExecContext(ctx, `DELETE FROM recent WHERE user_id = ? AND (user_id, source_id, path) NOT IN (SELECT user_id, source_id, path FROM recent WHERE user_id = ? ORDER BY opened_at DESC LIMIT 60)`, userID, userID)
	}
	return err
}

// ListRecent returns a person's lately opened files, newest first.
func (s *Store) ListRecent(ctx context.Context, userID int64, limit int) ([]Recent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT source_id, path, name, opened_at FROM recent WHERE user_id = ? ORDER BY opened_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recent{}
	for rows.Next() {
		var r Recent
		if err := rows.Scan(&r.SourceID, &r.Path, &r.Name, &r.OpenedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ForgetRecent drops a file from the recent list (moved, deleted or renamed).
func (s *Store) ForgetRecent(ctx context.Context, userID, sourceID int64, path string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM recent WHERE user_id = ? AND source_id = ? AND (path = ? OR path LIKE ? ESCAPE '\')`, userID, sourceID, path, likeEscape(path)+"/%")
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---- document keys ----

// DocKey is one document open in Document Server.
type DocKey struct {
	Key        string
	Canonical  string
	UserID     int64
	SourceID   int64
	Path       string
	ETag       string
	FetchToken string
	CreatedAt  string
	SavedAt    *string
}

const keyCols = `key, canonical, user_id, source_id, path, etag, fetch_token, created_at, saved_at`

func scanKey(row interface{ Scan(...any) error }) (*DocKey, error) {
	var k DocKey
	if err := row.Scan(&k.Key, &k.Canonical, &k.UserID, &k.SourceID, &k.Path, &k.ETag, &k.FetchToken, &k.CreatedAt, &k.SavedAt); err != nil {
		return nil, notFound(err)
	}
	return &k, nil
}

// ActiveKey returns the open key for a document, if any (keys older than a
// day are considered abandoned).
func (s *Store) ActiveKey(ctx context.Context, canonical string) (*DocKey, error) {
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	return scanKey(s.DB.QueryRowContext(ctx, `SELECT `+keyCols+` FROM doc_keys WHERE canonical = ? AND created_at > ? ORDER BY created_at DESC LIMIT 1`, canonical, cutoff))
}

// GetKey returns a key row.
func (s *Store) GetKey(ctx context.Context, key string) (*DocKey, error) {
	return scanKey(s.DB.QueryRowContext(ctx, `SELECT `+keyCols+` FROM doc_keys WHERE key = ?`, key))
}

// PutKey stores a new key.
func (s *Store) PutKey(ctx context.Context, k *DocKey) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO doc_keys (key, canonical, user_id, source_id, path, etag, fetch_token) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		k.Key, k.Canonical, k.UserID, k.SourceID, k.Path, k.ETag, k.FetchToken)
	return err
}

// KeySaved records a save and the file's new ETag.
func (s *Store) KeySaved(ctx context.Context, key, etag string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE doc_keys SET etag = ?, saved_at = ? WHERE key = ?`, etag, Now(), key)
	return err
}

// DropKey forgets a key: the next open gets a fresh one.
func (s *Store) DropKey(ctx context.Context, key string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM doc_keys WHERE key = ?`, key)
	return err
}

// DropKeysFor forgets the keys of a path (it was renamed or deleted).
func (s *Store) DropKeysFor(ctx context.Context, sourceID int64, path string) {
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM doc_keys WHERE source_id = ? AND (path = ? OR path LIKE ? ESCAPE '\')`, sourceID, path, likeEscape(path)+"/%")
}

// PurgeKeys forgets keys older than two days.
func (s *Store) PurgeKeys(ctx context.Context) {
	cutoff := time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM doc_keys WHERE created_at < ?`, cutoff)
}

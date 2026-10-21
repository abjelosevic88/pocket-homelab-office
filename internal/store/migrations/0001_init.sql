CREATE TABLE users (
  id            INTEGER PRIMARY KEY,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, Document Server address and secret).
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

-- Where a person's files are: the built-in folder, a server folder, a WebDAV
-- server, a Nextcloud (through Pocket Core or typed in) or Pocket Documents.
CREATE TABLE sources (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind       TEXT NOT NULL,                 -- builtin | folder | webdav | nextcloud | documents
  name       TEXT NOT NULL DEFAULT '',
  url        TEXT NOT NULL DEFAULT '',      -- server address (webdav, nextcloud, documents)
  username   TEXT NOT NULL DEFAULT '',
  secret     TEXT NOT NULL DEFAULT '',      -- password / app password / API token (direct mode)
  folder     TEXT NOT NULL DEFAULT '',      -- path inside the server (nextcloud, folder root)
  via_core   INTEGER NOT NULL DEFAULT 0,    -- credentials come from Pocket Core
  position   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at TEXT
);
CREATE INDEX sources_user ON sources(user_id, position);

-- One open document in Document Server: the key every editor of that
-- document shares until it is closed and saved, and where the result goes.
CREATE TABLE doc_keys (
  key         TEXT PRIMARY KEY,
  canonical   TEXT NOT NULL,                -- the same file through two sources is one document
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id   INTEGER NOT NULL,
  path        TEXT NOT NULL,
  etag        TEXT NOT NULL DEFAULT '',
  fetch_token TEXT NOT NULL,                -- lets Document Server download the file
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  saved_at    TEXT
);
CREATE INDEX doc_keys_canonical ON doc_keys(canonical);

-- What a person opened lately.
CREATE TABLE recent (
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_id INTEGER NOT NULL,
  path      TEXT NOT NULL,
  name      TEXT NOT NULL DEFAULT '',
  opened_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, source_id, path)
);

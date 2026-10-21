// Package storage is where a person's office files live: a folder tree on
// this server or on a WebDAV server such as Nextcloud. Pocket Office keeps no
// copy; files are read when opened and written back when Document Server
// reports a save, so every other app that reads the same folder sees the same
// files.
package storage

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Errors every backend maps its own failures onto.
var (
	ErrNotFound     = errors.New("storage: not found")
	ErrExists       = errors.New("storage: already exists")
	ErrPrecondition = errors.New("storage: changed meanwhile")
	ErrAuth         = errors.New("storage: sign-in refused")
	ErrLocked       = errors.New("storage: file is locked")
	ErrInvalidPath  = errors.New("storage: invalid path")
	ErrReadOnly     = errors.New("storage: read-only")
)

// Entry is one file or folder, its path relative to the root.
type Entry struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	IsDir    bool      `json:"is_dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	// ETag changes whenever the file does. Opaque, compared for equality only.
	ETag string `json:"etag,omitempty"`
	// FileID is the server's own id for the file (Nextcloud's oc:fileid), or "".
	FileID string `json:"file_id,omitempty"`
}

// Backend is one storage location.
type Backend interface {
	// Kind names the backend: builtin | folder | webdav | nextcloud.
	Kind() string
	// Describe says where the files are, for people ("Nextcloud · Documents").
	Describe() string
	// ListDir returns the files and folders directly inside a folder, hidden ones (".x") left out.
	ListDir(ctx context.Context, dir string) ([]Entry, error)
	// Stat returns one entry.
	Stat(ctx context.Context, p string) (Entry, error)
	// Read returns a file's bytes and its entry.
	Read(ctx context.Context, p string) ([]byte, Entry, error)
	// Write replaces a file when its ETag is still ifMatch ("" = whatever is
	// there), creating folders on the way. ErrPrecondition when it changed.
	Write(ctx context.Context, p string, data []byte, ifMatch string) (Entry, error)
	// Create writes a new file; ErrExists when the name is taken.
	Create(ctx context.Context, p string, data []byte) (Entry, error)
	// Move renames a file or folder; ErrExists when the target is taken.
	Move(ctx context.Context, from, to string) (Entry, error)
	// Delete removes a file or a folder with everything in it. A missing one is fine.
	Delete(ctx context.Context, p string) error
	// Mkdir makes a folder and its parents.
	Mkdir(ctx context.Context, p string) error
	// Test checks that the root can be read (and made, when missing).
	Test(ctx context.Context) error
	// Canonical names a file so the same file reached through two sources
	// (two people's Nextcloud accounts on one shared folder) is one document.
	Canonical(p string) string
}

// Clean normalises a relative path: forward slashes, no leading slash, no
// "." or ".." parts. An empty path is the root.
func Clean(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalidPath
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", ErrInvalidPath
		}
		out = append(out, part)
	}
	return strings.Join(out, "/"), nil
}

// Hidden reports whether a path has a part starting with a dot.
func Hidden(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

// Base is the last part of a path.
func Base(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Dir is everything before the last part ("" at the root).
func Dir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

// Join joins a folder and a name.
func Join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// ValidName reports whether a file or folder name can be used as is.
func ValidName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 255 {
		return false
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

package server

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
)

// Pocket Move (docs/move.md in pocket-homelab-core): a person's "My files" —
// the place on this box that is theirs alone — goes out as a zip with its
// folders, and such a zip comes back in. Files in Nextcloud, Pocket
// Documents, Pocket Files or a WebDAV server stay where they are.

func (s *Server) myFiles(ctx context.Context, uid int64) (storage.Backend, error) {
	src, err := s.store.BuiltinSource(ctx, uid)
	if err != nil {
		return nil, err
	}
	return s.backendFor(ctx, src)
}

// handleExportZip sends every file in the person's "My files" as a zip.
func (s *Server) handleExportZip(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.myFiles(ctx, me(r))
	if err != nil {
		s.fail(w, err, "my files")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-office-my-files.zip"`)
	zw := zip.NewWriter(w)
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := b.ListDir(ctx, dir)
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
		for _, e := range entries {
			if e.IsDir {
				if err := walk(e.Path); err != nil {
					return err
				}
				continue
			}
			data, _, err := b.Read(ctx, e.Path)
			if err != nil {
				return err
			}
			f, err := zw.CreateHeader(&zip.FileHeader{Name: e.Path, Method: zip.Deflate, Modified: e.Modified})
			if err != nil {
				return err
			}
			if _, err := f.Write(data); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		s.log.Warn("export my files", "err", err)
	}
	_ = zw.Close()
}

// handleImportZip adds the files of a zip (multipart field "file") to the
// person's "My files", folders kept. A file that is already there with the
// same bytes is skipped; a different file of the same name comes in next to
// it as "name (2).docx". Nothing is replaced or removed.
func (s *Server) handleImportZip(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := int64(s.cfg.MaxUploadMB) << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "send the zip as the multipart field \"file\"")
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) > limit {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("the zip is larger than %d MB", s.cfg.MaxUploadMB))
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that is not a zip file")
		return
	}
	b, err := s.myFiles(ctx, me(r))
	if err != nil {
		s.fail(w, err, "my files")
		return
	}
	imported, skipped, renamed, failed := 0, 0, 0, 0
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		p, err := storage.Clean(zf.Name)
		if err != nil || p == "" || storage.Hidden(p) || strings.HasPrefix(p, "__MACOSX/") {
			skipped++
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			failed++
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, limit+1))
		rc.Close()
		if err != nil || int64(len(data)) > limit {
			failed++
			continue
		}
		switch placed, err := addFile(ctx, b, p, data); {
		case err != nil:
			s.log.Warn("import my files", "path", p, "err", err)
			failed++
		case placed == "":
			skipped++
		case placed != p:
			imported++
			renamed++
		default:
			imported++
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported, "skipped": skipped, "renamed": renamed, "failed": failed})
}

// addFile writes data at p, or next to it as "name (2).ext" when another
// file has that name. It returns "" when the same bytes are already at one
// of those names (a second run of the same zip adds nothing).
func addFile(ctx context.Context, b storage.Backend, p string, data []byte) (string, error) {
	dir, name := storage.Dir(p), storage.Base(p)
	stem, ext := name, ""
	if i := strings.LastIndex(name, "."); i > 0 {
		stem, ext = name[:i], name[i:]
	}
	for n := 1; n <= 100; n++ {
		target := p
		if n > 1 {
			target = storage.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		have, _, err := b.Read(ctx, target)
		if errors.Is(err, storage.ErrNotFound) {
			if _, err := b.Create(ctx, target, data); err != nil {
				return "", err
			}
			return target, nil
		}
		if err != nil {
			return "", err
		}
		if bytes.Equal(have, data) {
			return "", nil
		}
	}
	return "", errors.New("too many files with that name")
}

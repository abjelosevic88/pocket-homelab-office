package server

import (
	"path/filepath"

	"github.com/abjelosevic88/pocket-homelab-office/internal/storage"
)

// scratchBackend is a folder under DATA_DIR for probes and temporary files.
func (s *Server) scratchBackend() storage.Backend {
	return storage.NewLocal(filepath.Join(s.cfg.DataDir, "tmp"), "builtin", "scratch")
}

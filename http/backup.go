package http

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// handleBackup streams a consistent database snapshot to the client as a
// downloadable .db file. Authenticated users only.
//
// The filename includes the current date for easy identification:
//
//	library-2026-06-11.db
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.BackupService == nil {
		respondError(w, fmt.Errorf("backup service not configured"))
		return
	}

	filename := fmt.Sprintf("library-%s.db", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "private, no-store")

	if err := s.BackupService.Backup(r.Context(), w); err != nil {
		// Headers already written — log and return; can't send a new status.
		log.Printf("backup: %v", err)
	}
}

package http

import (
	"net/http"

	"github.com/mwyvr/kid"
)

// handleStatus returns statistics for the authenticated user's own photos.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	st, err := s.StatusService.LibraryStatus(r.Context(), &userID)
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, st)
}

// handleAdminStatus returns system-wide statistics across all users.
// Admin only.
func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.StatusService.LibraryStatus(r.Context(), (*kid.ID)(nil))
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, st)
}

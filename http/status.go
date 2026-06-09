package http

import (
	"net/http"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.StatusService.LibraryStatus(r.Context())
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, st)
}

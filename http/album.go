package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

func (s *Server) registerAlbumRoutes() {
	s.router.HandleFunc("GET /api/v1/albums", s.requireAuth(s.handleListAlbums))
	s.router.HandleFunc("POST /api/v1/albums", s.requireAuth(s.handleCreateAlbum))
	s.router.HandleFunc("GET /api/v1/albums/{id}", s.requireAuth(s.handleGetAlbum))
	s.router.HandleFunc("PATCH /api/v1/albums/{id}", s.requireAuth(s.handleUpdateAlbum))
	s.router.HandleFunc("DELETE /api/v1/albums/{id}", s.requireAuth(s.handleDeleteAlbum))

	s.router.HandleFunc("GET /api/v1/albums/{id}/photos", s.requireAuth(s.handleListAlbumPhotos))
	s.router.HandleFunc("POST /api/v1/albums/{id}/photos/{photoId}", s.requireAuth(s.handleAddPhotoToAlbum))
	s.router.HandleFunc("DELETE /api/v1/albums/{id}/photos/{photoId}", s.requireAuth(s.handleRemovePhotoFromAlbum))
	s.router.HandleFunc("POST /api/v1/albums/{id}/photos/{photoId}/move", s.requireAuth(s.handleMovePhotoInAlbum))
	s.router.HandleFunc("PUT /api/v1/albums/{id}/cover/{photoId}", s.requireAuth(s.handleSetAlbumCover))
}

func (s *Server) handleListAlbums(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	filter := photo.AlbumFilter{
		UserID: userID,
		Limit:  100,
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	albums, total, err := s.AlbumService.FindAlbums(r.Context(), filter)
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]interface{}{
		"albums": albums,
		"total":  total,
	})
}

func (s *Server) handleCreateAlbum(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid request body"))
		return
	}

	a := &photo.Album{
		UserID:      userID,
		Name:        body.Name,
		Description: body.Description,
	}
	if err := s.AlbumService.CreateAlbum(r.Context(), a); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusCreated, a)
}

func (s *Server) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	idOrSlug := r.PathValue("id")
	a, err := s.resolveAlbum(r, idOrSlug)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if a.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	respond(w, http.StatusOK, a)
}

func (s *Server) handleUpdateAlbum(w http.ResponseWriter, r *http.Request) {
	idOrSlug := r.PathValue("id")
	a, err := s.resolveAlbum(r, idOrSlug)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if a.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	var body struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		CoverPhotoID *string `json:"coverPhotoId"` // "" to clear
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid request body"))
		return
	}

	upd := photo.AlbumUpdate{
		Name:        body.Name,
		Description: body.Description,
	}
	if body.CoverPhotoID != nil {
		if *body.CoverPhotoID == "" {
			// Clear cover photo.
			zero := kid.ID{}
			upd.CoverPhotoID = &zero
		} else {
			id, err := kid.FromString(*body.CoverPhotoID)
			if err != nil {
				respondError(w, photo.Errorf(photo.EINVALID, "invalid cover photo ID"))
				return
			}
			upd.CoverPhotoID = &id
		}
	}

	updated, err := s.AlbumService.UpdateAlbum(r.Context(), a.ID, upd)
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteAlbum(w http.ResponseWriter, r *http.Request) {
	idOrSlug := r.PathValue("id")
	a, err := s.resolveAlbum(r, idOrSlug)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if a.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	if err := s.AlbumService.DeleteAlbum(r.Context(), a.ID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) handleListAlbumPhotos(w http.ResponseWriter, r *http.Request) {
	idOrSlug := r.PathValue("id")
	a, err := s.resolveAlbum(r, idOrSlug)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if a.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	photos, total, err := s.AlbumService.FindAlbumPhotos(r.Context(), a.ID, offset, limit)
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]interface{}{
		"photos": photos,
		"total":  total,
		"offset": offset,
		"limit":  limit,
	})
}

func (s *Server) handleAddPhotoToAlbum(w http.ResponseWriter, r *http.Request) {
	a, err := s.resolveAlbum(r, r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if a.UserID != userIDFromContext(r.Context()) {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	photoID, ok := parsePathID(w, r, "photoId")
	if !ok {
		return
	}
	if err := s.AlbumService.AddPhoto(r.Context(), a.ID, photoID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) handleRemovePhotoFromAlbum(w http.ResponseWriter, r *http.Request) {
	a, err := s.resolveAlbum(r, r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if a.UserID != userIDFromContext(r.Context()) {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	photoID, ok := parsePathID(w, r, "photoId")
	if !ok {
		return
	}
	if err := s.AlbumService.RemovePhoto(r.Context(), a.ID, photoID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) handleMovePhotoInAlbum(w http.ResponseWriter, r *http.Request) {
	a, err := s.resolveAlbum(r, r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if a.UserID != userIDFromContext(r.Context()) {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	photoID, ok := parsePathID(w, r, "photoId")
	if !ok {
		return
	}

	var body struct {
		AfterPhotoID string `json:"afterPhotoId"`
	}
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	var afterID kid.ID
	if body.AfterPhotoID != "" {
		id, err := kid.FromString(body.AfterPhotoID)
		if err != nil {
			respondError(w, photo.Errorf(photo.EINVALID, "invalid afterPhotoId"))
			return
		}
		afterID = id
	}

	if err := s.AlbumService.MovePhoto(r.Context(), a.ID, photoID, afterID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) handleSetAlbumCover(w http.ResponseWriter, r *http.Request) {
	a, err := s.resolveAlbum(r, r.PathValue("id"))
	if err != nil {
		respondError(w, err)
		return
	}
	if a.UserID != userIDFromContext(r.Context()) {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	photoID, ok := parsePathID(w, r, "photoId")
	if !ok {
		return
	}
	updated, err := s.AlbumService.UpdateAlbum(r.Context(), a.ID, photo.AlbumUpdate{
		CoverPhotoID: &photoID,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, updated)
}

// resolveAlbum looks up an album by either its slug or kid ID string.
// This lets API callers use /api/v1/albums/travel or /api/v1/albums/06gb...
func (s *Server) resolveAlbum(r *http.Request, idOrSlug string) (*photo.Album, error) {
	// Try as kid ID first.
	if id, err := kid.FromString(idOrSlug); err == nil {
		return s.AlbumService.FindAlbumByID(r.Context(), id)
	}
	// Fall back to slug lookup.
	return s.AlbumService.FindAlbumBySlug(r.Context(), idOrSlug)
}

// ownsAlbum checks the authenticated user owns the album identified by slug or ID.
func (s *Server) ownsAlbum(r *http.Request, idOrSlug string) error {
	a, err := s.resolveAlbum(r, idOrSlug)
	if err != nil {
		return err
	}
	userID := userIDFromContext(r.Context())
	if a.UserID != userID {
		return photo.Errorf(photo.EFORBIDDEN, "access denied")
	}
	return nil
}

package http

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mwyvr/photo"
)

func (s *Server) handleListPhotos(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	q := r.URL.Query()

	filter := photo.PhotoFilter{
		UserID: userID,
		Limit:  50,
	}

	if v := q.Get("raw_only"); v == "true" {
		t := true
		filter.IsRaw = &t
	}
	if v := q.Get("location"); v != "" {
		filter.Location = &v
	}
	if v := q.Get("after"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, photo.Errorf(photo.EINVALID, "after: expected YYYY-MM-DD"))
			return
		}
		filter.CapturedAfter = &t
	}
	if v := q.Get("before"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			respondError(w, photo.Errorf(photo.EINVALID, "before: expected YYYY-MM-DD"))
			return
		}
		end := t.Add(24*time.Hour - time.Second)
		filter.CapturedBefore = &end
	}
	if v := q["tag"]; len(v) > 0 {
		filter.Tags = v
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			respondError(w, photo.Errorf(photo.EINVALID, "limit must be a positive integer"))
			return
		}
		filter.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			respondError(w, photo.Errorf(photo.EINVALID, "offset must be a non-negative integer"))
			return
		}
		filter.Offset = n
	}

	photos, total, err := s.PhotoService.FindPhotos(r.Context(), filter)
	if err != nil {
		respondError(w, err)
		return
	}

	respond(w, http.StatusOK, map[string]interface{}{
		"photos": photos,
		"total":  total,
		"offset": filter.Offset,
		"limit":  filter.Limit,
	})
}

func (s *Server) handleUploadPhoto(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	// Limit upload size to 200 MB.
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "could not parse multipart form: %v", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "file field is required"))
		return
	}
	defer file.Close()

	opts := photo.ImportOptions{
		UserID:  userID,
		RawOnly: r.URL.Query().Get("raw_only") == "true",
	}

	result := s.Importer.ImportReader(r.Context(), file, header.Filename, opts)
	if result.Err != nil {
		respondError(w, result.Err)
		return
	}
	if result.Skipped {
		respondError(w, photo.Errorf(photo.ECONFLICT, "%s", result.SkipReason))
		return
	}

	respond(w, http.StatusCreated, result.Photo)
}

func (s *Server) handleGetPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}

	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	// Enforce ownership.
	userID := userIDFromContext(r.Context())
	if p.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	respond(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}

	existing, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if existing.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	var body struct {
		Description  *string `json:"description"`
		LocationName *string `json:"locationName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid request body"))
		return
	}

	updated, err := s.PhotoService.UpdatePhoto(r.Context(), id, photo.PhotoUpdate{
		Description:  body.Description,
		LocationName: body.LocationName,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, updated)
}

// handleRegeocode reverse-geocodes a photo using its stored GPS coordinates,
// or sets the location_name directly from a user-supplied value.
//
//	POST /api/v1/photos/:id/regeocode
//	Body (optional): {"locationName": "Dawson Creek, Canada"}
//
// If locationName is provided it is stored directly; no Nominatim call is made.
// If locationName is absent the photo's GPS coords are used; fails if none present.
func (s *Server) handleRegeocode(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}

	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if p.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	var body struct {
		LocationName *string `json:"locationName"`
	}
	// Body is optional — ignore decode errors on empty body.
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	var locationName string

	if body.LocationName != nil {
		// Manual override — use directly.
		locationName = *body.LocationName
	} else {
		// Automatic — reverse geocode from GPS coords.
		if !p.HasGPS() {
			respondError(w, photo.Errorf(photo.EINVALID,
				"photo has no GPS data; provide a location manually with --location"))
			return
		}
		loc, err := s.Geocoder.ReverseGeocode(r.Context(), *p.GPSLat, *p.GPSLon)
		if err != nil {
			respondError(w, err)
			return
		}
		locationName = loc.DisplayName()
	}

	updated, err := s.PhotoService.UpdatePhoto(r.Context(), id, photo.PhotoUpdate{
		LocationName: &locationName,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, updated)
}

func (s *Server) handleDeletePhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}

	existing, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if existing.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	if err := s.PhotoService.DeletePhoto(r.Context(), id); err != nil {
		respondError(w, err)
		return
	}

	// Remove the file from disk after the DB record is gone.
	// Log but don't fail the request if the file is already missing.
	fullPath := filepath.Join(s.LibraryRoot, existing.StoredPath)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		log.Printf("delete photo: remove file %q: %v", fullPath, err)
	}

	respond(w, http.StatusNoContent, nil)
}

func (s *Server) handleAttachTag(w http.ResponseWriter, r *http.Request) {
	photoID, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}
	tagName := photo.NormalizeTagName(r.PathValue("name"))

	p, err := s.PhotoService.FindPhotoByID(r.Context(), photoID)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if p.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	t, err := s.TagService.FindOrCreateTag(r.Context(), tagName)
	if err != nil {
		respondError(w, err)
		return
	}
	if err := s.TagService.AttachTag(r.Context(), photoID, t.ID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

func (s *Server) handleDetachTag(w http.ResponseWriter, r *http.Request) {
	photoID, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}
	tagName := photo.NormalizeTagName(r.PathValue("name"))

	p, err := s.PhotoService.FindPhotoByID(r.Context(), photoID)
	if err != nil {
		respondError(w, err)
		return
	}
	userID := userIDFromContext(r.Context())
	if p.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}

	t, err := s.TagService.FindTagByName(r.Context(), tagName)
	if err != nil {
		respondError(w, err)
		return
	}
	if err := s.TagService.DetachTag(r.Context(), photoID, t.ID); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

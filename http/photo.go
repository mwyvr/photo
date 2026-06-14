package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mwyvr/photo"
)

// handlePhotoExists checks whether a photo with the given SHA-256 hash is
// already in the library. Returns 200 with the photo ID if found, 404 if not.
// Used by the CLI for pre-flight duplicate detection before uploading.
func (s *Server) handlePhotoExists(w http.ResponseWriter, r *http.Request) {
	sha256 := r.URL.Query().Get("sha256")
	if sha256 == "" {
		respondError(w, photo.Errorf(photo.EINVALID, "sha256 query parameter is required"))
		return
	}

	userID := userIDFromContext(r.Context())
	photos, _, err := s.PhotoService.FindPhotos(r.Context(), photo.PhotoFilter{
		UserID: userID,
		SHA256: &sha256,
	})
	if err != nil {
		respondError(w, err)
		return
	}
	if len(photos) == 0 {
		respondError(w, photo.Errorf(photo.ENOTFOUND, "photo not found"))
		return
	}
	respond(w, http.StatusOK, map[string]string{"id": photos[0].ID.String()})
}


func (s *Server) handleListPhotos(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	q := r.URL.Query()

	filter := photo.PhotoFilter{
		UserID:        userID,
		ViewerID:      userID,
		HouseholdMode: s.HouseholdMode,
		Limit:         50,
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

	// Strip any path components from the client-supplied filename.
	// filepath.Base("../../etc/passwd") → "passwd", preventing traversal.
	safeFilename := filepath.Base(header.Filename)

	// Determine visibility:
	// - If ?visibility= is explicitly set, use that value.
	// - Otherwise: RAW files are always private; rendered files use server default.
	q := r.URL.Query()
	var visibility photo.Visibility
	if v := q.Get("visibility"); photo.Visibility(v).IsValid() {
		visibility = photo.Visibility(v)
	} else if s.HouseholdMode {
		visibility = photo.VisibilityHousehold
	} else {
		visibility = photo.VisibilityPrivate
	}

	opts := photo.ImportOptions{
		UserID:     userID,
		RawOnly:    q.Get("raw_only") == "true",
		Visibility: visibility,
	}

	result := s.Importer.ImportReader(r.Context(), file, safeFilename, opts)
	if result.Err != nil {
		respondError(w, result.Err)
		return
	}
	if result.Skipped {
		respondError(w, photo.Errorf(photo.ECONFLICT, "%s", result.SkipReason))
		return
	}

	// RAW files are always private regardless of requested visibility.
	if result.Photo.IsRaw && result.Photo.Visibility != photo.VisibilityPrivate {
		priv := photo.VisibilityPrivate
		if _, err := s.PhotoService.UpdatePhoto(r.Context(), result.Photo.ID, photo.PhotoUpdate{
			Visibility: &priv,
		}); err != nil {
			log.Printf("correct RAW visibility for %s: %v", result.Photo.ID, err)
		}
		result.Photo.Visibility = photo.VisibilityPrivate
	}

	respond(w, http.StatusCreated, result.Photo)
}

// SafeFilePath joins libraryRoot and rel, returning an error if the result
// would escape libraryRoot. Prevents path traversal via crafted StoredPath values.
func SafeFilePath(libraryRoot, rel string) (string, error) {
	full := filepath.Join(libraryRoot, rel)
	prefix := libraryRoot + string(filepath.Separator)
	if !strings.HasPrefix(full, prefix) {
		return "", fmt.Errorf("path traversal attempt: %q", rel)
	}
	return full, nil
}

// safeFilePath is the internal alias used by handlers.
func safeFilePath(libraryRoot, rel string) (string, error) {
	return SafeFilePath(libraryRoot, rel)
}

// handleServePhotoFile serves the raw image file for an authenticated user.
func (s *Server) handleServePhotoFile(w http.ResponseWriter, r *http.Request) {
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

	fullPath, err := safeFilePath(s.LibraryRoot, p.StoredPath)
	if err != nil {
		log.Printf("serve photo file: %v", err)
		respondError(w, photo.Errorf(photo.EINTERNAL, "invalid file path"))
		return
	}
	http.ServeFile(w, r, fullPath)
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
		Description  *string            `json:"description"`
		LocationName *string            `json:"locationName"`
		Visibility   *photo.Visibility  `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid request body"))
		return
	}
	if body.Visibility != nil && !body.Visibility.IsValid() {
		respondError(w, photo.Errorf(photo.EINVALID, "invalid visibility value"))
		return
	}

	updated, err := s.PhotoService.UpdatePhoto(r.Context(), id, photo.PhotoUpdate{
		Description:  body.Description,
		LocationName: body.LocationName,
		Visibility:   body.Visibility,
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

// handleRegeocodeMissing finds all of the requesting user's photos that have
// GPS coordinates but no location name, and reverse-geocodes each in turn.
//
//	POST /api/v1/photos/regeocode-missing
//
// The NominatimGeocoder self-rate-limits to one request per second, so this
// may take a while for large libraries. Returns a summary of how many photos
// were updated and any that failed.
func (s *Server) handleRegeocodeMissing(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	t := true
	photos, _, err := s.PhotoService.FindPhotos(r.Context(), photo.PhotoFilter{
		UserID:          userID,
		MissingLocation: &t,
		Limit:           10000, // effectively unbounded for a personal library
	})
	if err != nil {
		respondError(w, err)
		return
	}

	type failure struct {
		PhotoID string `json:"photoId"`
		Error   string `json:"error"`
	}
	var updated int
	var failures []failure

	for _, p := range photos {
		if !p.HasGPS() {
			continue // shouldn't happen given the filter, but guard anyway
		}
		loc, err := s.Geocoder.ReverseGeocode(r.Context(), *p.GPSLat, *p.GPSLon)
		if err != nil {
			failures = append(failures, failure{PhotoID: p.ID.String(), Error: err.Error()})
			continue
		}
		name := loc.DisplayName()
		if _, err := s.PhotoService.UpdatePhoto(r.Context(), p.ID, photo.PhotoUpdate{
			LocationName: &name,
		}); err != nil {
			failures = append(failures, failure{PhotoID: p.ID.String(), Error: err.Error()})
			continue
		}
		updated++
	}

	respond(w, http.StatusOK, map[string]interface{}{
		"total":    len(photos),
		"updated":  updated,
		"failures": failures,
	})
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

// handleGeneratePhotoShareToken creates or replaces the share token on a photo.
//
//	POST /api/v1/photos/{id}/share
func (s *Server) handleGeneratePhotoShareToken(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if p.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	token, err := s.PhotoService.GenerateShareToken(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"token": token})
}

// handleRevokePhotoShareToken clears the share token on a photo.
//
//	DELETE /api/v1/photos/{id}/share
func (s *Server) handleRevokePhotoShareToken(w http.ResponseWriter, r *http.Request) {
	id, ok := parsePathID(w, r, "id")
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	if p.UserID != userID {
		respondError(w, photo.Errorf(photo.EFORBIDDEN, "access denied"))
		return
	}
	empty := ""
	if _, err := s.PhotoService.UpdatePhoto(r.Context(), id, photo.PhotoUpdate{ShareToken: &empty}); err != nil {
		respondError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "revoked"})
}

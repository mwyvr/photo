package html

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// handlePrivatePhotoFile serves a photo file for authenticated users via cookie session.
// Used by the detail page for private photos where Bearer tokens can't be used.
func (s *Server) handlePrivatePhotoFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.authenticatedUserID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	raw := r.PathValue("id")
	id, err := kid.FromString(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil || p.UserID != userID {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, filepath.Join(s.LibraryRoot, p.StoredPath))
}


// Returns 404 for unpublished photos to unauthenticated visitors.
func (s *Server) handlePublicPhoto(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolvePublicPhoto(w, r)
	if !ok {
		return
	}
	fullPath := filepath.Join(s.LibraryRoot, p.StoredPath)
	http.ServeFile(w, r, fullPath)
}

// handlePublicThumb serves (or generates) the thumbnail for a published photo.
func (s *Server) handlePublicThumb(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolvePublicPhoto(w, r)
	if !ok {
		return
	}

	// If cached thumbnail exists, serve it directly.
	if p.ThumbPath != nil && *p.ThumbPath != "" {
		thumbFull := filepath.Join(s.LibraryRoot, *p.ThumbPath)
		if _, err := os.Stat(thumbFull); err == nil {
			http.ServeFile(w, r, thumbFull)
			return
		}
	}

	// Generate thumbnail on first request.
	thumbPath, err := s.generateThumb(p)
	if err != nil {
		log.Printf("generate thumb for %s: %v", p.ID, err)
		// Fall back to serving the full image.
		http.ServeFile(w, r, filepath.Join(s.LibraryRoot, p.StoredPath))
		return
	}

	// Persist the thumb path so future requests skip generation.
	rel, _ := filepath.Rel(s.LibraryRoot, thumbPath)
	if _, err := s.PhotoService.UpdatePhoto(r.Context(), p.ID, photo.PhotoUpdate{
		ThumbPath: &rel,
	}); err != nil {
		log.Printf("save thumb path for %s: %v", p.ID, err)
	}

	http.ServeFile(w, r, thumbPath)
}

// resolvePublicPhoto looks up the photo by ID and enforces visibility.
// Authenticated users can see all their photos; unauthenticated visitors
// can only see published photos.
func (s *Server) resolvePublicPhoto(w http.ResponseWriter, r *http.Request) (*photo.Photo, bool) {
	raw := r.PathValue("id")
	id, err := kid.FromString(raw)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}

	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}

	_, authed := s.authenticatedUserID(r)
	if !p.Published && !authed {
		http.NotFound(w, r)
		return nil, false
	}

	return p, true
}

// generateThumb extracts the embedded JPEG preview from a photo using exiftool
// and caches it in .photo/thumbs/<kid-id>.jpg.
// Falls back to -ThumbnailImage for JPEGs that don't have a PreviewImage.
func (s *Server) generateThumb(p *photo.Photo) (string, error) {
	thumbsDir := filepath.Join(s.LibraryRoot, ".photo", "thumbs")
	if err := os.MkdirAll(thumbsDir, 0o755); err != nil {
		return "", fmt.Errorf("create thumbs dir: %w", err)
	}

	thumbPath := filepath.Join(thumbsDir, p.ID.String()+".jpg")
	srcPath := filepath.Join(s.LibraryRoot, p.StoredPath)

	// Try -PreviewImage first (works for RAW, higher quality).
	// Fall back to -ThumbnailImage (smaller, works for JPEG).
	for _, tag := range []string{"-PreviewImage", "-ThumbnailImage"} {
		cmd := exec.Command("exiftool", "-b", tag, "-q", srcPath)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			if err := os.WriteFile(thumbPath, out, 0o644); err != nil {
				return "", fmt.Errorf("write thumb: %w", err)
			}
			return thumbPath, nil
		}
	}

	return "", fmt.Errorf("no embedded preview found in %s", p.StoredPath)
}



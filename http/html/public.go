package html

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
	"golang.org/x/image/draw"
)

const (
	thumbMaxDim  = 400 // thumbnail longest edge in pixels
	thumbQuality = 75  // jpeg quality
)

// cacheImmutable sets headers for content that never changes at this URL.
func cacheImmutable(w http.ResponseWriter, public bool) {
	scope := "private"
	if public {
		scope = "public"
	}
	w.Header().Set("Cache-Control", scope+", max-age=31536000, immutable")
}

// cacheNoStore disables all caching — used for raw file downloads.
func cacheNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
}

// serveImmutable sets immutable cache headers then serves the file.
func serveImmutable(w http.ResponseWriter, r *http.Request, path string, public bool) {
	cacheImmutable(w, public)
	http.ServeFile(w, r, path)
}

// handlePrivatePreview serves a browser-displayable version of a photo.
// For RAW files: extracts the full-resolution embedded JPEG preview via exiftool.
// For JPEG/PNG/HEIC: serves the file directly.
func (s *Server) handlePrivatePreview(w http.ResponseWriter, r *http.Request) {
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

	srcPath := filepath.Join(s.LibraryRoot, p.StoredPath)

	if !p.IsRaw {
		cacheImmutable(w, false)
		http.ServeFile(w, r, srcPath)
		return
	}

	for _, tag := range []string{"-PreviewImage", "-JpgFromRaw", "-LargePreview"} {
		cmd := exec.Command("exiftool", "-b", tag, "-q", srcPath)
		out, err := cmd.Output()
		if err == nil && len(out) > 1024 && isJPEG(out) {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out)))
			w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
			w.WriteHeader(http.StatusOK)
			w.Write(out) //nolint:errcheck
			return
		}
	}

	// No embedded preview — serve the raw file (browser likely can't display it).
	cacheNoStore(w)
	http.ServeFile(w, r, srcPath)
}

// handlePrivateThumb serves (or generates) a thumbnail for authenticated users.
// Used by the grid and detail page when photos may not be published.
func (s *Server) handlePrivateThumb(w http.ResponseWriter, r *http.Request) {
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

	// Serve cached thumbnail if the DB knows about it.
	if p.ThumbPath != nil && *p.ThumbPath != "" {
		thumbFull := filepath.Join(s.LibraryRoot, *p.ThumbPath)
		if _, err := os.Stat(thumbFull); err == nil {
			serveImmutable(w, r, thumbFull, false)
			return
		}
	}

	// Check if file already exists on disk (previous save path write may have failed).
	expectedThumb := filepath.Join(s.LibraryRoot, ".photo", "thumbs", p.ID.String()+".jpg")
	if _, err := os.Stat(expectedThumb); err == nil {
		rel, _ := filepath.Rel(s.LibraryRoot, expectedThumb)
		s.PhotoService.UpdatePhoto(r.Context(), p.ID, photo.PhotoUpdate{ThumbPath: &rel}) //nolint
		serveImmutable(w, r, expectedThumb, false)
		return
	}

	thumbPath, generated, err := s.generateThumb(p)
	if err != nil {
		log.Printf("thumb %s: %v — serving full image", p.ID, err)
		http.ServeFile(w, r, filepath.Join(s.LibraryRoot, p.StoredPath))
		return
	}

	if generated {
		rel, _ := filepath.Rel(s.LibraryRoot, thumbPath)
		if _, err := s.PhotoService.UpdatePhoto(r.Context(), p.ID, photo.PhotoUpdate{
			ThumbPath: &rel,
		}); err != nil {
			log.Printf("thumb %s: save path: %v", p.ID, err)
		}
	}

	serveImmutable(w, r, thumbPath, false)
}

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
	cacheNoStore(w)
	http.ServeFile(w, r, filepath.Join(s.LibraryRoot, p.StoredPath))
}

// handlePublicPhoto serves a browser-displayable version of a published photo.
// For RAW files: extracts the embedded JPEG preview via exiftool.
// For JPEG/PNG/HEIC: serves the file directly.
func (s *Server) handlePublicPhoto(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolvePublicPhoto(w, r)
	if !ok {
		return
	}

	srcPath := filepath.Join(s.LibraryRoot, p.StoredPath)

	if !p.IsRaw {
		cacheImmutable(w, true)
		http.ServeFile(w, r, srcPath)
		return
	}

	for _, tag := range []string{"-PreviewImage", "-JpgFromRaw", "-LargePreview"} {
		cmd := exec.Command("exiftool", "-b", tag, "-q", srcPath)
		out, err := cmd.Output()
		if err == nil && len(out) > 1024 && isJPEG(out) {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out)))
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			w.WriteHeader(http.StatusOK)
			w.Write(out) //nolint:errcheck
			return
		}
	}

	cacheNoStore(w)
	http.ServeFile(w, r, srcPath)
}

// handlePublicThumb serves (or generates and caches) a thumbnail.
func (s *Server) handlePublicThumb(w http.ResponseWriter, r *http.Request) {
	p, ok := s.resolvePublicPhoto(w, r)
	if !ok {
		return
	}

	// Serve cached thumbnail if the DB knows about it.
	if p.ThumbPath != nil && *p.ThumbPath != "" {
		thumbFull := filepath.Join(s.LibraryRoot, *p.ThumbPath)
		if _, err := os.Stat(thumbFull); err == nil {
			serveImmutable(w, r, thumbFull, p.Published)
			return
		}
	}

	// Check if file already exists on disk (previous save path write may have failed).
	expectedThumb := filepath.Join(s.LibraryRoot, ".photo", "thumbs", p.ID.String()+".jpg")
	if _, err := os.Stat(expectedThumb); err == nil {
		rel, _ := filepath.Rel(s.LibraryRoot, expectedThumb)
		s.PhotoService.UpdatePhoto(r.Context(), p.ID, photo.PhotoUpdate{ThumbPath: &rel}) //nolint
		serveImmutable(w, r, expectedThumb, p.Published)
		return
	}

	// Generate thumbnail on first request.
	thumbPath, generated, err := s.generateThumb(p)
	if err != nil {
		log.Printf("thumb %s: generation failed: %v — serving full image", p.ID, err)
		http.ServeFile(w, r, filepath.Join(s.LibraryRoot, p.StoredPath))
		return
	}

	if generated {
		rel, _ := filepath.Rel(s.LibraryRoot, thumbPath)
		if _, err := s.PhotoService.UpdatePhoto(r.Context(), p.ID, photo.PhotoUpdate{
			ThumbPath: &rel,
		}); err != nil {
			log.Printf("thumb %s: save path failed: %v", p.ID, err)
		}
	}

	serveImmutable(w, r, thumbPath, p.Published)
}

// resolvePublicPhoto looks up the photo by ID and enforces visibility.
// Authenticated users (cookie session) can see all photos; public can
// only see published photos.
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

// generateThumb creates a thumbnail and returns its path.
// Returns (path, true, nil) when a new thumbnail was written to disk.
// Returns (srcPath, false, nil) when falling back to the full image.
// Returns ("", false, err) on hard failure.
//
// RAW files: exiftool extracts the embedded full-size JPEG preview.
// JPEG/PNG:  pure Go decode + resize using golang.org/x/image/draw.
func (s *Server) generateThumb(p *photo.Photo) (string, bool, error) {
	thumbsDir := filepath.Join(s.LibraryRoot, ".photo", "thumbs")
	if err := os.MkdirAll(thumbsDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create thumbs dir: %w", err)
	}

	thumbPath := filepath.Join(thumbsDir, p.ID.String()+".jpg")
	srcPath := filepath.Join(s.LibraryRoot, p.StoredPath)

	if p.IsRaw {
		return s.generateThumbRAW(srcPath, thumbPath, p.ID.String())
	}
	return s.generateThumbGo(srcPath, thumbPath, p.ID.String())
}

// generateThumbRAW extracts the embedded JPEG preview from a RAW file using
// exiftool. Nikon NEF files embed a full-resolution JPEG preview that exiftool
// can extract with -b -PreviewImage.
func (s *Server) generateThumbRAW(srcPath, thumbPath, id string) (string, bool, error) {
	// Tags tried in order of preference:
	// -PreviewImage:  full-size embedded JPEG (Nikon, Canon, Sony all embed this)
	// -JpgFromRaw:    alternative tag name used by some manufacturers
	// -LargePreview:  used by some Leica / Panasonic files
	// -ThumbnailImage: small embedded thumbnail (last resort, often tiny)
	tags := []string{"-PreviewImage", "-JpgFromRaw", "-LargePreview", "-ThumbnailImage"}

	for _, tag := range tags {
		cmd := exec.Command("exiftool", "-b", tag, "-q", srcPath)
		out, err := cmd.Output()
		if err != nil {
			log.Printf("thumb %s: exiftool %s failed: %v", id, tag, err)
			continue
		}
		if len(out) < 1024 {
			log.Printf("thumb %s: exiftool %s returned %d bytes (too small, skipping)", id, tag, len(out))
			continue
		}

		// Verify it's actually a JPEG by checking the magic bytes.
		if !isJPEG(out) {
			log.Printf("thumb %s: exiftool %s: output is not JPEG (%d bytes)", id, tag, len(out))
			continue
		}

		log.Printf("thumb %s: extracted %d-byte preview via exiftool %s", id, len(out), tag)

		// Resize the extracted preview to thumbMaxDim.
		resized, err := resizeJPEGBytes(out, thumbMaxDim)
		if err != nil {
			log.Printf("thumb %s: resize extracted preview failed: %v — using as-is", id, err)
			resized = out
		}

		if err := os.WriteFile(thumbPath, resized, 0o644); err != nil {
			return "", false, fmt.Errorf("write thumb: %w", err)
		}
		log.Printf("thumb %s: cached at %s", id, thumbPath)
		return thumbPath, true, nil
	}

	return "", false, fmt.Errorf("no usable embedded preview in %s", srcPath)
}

// generateThumbGo decodes a JPEG or PNG with the standard library and
// resizes it using golang.org/x/image/draw (high quality Catmull-Rom).
func (s *Server) generateThumbGo(srcPath, thumbPath, id string) (string, bool, error) {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return "", false, fmt.Errorf("read source: %w", err)
	}

	resized, err := resizeJPEGBytes(data, thumbMaxDim)
	if err != nil {
		// Can't decode — fall back to full image, don't cache.
		log.Printf("thumb %s: Go resize failed: %v — serving full image", id, err)
		return srcPath, false, nil
	}

	if err := os.WriteFile(thumbPath, resized, 0o644); err != nil {
		return "", false, fmt.Errorf("write thumb: %w", err)
	}
	log.Printf("thumb %s: cached Go-resized thumbnail at %s", id, thumbPath)
	return thumbPath, true, nil
}

// resizeJPEGBytes decodes imgData as JPEG or PNG, scales it so the longest
// edge is at most maxDim pixels, and re-encodes as JPEG.
func resizeJPEGBytes(imgData []byte, maxDim int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Calculate scaled dimensions preserving aspect ratio.
	if w <= maxDim && h <= maxDim {
		// Already small enough — re-encode as JPEG and return.
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: thumbQuality}); err != nil {
			return nil, fmt.Errorf("encode jpeg: %w", err)
		}
		return buf.Bytes(), nil
	}

	var newW, newH int
	if w > h {
		newW = maxDim
		newH = h * maxDim / w
	} else {
		newH = maxDim
		newW = w * maxDim / h
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbQuality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// isJPEG checks that data begins with the JPEG magic bytes FF D8.
func isJPEG(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8
}

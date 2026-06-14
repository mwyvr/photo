// Package importer orchestrates the full import pipeline for a single photo.
// It composes EXIFExtractor, Geocoder, PhotoService — all via interfaces.
//
// Pipeline per file:
//  1. Validate extension is supported.
//  2. Compute SHA-256 hash (duplicate detection).
//  3. Extract EXIF via EXIFExtractor.
//  4. Reverse-geocode GPS coords via Geocoder (if present).
//  5. Compute destination path: LibraryRoot/YYYY/MM/<kid-id>.ext
//  6. Copy/write file to destination.
//  7. Persist Photo record with LocationName denormalized from geocoding.
package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/kid"
)

// Verify compile-time interface satisfaction.
var _ photo.Importer = (*Importer)(nil)

// SupportedExtensions is the set of lowercased file extensions the importer handles.
var SupportedExtensions = map[string]struct{}{
	".jpg": {}, ".jpeg": {},
	".png": {},
	".heic": {}, ".heif": {},
	".cr2": {}, ".cr3": {},
	".nef": {}, ".nrw": {},
	".arw": {}, ".srf": {}, ".sr2": {},
	".raf": {},
	".orf": {},
	".rw2": {},
	".pef": {}, ".ptx": {},
	".dng": {},
	".rwl": {},
	".3fr": {},
}

// Importer orchestrates the full import pipeline.
type Importer struct {
	photoSvc    photo.PhotoService
	extractor   photo.EXIFExtractor
	geocoder    photo.Geocoder // nil is safe; GPS step is skipped
	libraryRoot string
}

// New returns an Importer wired with the given dependencies.
// geocoder may be nil — GPS coordinates will still be stored but not geocoded.
func New(
	photoSvc photo.PhotoService,
	extractor photo.EXIFExtractor,
	geocoder photo.Geocoder,
	libraryRoot string,
) *Importer {
	return &Importer{
		photoSvc:    photoSvc,
		extractor:   extractor,
		geocoder:    geocoder,
		libraryRoot: libraryRoot,
	}
}

// ImportFile imports a file already on the local filesystem (CLI path).
func (imp *Importer) ImportFile(ctx context.Context, srcPath string, opts photo.ImportOptions) photo.ImportResult {
	result := photo.ImportResult{SourcePath: srcPath}

	hash, err := hashFile(srcPath)
	if err != nil {
		result.Err = fmt.Errorf("hash %q: %w", srcPath, err)
		return result
	}

	exifData, err := imp.extractor.Extract(ctx, srcPath)
	if err != nil {
		result.Err = fmt.Errorf("extract EXIF from %q: %w", srcPath, err)
		return result
	}

	if skip, reason := shouldSkip(exifData, opts); skip {
		result.Skipped = true
		result.SkipReason = reason
		return result
	}

	fileInfo, err := os.Stat(srcPath)
	if err != nil {
		result.Err = fmt.Errorf("stat %q: %w", srcPath, err)
		return result
	}

	ext := strings.ToLower(filepath.Ext(srcPath))
	return imp.runPipeline(ctx, pipelineInput{
		filename: filepath.Base(srcPath),
		ext:      ext,
		hash:     hash,
		size:     fileInfo.Size(),
		exif:     exifData,
		copyFn: func(dest string) error {
			return copyFile(srcPath, dest)
		},
	}, opts)
}

// ImportReader imports bytes from r using filename as a format/extension hint (HTTP upload path).
func (imp *Importer) ImportReader(ctx context.Context, r io.Reader, filename string, opts photo.ImportOptions) photo.ImportResult {
	result := photo.ImportResult{SourcePath: filename}

	data, err := io.ReadAll(r)
	if err != nil {
		result.Err = fmt.Errorf("read %q: %w", filename, err)
		return result
	}

	hash := hashBytes(data)

	exifData, err := imp.extractor.ExtractReader(ctx, bytes.NewReader(data), filename)
	if err != nil {
		result.Err = fmt.Errorf("extract EXIF from %q: %w", filename, err)
		return result
	}

	if skip, reason := shouldSkip(exifData, opts); skip {
		result.Skipped = true
		result.SkipReason = reason
		return result
	}

	ext := strings.ToLower(filepath.Ext(filename))
	return imp.runPipeline(ctx, pipelineInput{
		filename: filename,
		ext:      ext,
		hash:     hash,
		size:     int64(len(data)),
		exif:     exifData,
		copyFn: func(dest string) error {
			return writeBytes(data, dest)
		},
	}, opts)
}

// shouldSkip returns true and a reason string if the file should not be imported.
// Called after EXIF extraction so decisions are based on actual file content.
func shouldSkip(exif *photo.EXIFData, opts photo.ImportOptions) (bool, string) {
	// Reject anything that isn't an image, using exiftool's MIME type.
	// An empty MIMEType means exiftool couldn't identify the file at all.
	if exif.MIMEType == "" || !strings.HasPrefix(exif.MIMEType, "image/") {
		ft := exif.FileType
		if ft == "" {
			ft = "unknown"
		}
		return true, fmt.Sprintf("not an image file (type: %s)", ft)
	}

	// If --raw-only is set, skip rendered images (JPEG, PNG, HEIC, etc.).
	if opts.RawOnly && !exif.IsRaw {
		return true, fmt.Sprintf("skipping non-RAW image (type: %s)", exif.FileType)
	}

	return false, ""
}


type pipelineInput struct {
	filename string
	ext      string
	hash     string
	size     int64
	exif     *photo.EXIFData
	copyFn   func(dest string) error
}

// runPipeline is the shared core: geocode → compute path → copy → persist.
func (imp *Importer) runPipeline(ctx context.Context, in pipelineInput, opts photo.ImportOptions) photo.ImportResult {
	result := photo.ImportResult{SourcePath: in.filename}

	// Reverse geocode GPS coordinates to a human-readable location name.
	var locationName string
	if in.exif.GPSLat != nil && in.exif.GPSLon != nil && imp.geocoder != nil {
		loc, err := imp.geocoder.ReverseGeocode(ctx, *in.exif.GPSLat, *in.exif.GPSLon)
		if err != nil && photo.ErrorCode(err) != photo.ENOTFOUND {
			result.Err = fmt.Errorf("reverse geocode (%.6f, %.6f): %w",
				*in.exif.GPSLat, *in.exif.GPSLon, err)
			return result
		}
		if loc != nil {
			locationName = loc.DisplayName()
		}
	}

	// Generate the kid ID for this photo — used as the filename in storage.
	photoID := kid.New()

	// Destination: LibraryRoot/YYYY/MM/<kid-id>.ext
	destPath := imp.destinationPath(photoID, in.exif, in.ext)

	if !opts.DryRun {
		if err := in.copyFn(destPath); err != nil {
			result.Err = fmt.Errorf("write to library %q: %w", destPath, err)
			return result
		}
	}

	p := &photo.Photo{
		ID:            photoID,
		UserID:        opts.UserID,
		Filename:      in.filename,
		StoredPath:    relPath(imp.libraryRoot, destPath),
		SHA256:        in.hash,
		FileType:      in.exif.FileType,
		MIMEType:      in.exif.MIMEType,
		FileSizeBytes: in.size,
		IsRaw:         in.exif.IsRaw,
		Visibility:    opts.Visibility,
		CameraMake:    in.exif.Make,
		CameraModel:   in.exif.Model,
		LensModel:     in.exif.LensModel,
		FocalLength:   in.exif.FocalLength,
		Aperture:      in.exif.Aperture,
		ShutterSpeed:  in.exif.ShutterSpeed,
		ISO:           in.exif.ISO,
		CapturedAt:    in.exif.CapturedAt,
		GPSLat:        in.exif.GPSLat,
		GPSLon:        in.exif.GPSLon,
		LocationName:  locationName,
		EXIFRaw:       in.exif.Raw,
	}

	if !opts.DryRun {
		if err := imp.photoSvc.CreatePhoto(ctx, p); err != nil {
			// Roll back the copied file regardless of error type.
			_ = os.Remove(destPath)
			if photo.ErrorCode(err) == photo.ECONFLICT {
				result.Skipped = true
				result.SkipReason = "duplicate (already in library)"
				return result
			}
			result.Err = fmt.Errorf("save photo record: %w", err)
			return result
		}
	}

	result.Photo = p
	return result
}

// ImportDir walks root recursively and imports every supported file.
// This is CLI-only and intentionally not on the photo.Importer interface.
// progressFn is called after each file; may be nil.
func ImportDir(ctx context.Context, imp *Importer, root string, opts photo.ImportOptions, progressFn func(photo.ImportResult)) ([]photo.ImportResult, error) {
	var results []photo.ImportResult
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		r := imp.ImportFile(ctx, path, opts)
		results = append(results, r)
		if progressFn != nil {
			progressFn(r)
		}
		return nil
	})
	if err != nil {
		return results, fmt.Errorf("walk %q: %w", root, err)
	}
	return results, nil
}

// --- internal helpers -------------------------------------------------------

// destinationPath returns: LibraryRoot/YYYY/MM/<kid-id>.ext
// Year and month come from CapturedAt if available, otherwise time.Now().
func (imp *Importer) destinationPath(id kid.ID, exif *photo.EXIFData, ext string) string {
	t := time.Now()
	if exif.CapturedAt != nil {
		t = *exif.CapturedAt
	}
	return filepath.Join(
		imp.libraryRoot,
		fmt.Sprintf("%04d", t.Year()),
		fmt.Sprintf("%02d", t.Month()),
		id.String()+ext,
	)
}

// sanitizeDirName is kept for any future use but not used in the main path now.
func sanitizeDirName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == ' ' || r == '_':
			b.WriteRune('-')
		case r == '.' || r == '-':
			b.WriteRune(r)
		}
	}
	result := regexp.MustCompile(`-{2,}`).ReplaceAllString(b.String(), "-")
	result = strings.Trim(result, "-")
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("destination already exists: %q", dst)
		}
		return fmt.Errorf("create destination: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("copy data: %w", err)
	}
	return out.Close()
}

func writeBytes(data []byte, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("destination already exists: %q", dst)
		}
		return fmt.Errorf("create destination: %w", err)
	}
	if _, err := out.Write(data); err != nil {
		out.Close()
		os.Remove(dst)
		return fmt.Errorf("write data: %w", err)
	}
	return out.Close()
}

func relPath(base, dst string) string {
	rel, err := filepath.Rel(base, dst)
	if err != nil {
		return dst
	}
	return rel
}

func mimeTypeForExt(ext string) string {
	static := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg",
		".png":  "image/png",
		".heic": "image/heic", ".heif": "image/heif",
		".cr2": "image/x-canon-cr2", ".cr3": "image/x-canon-cr3",
		".nef": "image/x-nikon-nef", ".nrw": "image/x-nikon-nrw",
		".arw": "image/x-sony-arw", ".srf": "image/x-sony-srf", ".sr2": "image/x-sony-sr2",
		".raf": "image/x-fuji-raf",
		".orf": "image/x-olympus-orf",
		".rw2": "image/x-panasonic-rw2",
		".pef": "image/x-pentax-pef", ".ptx": "image/x-pentax-ptx",
		".dng": "image/x-adobe-dng",
		".rwl": "image/x-leica-rwl",
		".3fr": "image/x-hasselblad-3fr",
	}
	if m, ok := static[ext]; ok {
		return m
	}
	if m := mime.TypeByExtension(ext); m != "" {
		return m
	}
	return "application/octet-stream"
}

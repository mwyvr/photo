package photo

import (
	"context"
	"io"
	"time"

	"github.com/mwyvr/kid"
)

// Photo is the central domain type.
type Photo struct {
	ID kid.ID `json:"id"`

	// Owner.
	UserID kid.ID `json:"userId"`

	// Original filename as it existed at import time.
	Filename string `json:"filename"`

	// Path relative to LibraryRoot: YYYY/MM/<kid-id>.ext
	StoredPath string `json:"storedPath"`

	// SHA-256 hex digest. Used for duplicate detection.
	SHA256 string `json:"sha256"`

	MIMEType      string `json:"mimeType"`
	FileSizeBytes int64  `json:"fileSizeBytes"`

	// EXIF-derived fields.
	CameraModel  string     `json:"cameraModel"`
	CameraMake   string     `json:"cameraMake"`
	LensModel    string     `json:"lensModel"`
	FocalLength  string     `json:"focalLength"`
	Aperture     string     `json:"aperture"`
	ShutterSpeed string     `json:"shutterSpeed"`
	ISO          int        `json:"iso"`
	CapturedAt   *time.Time `json:"capturedAt"`

	// GPS coordinates extracted from EXIF.
	GPSLat *float64 `json:"gpsLat,omitempty"`
	GPSLon *float64 `json:"gpsLon,omitempty"`

	// Denormalised human-readable location derived from reverse geocoding.
	// Stored directly on Photo for easy display and search without a join.
	// Example: "Tokyo, Japan"
	LocationName string `json:"locationName,omitempty"`

	// Full exiftool JSON output, stored as a blob. Excluded from API responses.
	EXIFRaw string `json:"-"`

	// User-applied metadata.
	Tags        []*Tag `json:"tags,omitempty"`
	Description string `json:"description"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Validate returns an error if the photo is missing required fields.
func (p *Photo) Validate() error {
	if p.Filename == "" {
		return Errorf(EINVALID, "filename is required")
	}
	if p.StoredPath == "" {
		return Errorf(EINVALID, "stored path is required")
	}
	if p.SHA256 == "" {
		return Errorf(EINVALID, "sha256 is required")
	}
	if p.UserID.IsNil() {
		return Errorf(EINVALID, "user ID is required")
	}
	return nil
}

// HasGPS returns true if the photo has valid GPS coordinates.
func (p *Photo) HasGPS() bool {
	return p.GPSLat != nil && p.GPSLon != nil
}

// TagNames returns the tag names attached to this photo.
func (p *Photo) TagNames() []string {
	names := make([]string, len(p.Tags))
	for i, t := range p.Tags {
		names[i] = t.Name
	}
	return names
}

// PhotoFilter is passed to FindPhotos. All non-nil/non-zero fields are ANDed.
type PhotoFilter struct {
	UserID kid.ID

	CapturedAfter  *time.Time
	CapturedBefore *time.Time

	// Location searches LocationName with a case-insensitive LIKE match.
	Location *string

	// Tags: all listed tags must be present (AND semantics).
	Tags []string

	Offset int
	Limit  int
}

// PhotoUpdate carries mutable fields a caller may change after import.
type PhotoUpdate struct {
	Description *string
}

// ImportOptions configures a single file import.
type ImportOptions struct {
	// DryRun performs all steps except the file copy and database write.
	DryRun bool

	// UserID is the owner of the imported photo. Required.
	UserID kid.ID
}

// ImportResult describes the outcome of importing a single file.
type ImportResult struct {
	SourcePath string
	Photo      *Photo
	Skipped    bool
	SkipReason string
	Err        error
}

// Importer is the interface for importing a single photo into the library.
// ImportDir (directory walking) is CLI-only and lives in cmd/photo.
type Importer interface {
	ImportFile(ctx context.Context, srcPath string, opts ImportOptions) ImportResult
	ImportReader(ctx context.Context, r io.Reader, filename string, opts ImportOptions) ImportResult
}

// PhotoService manages photos.
type PhotoService interface {
	FindPhotoByID(ctx context.Context, id kid.ID) (*Photo, error)
	FindPhotos(ctx context.Context, filter PhotoFilter) ([]*Photo, int, error)
	CreatePhoto(ctx context.Context, photo *Photo) error
	UpdatePhoto(ctx context.Context, id kid.ID, upd PhotoUpdate) (*Photo, error)
	DeletePhoto(ctx context.Context, id kid.ID) error
}

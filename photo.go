package photo

import (
	"context"
	"io"
	"time"

	"github.com/mwyvr/kid"
)

// Visibility controls who can see a photo or album.
type Visibility string

const (
	// VisibilityPrivate is visible only to the owner.
	VisibilityPrivate Visibility = "private"

	// VisibilityHousehold is visible to all authenticated users on this
	// server — intended for couples or small groups sharing a library.
	// The default when HouseholdMode is enabled in server config.
	VisibilityHousehold Visibility = "household"

	// VisibilityPublished is publicly visible: appears in the grid, RSS
	// feed, and is accessible at /p/{id} without authentication.
	VisibilityPublished Visibility = "published"
)

// IsValid reports whether v is a known visibility value.
func (v Visibility) IsValid() bool {
	switch v {
	case VisibilityPrivate, VisibilityHousehold, VisibilityPublished:
		return true
	}
	return false
}

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

	// FileType is exiftool's detected file type string, e.g. "JPEG", "NEF", "CR2".
	// Derived from file content, not the filename extension.
	FileType      string `json:"fileType"`
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
	LocationName string `json:"locationName,omitempty"`

	// ThumbPath is the path relative to LibraryRoot of the cached thumbnail.
	ThumbPath *string `json:"thumbPath,omitempty"`

	// Full exiftool JSON output, stored as a blob.
	EXIFRaw string `json:"exifRaw,omitempty"`

	// IsRaw is true when the file is a camera RAW format (NEF, CR2, ARW, etc.)
	IsRaw bool `json:"isRaw"`

	// Visibility controls who can see this photo.
	// See Visibility constants for semantics.
	Visibility Visibility `json:"visibility"`

	// ShareToken, when non-nil, grants unauthenticated access to this photo
	// via /s/{token} regardless of Visibility. Generate with photo share,
	// revoke by clearing (sets to nil).
	ShareToken *string `json:"shareToken,omitempty"`

	// RawPartnerID points to the RAW/JPEG counterpart; reserved for future use.
	RawPartnerID *kid.ID `json:"rawPartnerId,omitempty"`

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
	if !p.Visibility.IsValid() {
		return Errorf(EINVALID, "invalid visibility value")
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

// IsVisibleTo reports whether the photo is visible to a given viewer.
// viewerID is the authenticated viewer's ID (zero value = unauthenticated).
// ownerID is the photo owner's ID.
// householdMode controls whether household-visibility photos are visible to
// all authenticated users.
func (p *Photo) IsVisibleTo(viewerID, ownerID kid.ID, householdMode bool) bool {
	if p.UserID == viewerID {
		return true // always visible to owner
	}
	switch p.Visibility {
	case VisibilityPublished:
		return true
	case VisibilityHousehold:
		return householdMode && !viewerID.IsNil()
	default: // VisibilityPrivate
		return false
	}
}

// PhotoFilter is passed to FindPhotos. All non-nil/non-zero fields are ANDed.
type PhotoFilter struct {
	// UserID, when set, scopes results to a specific owner.
	UserID kid.ID

	// ViewerID is the authenticated user making the request (zero = unauthed).
	// Combined with HouseholdMode to determine visibility of other users' photos.
	ViewerID kid.ID

	// HouseholdMode, when true, makes household-visibility photos from other
	// users visible to authenticated viewers.
	HouseholdMode bool

	CapturedAfter  *time.Time
	CapturedBefore *time.Time

	// Location searches LocationName with a case-insensitive LIKE match.
	Location *string

	// SHA256 filters to an exact hash match.
	SHA256 *string

	// Tags: all listed tags must be present (AND semantics).
	Tags []string

	// IsRaw filters to RAW-only (true) or non-RAW only (false). Nil = both.
	IsRaw *bool

	// MissingLocation filters to photos with GPS but no location name.
	MissingLocation *bool

	Offset int
	Limit  int
}

// PhotoUpdate carries mutable fields a caller may change after import.
// Nil pointer fields are left unchanged.
type PhotoUpdate struct {
	Description  *string
	LocationName *string
	Visibility   *Visibility
	ShareToken   *string // set to "" to clear/revoke
	ThumbPath    *string
}

// ImportOptions configures a single file import.
type ImportOptions struct {
	DryRun  bool
	UserID  kid.ID
	RawOnly bool

	// Visibility sets the initial visibility of the imported photo.
	// Defaults to VisibilityHousehold when HouseholdMode is enabled.
	Visibility Visibility
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
type Importer interface {
	ImportFile(ctx context.Context, srcPath string, opts ImportOptions) ImportResult
	ImportReader(ctx context.Context, r io.Reader, filename string, opts ImportOptions) ImportResult
}

// PhotoService manages photos.
type PhotoService interface {
	FindPhotoByID(ctx context.Context, id kid.ID) (*Photo, error)
	FindPhotoByShareToken(ctx context.Context, token string) (*Photo, error)
	FindPhotos(ctx context.Context, filter PhotoFilter) ([]*Photo, int, error)
	CreatePhoto(ctx context.Context, photo *Photo) error
	UpdatePhoto(ctx context.Context, id kid.ID, upd PhotoUpdate) (*Photo, error)
	DeletePhoto(ctx context.Context, id kid.ID) error
	GenerateShareToken(ctx context.Context, id kid.ID) (string, error)
}

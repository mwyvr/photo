package photo

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mwyvr/kid"
)

// Album is a named, ordered collection of photos.
// Photos may belong to multiple albums; each album maintains its own ordering.
type Album struct {
	ID           kid.ID  `json:"id"`
	UserID       kid.ID  `json:"userId"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Description  string  `json:"description"`
	CoverPhotoID *kid.ID `json:"coverPhotoId,omitempty"`
	PhotoCount   int     `json:"photoCount"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Validate returns an error if required fields are missing.
func (a *Album) Validate() error {
	if a.Name == "" {
		return Errorf(EINVALID, "album name is required")
	}
	if a.UserID.IsNil() {
		return Errorf(EINVALID, "album user ID is required")
	}
	if a.Slug == "" {
		return Errorf(EINVALID, "album slug is required")
	}
	return nil
}

var (
	slugNonAlnum  = regexp.MustCompile(`[^a-z0-9]+`)
	slugMultiDash = regexp.MustCompile(`-{2,}`)
)

// Slugify converts a human-readable name into a URL-safe slug.
// Examples:
//
//	"France 2024"     → "france-2024"
//	"Hiking/Camping"  → "hiking-camping"
//	"Black & White"   → "black-white"
//	"Dawson Creek, BC"→ "dawson-creek-bc"
func Slugify(name string) string {
	// Normalise unicode — replace accented chars with their ASCII base where possible.
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	s := slugNonAlnum.ReplaceAllString(b.String(), "-")
	s = slugMultiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "album"
	}
	return s
}

// AlbumPhoto represents a photo's membership in an album, including its
// position within that album's ordering.
type AlbumPhoto struct {
	AlbumID  kid.ID `json:"albumId"`
	PhotoID  kid.ID `json:"photoId"`
	Position int    `json:"position"`
}

// AlbumFilter is passed to FindAlbums.
type AlbumFilter struct {
	UserID kid.ID
	Offset int
	Limit  int
}

// AlbumUpdate carries mutable fields on an album.
type AlbumUpdate struct {
	Name         *string
	Description  *string
	CoverPhotoID *kid.ID
}

// AlbumService manages albums and their photo memberships.
type AlbumService interface {
	// FindAlbumByID retrieves a single album by ID including its photo count.
	FindAlbumByID(ctx context.Context, id kid.ID) (*Album, error)

	// FindAlbumBySlug retrieves an album by its URL slug.
	// Returns ENOTFOUND if the slug does not exist.
	FindAlbumBySlug(ctx context.Context, slug string) (*Album, error)

	// FindAlbums retrieves albums matching the filter.
	FindAlbums(ctx context.Context, filter AlbumFilter) ([]*Album, int, error)

	// CreateAlbum persists a new album. On success, album.ID and album.Slug are set.
	// Returns ECONFLICT if the slug is already in use.
	CreateAlbum(ctx context.Context, album *Album) error

	// UpdateAlbum updates mutable fields on an album.
	UpdateAlbum(ctx context.Context, id kid.ID, upd AlbumUpdate) (*Album, error)

	// DeleteAlbum removes an album and all its photo memberships.
	DeleteAlbum(ctx context.Context, id kid.ID) error

	// AddPhoto adds a photo to an album at the end of the current ordering.
	AddPhoto(ctx context.Context, albumID, photoID kid.ID) error

	// RemovePhoto removes a photo from an album.
	RemovePhoto(ctx context.Context, albumID, photoID kid.ID) error

	// MovePhoto repositions a photo within an album's ordering.
	MovePhoto(ctx context.Context, albumID, photoID, afterPhotoID kid.ID) error

	// FindAlbumPhotos retrieves the ordered list of photos in an album.
	FindAlbumPhotos(ctx context.Context, albumID kid.ID, offset, limit int) ([]*Photo, int, error)
}


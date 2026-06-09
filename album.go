package photo

import (
	"context"
	"time"

	"github.com/mwyvr/kid"
)

// Album is a named, ordered collection of photos.
// Photos may belong to multiple albums; each album maintains its own ordering.
type Album struct {
	ID          kid.ID    `json:"id"`
	UserID      kid.ID    `json:"userId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CoverPhotoID *kid.ID  `json:"coverPhotoId,omitempty"`
	PhotoCount  int       `json:"photoCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Validate returns an error if required fields are missing.
func (a *Album) Validate() error {
	if a.Name == "" {
		return Errorf(EINVALID, "album name is required")
	}
	if a.UserID.IsNil() {
		return Errorf(EINVALID, "album user ID is required")
	}
	return nil
}

// AlbumPhoto represents a photo's membership in an album, including its
// position within that album's ordering.
type AlbumPhoto struct {
	AlbumID  kid.ID `json:"albumId"`
	PhotoID  kid.ID `json:"photoId"`
	Position int    `json:"position"` // 1-based; determines display order
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
	CoverPhotoID *kid.ID // set to &kid.ID{} (nil kid) to clear the cover
}

// AlbumService manages albums and their photo memberships.
type AlbumService interface {
	// FindAlbumByID retrieves a single album by ID including its photo count.
	// Returns ENOTFOUND if the ID does not exist.
	FindAlbumByID(ctx context.Context, id kid.ID) (*Album, error)

	// FindAlbums retrieves albums matching the filter.
	// Also returns the total count of matching albums.
	FindAlbums(ctx context.Context, filter AlbumFilter) ([]*Album, int, error)

	// CreateAlbum persists a new album. On success, album.ID is set.
	CreateAlbum(ctx context.Context, album *Album) error

	// UpdateAlbum updates mutable fields on an album.
	// Returns ENOTFOUND if the album does not exist.
	UpdateAlbum(ctx context.Context, id kid.ID, upd AlbumUpdate) (*Album, error)

	// DeleteAlbum removes an album and all its photo memberships.
	// Does NOT delete the photos themselves.
	// Returns ENOTFOUND if the album does not exist.
	DeleteAlbum(ctx context.Context, id kid.ID) error

	// AddPhoto adds a photo to an album at the end of the current ordering.
	// Is a no-op if the photo is already in the album.
	// Returns ENOTFOUND if either the album or photo does not exist.
	AddPhoto(ctx context.Context, albumID, photoID kid.ID) error

	// RemovePhoto removes a photo from an album.
	// Returns ENOTFOUND if the association does not exist.
	RemovePhoto(ctx context.Context, albumID, photoID kid.ID) error

	// MovePhoto repositions a photo within an album's ordering.
	// afterPhotoID is the photo after which the moved photo should appear.
	// Pass a zero kid.ID to move the photo to the beginning of the album.
	// Returns ENOTFOUND if the album, moved photo, or after photo is not in the album.
	MovePhoto(ctx context.Context, albumID, photoID, afterPhotoID kid.ID) error

	// FindAlbumPhotos retrieves the ordered list of photos in an album.
	// Also returns the total count.
	FindAlbumPhotos(ctx context.Context, albumID kid.ID, offset, limit int) ([]*Photo, int, error)
}

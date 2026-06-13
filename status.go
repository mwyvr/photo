package photo

import (
	"context"
	"time"

	"github.com/mwyvr/kid"
)

// LibraryStatus holds aggregate statistics about the photo library, or about
// a single user's photos when scoped.
type LibraryStatus struct {
	// Photo counts.
	TotalPhotos    int `json:"totalPhotos"`
	TotalRAW       int `json:"totalRaw"`
	TotalNonRAW    int `json:"totalNonRaw"`
	TotalPublished int `json:"totalPublished"`

	// Storage.
	TotalSizeBytes int64 `json:"totalSizeBytes"`

	// Metadata coverage.
	WithLocation    int `json:"withLocation"`
	WithoutLocation int `json:"withoutLocation"`
	WithGPS         int `json:"withGps"`
	WithDescription int `json:"withDescription"`

	// Tags and albums.
	TotalTags   int `json:"totalTags"`
	TotalAlbums int `json:"totalAlbums"`

	// Date range of captured photos. Nil if no photos with capture dates exist.
	OldestCapturedAt *time.Time `json:"oldestCapturedAt"`
	NewestCapturedAt *time.Time `json:"newestCapturedAt"`
}

// StatusService computes library statistics.
type StatusService interface {
	// LibraryStatus returns aggregate statistics. If userID is non-nil,
	// statistics are scoped to that user's photos only; if nil, statistics
	// cover the entire library across all users.
	LibraryStatus(ctx context.Context, userID *kid.ID) (*LibraryStatus, error)
}

package photo

import (
	"context"
	"time"
)

// LibraryStatus holds aggregate statistics about the photo library.
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
	// LibraryStatus returns aggregate statistics for the library.
	LibraryStatus(ctx context.Context) (*LibraryStatus, error)
}

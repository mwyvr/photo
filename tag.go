package photo

import (
	"context"
	"strings"

	"github.com/mwyvr/kid"
)

// Tag is a user-defined label attachable to photos.
type Tag struct {
	ID   kid.ID `json:"id"`
	Name string `json:"name"` // always lowercase
}

// NormalizeTagName returns a canonical lowercase tag name.
func NormalizeTagName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// TagFilter is passed to FindTags.
type TagFilter struct {
	NamePrefix *string
	Offset     int
	Limit      int
}

// TagService manages tags.
type TagService interface {
	FindTagByName(ctx context.Context, name string) (*Tag, error)
	FindOrCreateTag(ctx context.Context, name string) (*Tag, error)
	FindTags(ctx context.Context, filter TagFilter) ([]*Tag, int, error)
	AttachTag(ctx context.Context, photoID kid.ID, tagID kid.ID) error
	DetachTag(ctx context.Context, photoID kid.ID, tagID kid.ID) error
}

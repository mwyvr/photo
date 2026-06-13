package photo

import (
	"context"
	"io"
)

// BackupService produces a consistent snapshot of the library database.
type BackupService interface {
	// Backup writes a complete database snapshot to w.
	// Safe to call while the database is in use.
	Backup(ctx context.Context, w io.Writer) error
}

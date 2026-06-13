package sqlite

import (
	"context"
	"io"
)

// BackupService implements photo.BackupService using SQLite's VACUUM INTO.
type BackupService struct{ db *DB }

func NewBackupService(db *DB) *BackupService { return &BackupService{db: db} }

func (s *BackupService) Backup(ctx context.Context, w io.Writer) error {
	return s.db.Backup(ctx, w)
}

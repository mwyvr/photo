package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mwyvr/photo"
)

// StatusService implements photo.StatusService using SQLite.
type StatusService struct{ db *DB }

func NewStatusService(db *DB) *StatusService { return &StatusService{db: db} }

func (s *StatusService) LibraryStatus(ctx context.Context) (*photo.LibraryStatus, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	st := &photo.LibraryStatus{}

	// Photo counts and storage in one pass.
	err = tx.QueryRowContext(ctx, `
		SELECT
			COUNT(*)                                      AS total,
			COALESCE(SUM(CASE WHEN is_raw = 1 THEN 1 ELSE 0 END), 0)       AS total_raw,
			COALESCE(SUM(CASE WHEN is_raw = 0 THEN 1 ELSE 0 END), 0)       AS total_non_raw,
			COALESCE(SUM(CASE WHEN published = 1 THEN 1 ELSE 0 END), 0)    AS total_published,
			COALESCE(SUM(file_size_bytes), 0)             AS total_bytes,
			COALESCE(SUM(CASE WHEN location_name != '' THEN 1 ELSE 0 END), 0) AS with_location,
			COALESCE(SUM(CASE WHEN location_name = ''  THEN 1 ELSE 0 END), 0) AS without_location,
			COALESCE(SUM(CASE WHEN gps_lat IS NOT NULL THEN 1 ELSE 0 END), 0) AS with_gps,
			COALESCE(SUM(CASE WHEN description != '' THEN 1 ELSE 0 END), 0)   AS with_description
		FROM photos
	`).Scan(
		&st.TotalPhotos,
		&st.TotalRAW,
		&st.TotalNonRAW,
		&st.TotalPublished,
		&st.TotalSizeBytes,
		&st.WithLocation,
		&st.WithoutLocation,
		&st.WithGPS,
		&st.WithDescription,
	)
	if err != nil {
		return nil, fmt.Errorf("status: photo counts: %w", err)
	}

	// Date range.
	var oldest, newest NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT MIN(captured_at), MAX(captured_at)
		FROM photos
		WHERE captured_at IS NOT NULL
	`).Scan(&oldest, &newest)
	if err != nil {
		return nil, fmt.Errorf("status: date range: %w", err)
	}
	if t := oldest.Time(); !t.IsZero() {
		st.OldestCapturedAt = &t
	}
	if t := newest.Time(); !t.IsZero() {
		st.NewestCapturedAt = &t
	}

	// Tag count.
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags`).Scan(&st.TotalTags); err != nil {
		return nil, fmt.Errorf("status: tag count: %w", err)
	}

	// Album count — table may not exist yet during migration; handle gracefully.
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM albums`).Scan(&st.TotalAlbums)
	if err != nil {
		st.TotalAlbums = 0 // albums table not yet created
	}

	return st, nil
}

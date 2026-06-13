package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// StatusService implements photo.StatusService using SQLite.
type StatusService struct{ db *DB }

func NewStatusService(db *DB) *StatusService { return &StatusService{db: db} }

func (s *StatusService) LibraryStatus(ctx context.Context, userID *kid.ID) (*photo.LibraryStatus, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	st := &photo.LibraryStatus{}

	where := "1 = 1"
	args := []interface{}{}
	if userID != nil {
		where = "user_id = ?"
		args = append(args, *userID)
	}

	// Photo counts and storage in one pass.
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
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
		WHERE %s
	`, where), args...).Scan(
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
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT MIN(captured_at), MAX(captured_at)
		FROM photos
		WHERE captured_at IS NOT NULL AND %s
	`, where), args...).Scan(&oldest, &newest)
	if err != nil {
		return nil, fmt.Errorf("status: date range: %w", err)
	}
	if t := oldest.Time(); !t.IsZero() {
		st.OldestCapturedAt = &t
	}
	if t := newest.Time(); !t.IsZero() {
		st.NewestCapturedAt = &t
	}

	// Tag count — tags are global, not per-photo-owner, so only meaningful
	// for the system-wide view. For a user-scoped view we count distinct
	// tags attached to that user's photos.
	if userID == nil {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags`).Scan(&st.TotalTags); err != nil {
			return nil, fmt.Errorf("status: tag count: %w", err)
		}
	} else {
		err = tx.QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT pt.tag_id)
			FROM photo_tags pt
			JOIN photos p ON p.id = pt.photo_id
			WHERE p.user_id = ?
		`, *userID).Scan(&st.TotalTags)
		if err != nil {
			return nil, fmt.Errorf("status: tag count (user): %w", err)
		}
	}

	// Album count.
	albumWhere := "1 = 1"
	albumArgs := []interface{}{}
	if userID != nil {
		albumWhere = "user_id = ?"
		albumArgs = append(albumArgs, *userID)
	}
	err = tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM albums WHERE %s`, albumWhere), albumArgs...,
	).Scan(&st.TotalAlbums)
	if err != nil {
		st.TotalAlbums = 0 // albums table not yet created in old DBs
	}

	return st, nil
}

package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// AlbumService implements photo.AlbumService using SQLite.
type AlbumService struct{ db *DB }

func NewAlbumService(db *DB) *AlbumService { return &AlbumService{db: db} }

// MigrateExistingSlugs populates slug for any albums that have an empty slug.
func (s *AlbumService) MigrateExistingSlugs(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id, name FROM albums WHERE slug = ''`)
	if err != nil {
		return fmt.Errorf("migrate slugs: query: %w", err)
	}
	defer rows.Close()

	type row struct{ id, name string }
	var needSlug []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return err
		}
		needSlug = append(needSlug, r)
	}
	rows.Close()

	for _, r := range needSlug {
		slug := s.uniqueSlug(ctx, tx, photo.Slugify(r.name), "")
		if _, err := tx.ExecContext(ctx,
			`UPDATE albums SET slug = ? WHERE id = ?`, slug, r.id,
		); err != nil {
			return fmt.Errorf("migrate slug for %s: %w", r.id, err)
		}
	}
	return tx.Commit()
}

func (s *AlbumService) FindAlbumByID(ctx context.Context, id kid.ID) (*photo.Album, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return findAlbumByID(ctx, tx, id)
}

func (s *AlbumService) FindAlbumBySlug(ctx context.Context, slug string) (*photo.Album, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
		SELECT a.id, a.user_id, a.name, a.slug, a.description, a.cover_photo_id,
		       COUNT(ap.photo_id) AS photo_count,
		       a.visibility, a.share_token,
		       a.created_at, a.updated_at
		FROM albums a
		LEFT JOIN album_photos ap ON ap.album_id = a.id
		WHERE a.slug = ?
		GROUP BY a.id
	`, slug)
	a, err := scanAlbum(row)
	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "album not found: %s", slug)
	}
	return a, err
}

func (s *AlbumService) FindAlbumByShareToken(ctx context.Context, token string) (*photo.Album, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
		SELECT a.id, a.user_id, a.name, a.slug, a.description, a.cover_photo_id,
		       COUNT(ap.photo_id) AS photo_count,
		       a.visibility, a.share_token,
		       a.created_at, a.updated_at
		FROM albums a
		LEFT JOIN album_photos ap ON ap.album_id = a.id
		WHERE a.share_token = ?
		GROUP BY a.id
	`, token)
	a, err := scanAlbum(row)
	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "album not found")
	}
	return a, err
}

func (s *AlbumService) FindAlbums(ctx context.Context, filter photo.AlbumFilter) ([]*photo.Album, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	where := []string{}
	args := []interface{}{}

	if !filter.UserID.IsNil() {
		if !filter.ViewerID.IsNil() && filter.HouseholdMode {
			where = append(where, "(a.user_id = ? OR a.visibility IN ('household', 'published'))")
			args = append(args, filter.UserID)
		} else if !filter.ViewerID.IsNil() {
			where = append(where, "(a.user_id = ? OR a.visibility = 'published')")
			args = append(args, filter.UserID)
		} else {
			where = append(where, "a.user_id = ?")
			args = append(args, filter.UserID)
		}
	} else if !filter.ViewerID.IsNil() {
		if filter.HouseholdMode {
			where = append(where, "a.visibility IN ('household', 'published')")
		} else {
			where = append(where, "a.visibility = 'published'")
		}
	} else {
		where = append(where, "a.visibility = 'published'")
	}

	clause := "1 = 1"
	if len(where) > 0 {
		clause = strings.Join(where, " AND ")
	}

	var n int
	if err := tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM albums a WHERE %s`, clause), args...,
	).Scan(&n); err != nil {
		return nil, 0, fmt.Errorf("count albums: %w", err)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT a.id, a.user_id, a.name, a.slug, a.description, a.cover_photo_id,
		       COUNT(ap.photo_id) AS photo_count,
		       a.visibility, a.share_token,
		       a.created_at, a.updated_at
		FROM albums a
		LEFT JOIN album_photos ap ON ap.album_id = a.id
		WHERE %s
		GROUP BY a.id
		ORDER BY a.name ASC
		%s
	`, clause, FormatLimitOffset(filter.Limit, filter.Offset)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query albums: %w", err)
	}
	defer rows.Close()

	var albums []*photo.Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, 0, err
		}
		albums = append(albums, a)
	}
	return albums, n, rows.Err()
}

func (s *AlbumService) CreateAlbum(ctx context.Context, a *photo.Album) error {
	if a.Slug == "" && a.Name != "" {
		a.Slug = photo.Slugify(a.Name)
	}
	if a.Visibility == "" {
		a.Visibility = photo.VisibilityHousehold
	}
	if err := a.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	a.Slug = s.uniqueSlug(ctx, tx, a.Slug, "")
	a.ID = kid.New()
	a.CreatedAt = tx.now
	a.UpdatedAt = tx.now

	_, err = tx.ExecContext(ctx, `
		INSERT INTO albums (id, user_id, name, slug, description, cover_photo_id, visibility, share_token, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.UserID, a.Name, a.Slug, a.Description, nullKidID(a.CoverPhotoID),
		string(a.Visibility), a.ShareToken,
		tx.nowStr(), tx.nowStr(),
	)
	if err != nil {
		return FormatError(err)
	}
	return tx.Commit()
}

func (s *AlbumService) uniqueSlug(ctx context.Context, tx *Tx, base, excludeID string) string {
	slug := base
	for i := 2; ; i++ {
		var count int
		q := `SELECT COUNT(*) FROM albums WHERE slug = ?`
		args := []interface{}{slug}
		if excludeID != "" {
			q += ` AND id != ?`
			args = append(args, excludeID)
		}
		tx.QueryRowContext(ctx, q, args...).Scan(&count) //nolint
		if count == 0 {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func (s *AlbumService) UpdateAlbum(ctx context.Context, id kid.ID, upd photo.AlbumUpdate) (*photo.Album, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	a, err := findAlbumByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if v := upd.Name; v != nil {
		a.Name = *v
	}
	if v := upd.Description; v != nil {
		a.Description = *v
	}
	if upd.CoverPhotoID != nil {
		if upd.CoverPhotoID.IsNil() {
			a.CoverPhotoID = nil
		} else {
			a.CoverPhotoID = upd.CoverPhotoID
		}
	}
	if v := upd.Visibility; v != nil {
		a.Visibility = *v
	}
	if upd.ShareToken != nil {
		if *upd.ShareToken == "" {
			a.ShareToken = nil
		} else {
			t := *upd.ShareToken
			a.ShareToken = &t
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE albums SET name = ?, description = ?, cover_photo_id = ?,
		visibility = ?, share_token = ?, updated_at = ?
		WHERE id = ?`,
		a.Name, a.Description, nullKidID(a.CoverPhotoID),
		string(a.Visibility), a.ShareToken, tx.nowStr(), id,
	); err != nil {
		return nil, fmt.Errorf("update album: %w", err)
	}
	a.UpdatedAt = tx.now
	return a, tx.Commit()
}

// GenerateShareToken generates a random token for album link-sharing.
func (s *AlbumService) GenerateShareToken(ctx context.Context, id kid.ID) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate share token: %w", err)
	}
	token := hex.EncodeToString(b)
	if _, err := s.UpdateAlbum(ctx, id, photo.AlbumUpdate{ShareToken: &token}); err != nil {
		return "", err
	}
	return token, nil
}

func (s *AlbumService) DeleteAlbum(ctx context.Context, id kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete album: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return photo.Errorf(photo.ENOTFOUND, "album not found: %s", id)
	}
	return tx.Commit()
}

func (s *AlbumService) AddPhoto(ctx context.Context, albumID, photoID kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := findAlbumByID(ctx, tx, albumID); err != nil {
		return err
	}
	if _, err := findPhotoByID(ctx, tx, photoID); err != nil {
		return err
	}
	var maxPos int
	tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(position), 0) FROM album_photos WHERE album_id = ?`, albumID,
	).Scan(&maxPos) //nolint
	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO album_photos (album_id, photo_id, position) VALUES (?, ?, ?)`,
		albumID, photoID, maxPos+1,
	)
	if err != nil {
		return FormatError(err)
	}
	return tx.Commit()
}

func (s *AlbumService) RemovePhoto(ctx context.Context, albumID, photoID kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		`DELETE FROM album_photos WHERE album_id = ? AND photo_id = ?`, albumID, photoID,
	)
	if err != nil {
		return fmt.Errorf("remove photo from album: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return photo.Errorf(photo.ENOTFOUND, "photo not in album")
	}
	return tx.Commit()
}

func (s *AlbumService) MovePhoto(ctx context.Context, albumID, photoID, afterPhotoID kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var newPos int
	if afterPhotoID.IsNil() {
		newPos = 0
	} else {
		err = tx.QueryRowContext(ctx,
			`SELECT position FROM album_photos WHERE album_id = ? AND photo_id = ?`,
			albumID, afterPhotoID,
		).Scan(&newPos)
		if err == sql.ErrNoRows {
			return photo.Errorf(photo.ENOTFOUND, "reference photo not in album")
		} else if err != nil {
			return fmt.Errorf("find reference position: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE album_photos SET position = position + 1
		WHERE album_id = ? AND position > ?`, albumID, newPos,
	); err != nil {
		return fmt.Errorf("shift positions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE album_photos SET position = ?
		WHERE album_id = ? AND photo_id = ?`, newPos+1, albumID, photoID,
	); err != nil {
		return fmt.Errorf("set new position: %w", err)
	}
	return tx.Commit()
}

func (s *AlbumService) FindAlbumPhotos(ctx context.Context, albumID kid.ID, offset, limit int) ([]*photo.Photo, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM album_photos WHERE album_id = ?`, albumID,
	).Scan(&n); err != nil {
		return nil, 0, fmt.Errorf("count album photos: %w", err)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s
		FROM photos p
		JOIN album_photos ap ON ap.photo_id = p.id
		WHERE ap.album_id = ?
		ORDER BY ap.position ASC
		%s
	`, photoSelectColsAliased, FormatLimitOffset(limit, offset)), albumID)
	if err != nil {
		return nil, 0, fmt.Errorf("query album photos: %w", err)
	}
	defer rows.Close()

	var photos []*photo.Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, 0, err
		}
		if err := attachTags(ctx, tx, p); err != nil {
			return nil, 0, err
		}
		photos = append(photos, p)
	}
	return photos, n, rows.Err()
}

// --- internal helpers -------------------------------------------------------

func findAlbumByID(ctx context.Context, tx *Tx, id kid.ID) (*photo.Album, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT a.id, a.user_id, a.name, a.slug, a.description, a.cover_photo_id,
		       COUNT(ap.photo_id) AS photo_count,
		       a.visibility, a.share_token,
		       a.created_at, a.updated_at
		FROM albums a
		LEFT JOIN album_photos ap ON ap.album_id = a.id
		WHERE a.id = ?
		GROUP BY a.id
	`, id)
	a, err := scanAlbum(row)
	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "album not found: %s", id)
	}
	return a, err
}

type albumScanner interface {
	Scan(dest ...interface{}) error
}

func scanAlbum(s albumScanner) (*photo.Album, error) {
	var a photo.Album
	var coverPhotoID sql.NullString
	var visibility string
	var shareToken sql.NullString
	var createdAt, updatedAt NullTime

	err := s.Scan(
		&a.ID, &a.UserID, &a.Name, &a.Slug, &a.Description, &coverPhotoID,
		&a.PhotoCount,
		&visibility, &shareToken,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if coverPhotoID.Valid {
		id, err := kid.FromString(coverPhotoID.String)
		if err == nil {
			a.CoverPhotoID = &id
		}
	}
	a.Visibility = photo.Visibility(visibility)
	if shareToken.Valid {
		a.ShareToken = &shareToken.String
	}
	a.CreatedAt = createdAt.Time()
	a.UpdatedAt = updatedAt.Time()
	return &a, nil
}

func nullKidID(id *kid.ID) interface{} {
	if id == nil || id.IsNil() {
		return nil
	}
	return id.String()
}

// photoSelectColsAliased is used in FindAlbumPhotos where photos is aliased as p.
const photoSelectColsAliased = `p.id, p.user_id, p.filename, p.stored_path, p.sha256, p.mime_type, p.file_size_bytes,
	       p.camera_make, p.camera_model, p.lens_model, p.focal_length,
	       p.aperture, p.shutter_speed, p.iso, p.gps_lat, p.gps_lon,
	       p.captured_at, p.location_name, p.exif_raw, p.description,
	       p.is_raw, p.raw_partner_id, p.file_type,
	       p.visibility, p.share_token, p.thumb_path,
	       p.created_at, p.updated_at`

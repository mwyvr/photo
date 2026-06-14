package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// PhotoService implements photo.PhotoService using SQLite.
type PhotoService struct{ db *DB }

func NewPhotoService(db *DB) *PhotoService { return &PhotoService{db: db} }

func (s *PhotoService) FindPhotoByID(ctx context.Context, id kid.ID) (*photo.Photo, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := findPhotoByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := attachTags(ctx, tx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PhotoService) FindPhotoByShareToken(ctx context.Context, token string) (*photo.Photo, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, photoSelectCols+` WHERE share_token = ?`, token)
	p, err := scanPhoto(row)
	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "photo not found")
	}
	if err != nil {
		return nil, err
	}
	if err := attachTags(ctx, tx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *PhotoService) FindPhotos(ctx context.Context, filter photo.PhotoFilter) ([]*photo.Photo, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	photos, n, err := findPhotos(ctx, tx, filter)
	if err != nil {
		return nil, 0, err
	}
	for _, p := range photos {
		if err := attachTags(ctx, tx, p); err != nil {
			return nil, 0, err
		}
	}
	return photos, n, nil
}

func (s *PhotoService) CreatePhoto(ctx context.Context, p *photo.Photo) error {
	if p.Visibility == "" {
		p.Visibility = photo.VisibilityHousehold
	}
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := createPhoto(ctx, tx, p); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PhotoService) UpdatePhoto(ctx context.Context, id kid.ID, upd photo.PhotoUpdate) (*photo.Photo, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	p, err := findPhotoByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if v := upd.Description; v != nil {
		p.Description = *v
	}
	if v := upd.LocationName; v != nil {
		p.LocationName = *v
	}
	if v := upd.Visibility; v != nil {
		p.Visibility = *v
	}
	if v := upd.ThumbPath; v != nil {
		p.ThumbPath = v
	}
	if upd.ShareToken != nil {
		if *upd.ShareToken == "" {
			p.ShareToken = nil // revoke
		} else {
			s := *upd.ShareToken
			p.ShareToken = &s
		}
	}

	_, err = tx.ExecContext(ctx,
		`UPDATE photos SET description = ?, location_name = ?, visibility = ?,
		 share_token = ?, thumb_path = ?, updated_at = ? WHERE id = ?`,
		p.Description, p.LocationName, string(p.Visibility),
		p.ShareToken, p.ThumbPath, tx.nowStr(), id,
	)
	if err != nil {
		return nil, fmt.Errorf("update photo: %w", err)
	}
	p.UpdatedAt = tx.now
	return p, tx.Commit()
}

func (s *PhotoService) DeletePhoto(ctx context.Context, id kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM photos WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete photo: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return photo.Errorf(photo.ENOTFOUND, "photo not found: %s", id)
	}
	return tx.Commit()
}

// GenerateShareToken generates a cryptographically random token, stores it on
// the photo, and returns it. If the photo already has a token, it is replaced.
func (s *PhotoService) GenerateShareToken(ctx context.Context, id kid.ID) (string, error) {
	b := make([]byte, 24) // 192 bits → 48 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate share token: %w", err)
	}
	token := hex.EncodeToString(b)
	if _, err := s.UpdatePhoto(ctx, id, photo.PhotoUpdate{ShareToken: &token}); err != nil {
		return "", err
	}
	return token, nil
}

// --- internal helpers -------------------------------------------------------

func findPhotoByID(ctx context.Context, tx *Tx, id kid.ID) (*photo.Photo, error) {
	row := tx.QueryRowContext(ctx, photoSelectCols+` WHERE id = ?`, id)
	p, err := scanPhoto(row)
	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "photo not found: %s", id)
	}
	return p, err
}

func findPhotos(ctx context.Context, tx *Tx, filter photo.PhotoFilter) ([]*photo.Photo, int, error) {
	where := []string{}
	args := []interface{}{}

	if !filter.UserID.IsNil() {
		// Scoped to a specific owner — always show their own photos.
		// Additionally show other users' photos based on visibility and mode.
		if !filter.ViewerID.IsNil() && filter.HouseholdMode {
			// Household mode: show owner's photos + other users' household/published photos.
			where = append(where, "(user_id = ? OR visibility IN ('household', 'published'))")
			args = append(args, filter.UserID)
		} else if !filter.ViewerID.IsNil() {
			// Authenticated viewer, no household: owner's photos + published from others.
			where = append(where, "(user_id = ? OR visibility = 'published')")
			args = append(args, filter.UserID)
		} else {
			where = append(where, "user_id = ?")
			args = append(args, filter.UserID)
		}
	} else if filter.SHA256 != nil {
		// SHA256 is an internal exact-match lookup (duplicate detection).
		// No visibility filtering — the caller is responsible for scoping.
	} else if !filter.ViewerID.IsNil() {
		// No owner filter — viewer sees photos based on visibility.
		if filter.HouseholdMode {
			where = append(where, "visibility IN ('household', 'published')")
		} else {
			where = append(where, "visibility = 'published'")
		}
	} else {
		// Unauthenticated: published only.
		where = append(where, "visibility = 'published'")
	}

	if v := filter.CapturedAfter; v != nil {
		where = append(where, "captured_at >= ?")
		args = append(args, v.UTC().Format(time.RFC3339))
	}
	if v := filter.CapturedBefore; v != nil {
		where = append(where, "captured_at <= ?")
		args = append(args, v.UTC().Format(time.RFC3339))
	}
	if v := filter.Location; v != nil {
		where = append(where, "LOWER(location_name) LIKE '%' || LOWER(?) || '%'")
		args = append(args, *v)
	}
	if v := filter.IsRaw; v != nil {
		where = append(where, "is_raw = ?")
		args = append(args, boolToInt(*v))
	}
	if v := filter.MissingLocation; v != nil && *v {
		where = append(where, "gps_lat IS NOT NULL AND (location_name IS NULL OR location_name = '')")
	}
	if v := filter.SHA256; v != nil {
		where = append(where, "sha256 = ?")
		args = append(args, *v)
	}
	for _, tag := range filter.Tags {
		where = append(where, `id IN (
			SELECT pt.photo_id FROM photo_tags pt
			JOIN tags t ON t.id = pt.tag_id
			WHERE LOWER(t.name) = LOWER(?)
		)`)
		args = append(args, tag)
	}

	clause := "1 = 1"
	if len(where) > 0 {
		clause = strings.Join(where, " AND ")
	}

	var n int
	if err := tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM photos WHERE %s`, clause), args...,
	).Scan(&n); err != nil {
		return nil, 0, fmt.Errorf("count photos: %w", err)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(
		photoSelectCols+` WHERE %s ORDER BY captured_at DESC, id DESC %s`,
		clause, FormatLimitOffset(filter.Limit, filter.Offset),
	), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query photos: %w", err)
	}
	defer rows.Close()

	var photos []*photo.Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, 0, err
		}
		photos = append(photos, p)
	}
	return photos, n, rows.Err()
}

func createPhoto(ctx context.Context, tx *Tx, p *photo.Photo) error {
	p.ID = kid.New()
	p.CreatedAt = tx.now
	p.UpdatedAt = tx.now

	_, err := tx.ExecContext(ctx, `
		INSERT INTO photos (
			id, user_id, filename, stored_path, sha256, mime_type, file_size_bytes,
			camera_make, camera_model, lens_model, focal_length,
			aperture, shutter_speed, iso, gps_lat, gps_lon,
			captured_at, location_name, exif_raw, description,
			is_raw, raw_partner_id, file_type,
			visibility, share_token, thumb_path,
			created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.UserID, p.Filename, p.StoredPath, p.SHA256, p.MIMEType, p.FileSizeBytes,
		p.CameraMake, p.CameraModel, p.LensModel, p.FocalLength,
		p.Aperture, p.ShutterSpeed, nullInt(p.ISO), p.GPSLat, p.GPSLon,
		nullTimeStr(p.CapturedAt), p.LocationName, p.EXIFRaw, p.Description,
		boolToInt(p.IsRaw), p.RawPartnerID, p.FileType,
		string(p.Visibility), p.ShareToken, p.ThumbPath,
		tx.nowStr(), tx.nowStr(),
	)
	return FormatError(err)
}

func attachTags(ctx context.Context, tx *Tx, p *photo.Photo) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT t.id, t.name FROM tags t
		JOIN photo_tags pt ON pt.tag_id = t.id
		WHERE pt.photo_id = ? ORDER BY t.name`, p.ID)
	if err != nil {
		return fmt.Errorf("load tags for photo %s: %w", p.ID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var t photo.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return err
		}
		p.Tags = append(p.Tags, &t)
	}
	return rows.Err()
}

// photoSelectCols is the SELECT prefix shared by all photo queries.
const photoSelectCols = `
	SELECT id, user_id, filename, stored_path, sha256, mime_type, file_size_bytes,
	       camera_make, camera_model, lens_model, focal_length,
	       aperture, shutter_speed, iso, gps_lat, gps_lon,
	       captured_at, location_name, exif_raw, description,
	       is_raw, raw_partner_id, file_type,
	       visibility, share_token, thumb_path,
	       created_at, updated_at
	FROM photos`

type photoScanner interface {
	Scan(dest ...interface{}) error
}

func scanPhoto(s photoScanner) (*photo.Photo, error) {
	var p photo.Photo
	var capturedAt NullTime
	var createdAt, updatedAt NullTime
	var iso sql.NullInt64
	var gpsLat, gpsLon sql.NullFloat64
	var exifRaw sql.NullString
	var isRaw int
	var visibility string
	var shareToken sql.NullString
	var rawPartnerID sql.NullString
	var fileType sql.NullString
	var thumbPath sql.NullString

	err := s.Scan(
		&p.ID, &p.UserID, &p.Filename, &p.StoredPath, &p.SHA256, &p.MIMEType, &p.FileSizeBytes,
		&p.CameraMake, &p.CameraModel, &p.LensModel, &p.FocalLength,
		&p.Aperture, &p.ShutterSpeed, &iso, &gpsLat, &gpsLon,
		&capturedAt, &p.LocationName, &exifRaw, &p.Description,
		&isRaw, &rawPartnerID, &fileType,
		&visibility, &shareToken, &thumbPath,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if iso.Valid {
		p.ISO = int(iso.Int64)
	}
	if gpsLat.Valid {
		p.GPSLat = &gpsLat.Float64
	}
	if gpsLon.Valid {
		p.GPSLon = &gpsLon.Float64
	}
	t := capturedAt.Time()
	if !t.IsZero() {
		p.CapturedAt = &t
	}
	if exifRaw.Valid {
		p.EXIFRaw = exifRaw.String
	}
	p.IsRaw = isRaw == 1
	p.Visibility = photo.Visibility(visibility)
	if shareToken.Valid {
		p.ShareToken = &shareToken.String
	}
	if rawPartnerID.Valid {
		id, err := kid.FromString(rawPartnerID.String)
		if err == nil {
			p.RawPartnerID = &id
		}
	}
	if fileType.Valid {
		p.FileType = fileType.String
	}
	if thumbPath.Valid {
		p.ThumbPath = &thumbPath.String
	}
	p.CreatedAt = createdAt.Time()
	p.UpdatedAt = updatedAt.Time()
	return &p, nil
}

func nullInt(v int) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullTimeStr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

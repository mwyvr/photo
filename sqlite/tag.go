package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/kid"
)

// TagService implements photo.TagService using SQLite.
type TagService struct{ db *DB }

func NewTagService(db *DB) *TagService { return &TagService{db: db} }

func (s *TagService) FindTagByName(ctx context.Context, name string) (*photo.Tag, error) {
	name = photo.NormalizeTagName(name)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var t photo.Tag
	err = tx.QueryRowContext(ctx,
		`SELECT id, name FROM tags WHERE name = ?`, name,
	).Scan(&t.ID, &t.Name)
	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "tag not found: %q", name)
	} else if err != nil {
		return nil, fmt.Errorf("find tag by name %q: %w", name, err)
	}
	return &t, nil
}

func (s *TagService) FindOrCreateTag(ctx context.Context, name string) (*photo.Tag, error) {
	name = photo.NormalizeTagName(name)
	if name == "" {
		return nil, photo.Errorf(photo.EINVALID, "tag name cannot be empty")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t, err := findOrCreateTag(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	return t, tx.Commit()
}

func (s *TagService) FindTags(ctx context.Context, filter photo.TagFilter) ([]*photo.Tag, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	return findTags(ctx, tx, filter)
}

func (s *TagService) AttachTag(ctx context.Context, photoID kid.ID, tagID kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO photo_tags (photo_id, tag_id) VALUES (?, ?)`,
		photoID, tagID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TagService) DetachTag(ctx context.Context, photoID kid.ID, tagID kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx,
		`DELETE FROM photo_tags WHERE photo_id = ? AND tag_id = ?`, photoID, tagID,
	)
	if err != nil {
		return fmt.Errorf("detach tag: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return photo.Errorf(photo.ENOTFOUND, "tag association not found")
	}
	return tx.Commit()
}

func findOrCreateTag(ctx context.Context, tx *Tx, name string) (*photo.Tag, error) {
	var t photo.Tag
	err := tx.QueryRowContext(ctx,
		`SELECT id, name FROM tags WHERE name = ?`, name,
	).Scan(&t.ID, &t.Name)
	if err == nil {
		return &t, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("find tag %q: %w", name, err)
	}
	t.ID = kid.New()
	t.Name = name
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tags (id, name) VALUES (?, ?)`, t.ID, t.Name,
	); err != nil {
		return nil, FormatError(err)
	}
	return &t, nil
}

func findTags(ctx context.Context, tx *Tx, filter photo.TagFilter) ([]*photo.Tag, int, error) {
	where := []string{"1 = 1"}
	args := []interface{}{}
	if v := filter.NamePrefix; v != nil {
		where = append(where, "name LIKE ? || '%'")
		args = append(args, strings.ToLower(*v))
	}
	clause := strings.Join(where, " AND ")

	var n int
	if err := tx.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM tags WHERE %s`, clause), args...,
	).Scan(&n); err != nil {
		return nil, 0, fmt.Errorf("count tags: %w", err)
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, name FROM tags WHERE %s ORDER BY name %s`,
		clause, FormatLimitOffset(filter.Limit, filter.Offset),
	), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	var tags []*photo.Tag
	for rows.Next() {
		var t photo.Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, 0, err
		}
		tags = append(tags, &t)
	}
	return tags, n, rows.Err()
}

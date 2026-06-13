package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// UserService implements photo.UserService using SQLite.
type UserService struct{ db *DB }

func NewUserService(db *DB) *UserService { return &UserService{db: db} }

func (s *UserService) CreateUser(ctx context.Context, u *photo.User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	u.ID = kid.New()
	u.CreatedAt = tx.now
	u.UpdatedAt = tx.now

	_, err = tx.ExecContext(ctx, `
		INSERT INTO users (id, username, first_name, last_name, password_hash, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.FirstName, u.LastName, u.PasswordHash, boolToInt(u.IsAdmin),
		tx.nowStr(), tx.nowStr(),
	)
	if err != nil {
		return FormatError(err)
	}
	return tx.Commit()
}

func (s *UserService) FindUserByID(ctx context.Context, id kid.ID) (*photo.User, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return findUserBy(ctx, tx, "id", id.String())
}

func (s *UserService) FindUserByUsername(ctx context.Context, username string) (*photo.User, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return findUserBy(ctx, tx, "username", username)
}

func (s *UserService) CountUsers(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

func findUserBy(ctx context.Context, tx *Tx, col, val string) (*photo.User, error) {
	var u photo.User
	var isAdmin int
	var createdAt, updatedAt NullTime

	err := tx.QueryRowContext(ctx,
		`SELECT id, username, first_name, last_name, password_hash, is_admin, created_at, updated_at
		 FROM users WHERE `+col+` = ?`, val,
	).Scan(&u.ID, &u.Username, &u.FirstName, &u.LastName, &u.PasswordHash, &isAdmin, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "user not found")
	} else if err != nil {
		return nil, fmt.Errorf("find user by %s: %w", col, err)
	}
	u.IsAdmin = isAdmin != 0
	u.CreatedAt = createdAt.Time()
	u.UpdatedAt = updatedAt.Time()
	return &u, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/kid"
)

// SessionService implements photo.SessionService using SQLite.
type SessionService struct{ db *DB }

func NewSessionService(db *DB) *SessionService { return &SessionService{db: db} }

func (s *SessionService) CreateSession(ctx context.Context, sess *photo.Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	sess.ID = kid.New()
	sess.CreatedAt = tx.now

	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID,
		sess.TokenHash,
		sess.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		tx.nowStr(),
	)
	if err != nil {
		return FormatError(err)
	}
	return tx.Commit()
}

func (s *SessionService) FindSessionByTokenHash(ctx context.Context, tokenHash string) (*photo.Session, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var sess photo.Session
	var expiresAt, createdAt NullTime

	err = tx.QueryRowContext(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at
		 FROM sessions WHERE token_hash = ?`, tokenHash,
	).Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &expiresAt, &createdAt)

	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "session not found")
	} else if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}
	sess.ExpiresAt = expiresAt.Time()
	sess.CreatedAt = createdAt.Time()
	return &sess, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return photo.Errorf(photo.ENOTFOUND, "session not found")
	}
	return tx.Commit()
}

func (s *SessionService) DeleteExpiredSessions(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`, tx.nowStr(),
	); err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return tx.Commit()
}

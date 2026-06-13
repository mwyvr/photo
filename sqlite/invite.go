package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"fmt"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// InviteService implements photo.InviteService using SQLite.
type InviteService struct{ db *DB }

func NewInviteService(db *DB) *InviteService { return &InviteService{db: db} }

// generateToken returns a random URL-safe token suitable for an invite link.
// 20 bytes of entropy, base32-encoded without padding — short enough to
// type/paste but infeasible to guess.
func generateToken() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func (s *InviteService) CreateInvite(ctx context.Context, createdBy kid.ID, ttl time.Duration) (*photo.Invite, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// ExpiresAt is computed from real wall-clock time, not tx.now —
	// invite expiry is a real-world concept independent of any DB
	// timestamp override used in tests.
	now := time.Now()
	inv := &photo.Invite{
		ID:        kid.New(),
		Token:     token,
		CreatedBy: createdBy,
		CreatedAt: tx.now,
		ExpiresAt: now.Add(ttl),
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO invites (id, token, created_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		inv.ID, inv.Token, inv.CreatedBy, tx.nowStr(), inv.ExpiresAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, FormatError(err)
	}
	return inv, tx.Commit()
}

func (s *InviteService) FindInviteByToken(ctx context.Context, token string) (*photo.Invite, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return findInviteByToken(ctx, tx, token)
}

func findInviteByToken(ctx context.Context, tx *Tx, token string) (*photo.Invite, error) {
	var inv photo.Invite
	var createdAt NullTime
	var expiresAt string
	var usedAt sql.NullString
	var usedBy sql.NullString

	err := tx.QueryRowContext(ctx, `
		SELECT id, token, created_by, created_at, expires_at, used_at, used_by
		FROM invites WHERE token = ?`, token,
	).Scan(&inv.ID, &inv.Token, &inv.CreatedBy, &createdAt, &expiresAt, &usedAt, &usedBy)

	if err == sql.ErrNoRows {
		return nil, photo.Errorf(photo.ENOTFOUND, "invite not found")
	} else if err != nil {
		return nil, fmt.Errorf("find invite: %w", err)
	}

	inv.CreatedAt = createdAt.Time()
	if t, perr := time.Parse(time.RFC3339, expiresAt); perr == nil {
		inv.ExpiresAt = t
	}
	if usedAt.Valid {
		if t, perr := time.Parse(time.RFC3339, usedAt.String); perr == nil {
			inv.UsedAt = &t
		}
	}
	if usedBy.Valid {
		if id, ferr := kid.FromString(usedBy.String); ferr == nil {
			inv.UsedBy = &id
		}
	}
	return &inv, nil
}

func (s *InviteService) MarkInviteUsed(ctx context.Context, token string, usedBy kid.ID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	inv, err := findInviteByToken(ctx, tx, token)
	if err != nil {
		return err
	}
	if !inv.IsValid() {
		return photo.Errorf(photo.ECONFLICT, "invite has already been used or has expired")
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE invites SET used_at = ?, used_by = ? WHERE token = ?`,
		tx.now.UTC().Format(time.RFC3339), usedBy, token,
	)
	if err != nil {
		return fmt.Errorf("mark invite used: %w", err)
	}
	return tx.Commit()
}

func (s *InviteService) FindInvites(ctx context.Context) ([]*photo.Invite, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, token, created_by, created_at, expires_at, used_at, used_by
		FROM invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query invites: %w", err)
	}
	defer rows.Close()

	var out []*photo.Invite
	for rows.Next() {
		var inv photo.Invite
		var createdAt NullTime
		var expiresAt string
		var usedAt sql.NullString
		var usedBy sql.NullString

		if err := rows.Scan(&inv.ID, &inv.Token, &inv.CreatedBy, &createdAt, &expiresAt, &usedAt, &usedBy); err != nil {
			return nil, err
		}
		inv.CreatedAt = createdAt.Time()
		if t, perr := time.Parse(time.RFC3339, expiresAt); perr == nil {
			inv.ExpiresAt = t
		}
		if usedAt.Valid {
			if t, perr := time.Parse(time.RFC3339, usedAt.String); perr == nil {
				inv.UsedAt = &t
			}
		}
		if usedBy.Valid {
			if id, ferr := kid.FromString(usedBy.String); ferr == nil {
				inv.UsedBy = &id
			}
		}
		out = append(out, &inv)
	}
	return out, rows.Err()
}

func (s *InviteService) DeleteInvite(ctx context.Context, token string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `DELETE FROM invites WHERE token = ? AND used_at IS NULL`, token)
	if err != nil {
		return fmt.Errorf("delete invite: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return photo.Errorf(photo.ENOTFOUND, "invite not found or already used")
	}
	return tx.Commit()
}

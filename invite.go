package photo

import (
	"context"
	"time"

	"github.com/mwyvr/kid"
)

// Invite is a single-use token that permits one registration.
// Created by an admin via the CLI and consumed during /api/v1/register.
type Invite struct {
	ID        kid.ID     `json:"id"`
	Token     string     `json:"token"` // random URL-safe string, shown to admin once
	CreatedBy kid.ID     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	UsedBy    *kid.ID    `json:"usedBy,omitempty"`
}

// IsValid returns true if the invite has not been used and has not expired.
func (i *Invite) IsValid() bool {
	if i.UsedAt != nil {
		return false
	}
	return time.Now().Before(i.ExpiresAt)
}

// InviteService manages registration invites.
type InviteService interface {
	// CreateInvite generates a new single-use invite token.
	// ttl controls how long the invite remains valid before expiring unused.
	CreateInvite(ctx context.Context, createdBy kid.ID, ttl time.Duration) (*Invite, error)

	// FindInviteByToken retrieves an invite by its token string.
	// Returns ENOTFOUND if the token does not exist.
	FindInviteByToken(ctx context.Context, token string) (*Invite, error)

	// MarkInviteUsed marks an invite as consumed by the given user.
	// Returns ECONFLICT if the invite was already used.
	MarkInviteUsed(ctx context.Context, token string, usedBy kid.ID) error

	// FindInvites lists all invites, most recent first.
	FindInvites(ctx context.Context) ([]*Invite, error)

	// DeleteInvite revokes an unused invite.
	DeleteInvite(ctx context.Context, token string) error
}

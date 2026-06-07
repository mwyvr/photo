package photo

import (
	"context"
	"time"

	"github.com/mwyvr/kid"
)

// User represents a registered user of the photo library.
type User struct {
	ID           kid.ID    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"` // bcrypt hash; never serialised
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Validate returns an error if required fields are missing.
func (u *User) Validate() error {
	if u.Username == "" {
		return Errorf(EINVALID, "username is required")
	}
	if u.Email == "" {
		return Errorf(EINVALID, "email is required")
	}
	if u.PasswordHash == "" {
		return Errorf(EINVALID, "password hash is required")
	}
	return nil
}

// UserService manages user accounts.
type UserService interface {
	// CreateUser persists a new user. On success, user.ID is set.
	// Returns ECONFLICT if username or email is already taken.
	CreateUser(ctx context.Context, user *User) error

	// FindUserByID retrieves a user by ID.
	// Returns ENOTFOUND if the ID does not exist.
	FindUserByID(ctx context.Context, id kid.ID) (*User, error)

	// FindUserByUsername retrieves a user by username (case-insensitive).
	// Returns ENOTFOUND if no user with that username exists.
	FindUserByUsername(ctx context.Context, username string) (*User, error)
}

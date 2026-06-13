package photo

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/mwyvr/kid"
)

// User represents a registered user of the photo library.
// Username is the user's email address — it serves as both their login
// identifier and (potentially) a future contact point, without requiring
// a separate email field.
type User struct {
	ID           kid.ID    `json:"id"`
	Username     string    `json:"username"` // email address; used for login
	FirstName    string    `json:"firstName,omitempty"`
	LastName     string    `json:"lastName,omitempty"`
	PasswordHash string    `json:"-"` // bcrypt hash; never serialised
	IsAdmin      bool      `json:"isAdmin"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// DisplayName returns a human-friendly name for the user: "First Last" if
// either is set, otherwise falling back to the username (email).
func (u *User) DisplayName() string {
	first := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	switch {
	case first != "" && last != "":
		return first + " " + last
	case first != "":
		return first
	case last != "":
		return last
	default:
		return u.Username
	}
}

// Validate returns an error if required fields are missing or malformed.
func (u *User) Validate() error {
	if u.Username == "" {
		return Errorf(EINVALID, "username is required")
	}
	if _, err := mail.ParseAddress(u.Username); err != nil {
		return Errorf(EINVALID, "username must be a valid email address")
	}
	if u.PasswordHash == "" {
		return Errorf(EINVALID, "password hash is required")
	}
	return nil
}

// UserService manages user accounts.
type UserService interface {
	// CreateUser persists a new user. On success, user.ID is set.
	// Returns ECONFLICT if the username (email) is already taken.
	CreateUser(ctx context.Context, user *User) error

	// FindUserByID retrieves a user by ID.
	// Returns ENOTFOUND if the ID does not exist.
	FindUserByID(ctx context.Context, id kid.ID) (*User, error)

	// FindUserByUsername retrieves a user by username (case-insensitive).
	// Returns ENOTFOUND if no user with that username exists.
	FindUserByUsername(ctx context.Context, username string) (*User, error)

	// CountUsers returns the total number of registered users.
	// Used to determine whether this is the first (bootstrap) registration.
	CountUsers(ctx context.Context) (int, error)

	// FindUsers returns all registered users, ordered by creation date.
	// Used by the admin user management page.
	FindUsers(ctx context.Context) ([]*User, error)
}

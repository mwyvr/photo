package sqlite_test

import (
	"context"
	"testing"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/sqlite"
)

// makeUser creates a user with the given email (used as username) and
// returns the created photo.User. Tests share this helper to avoid boilerplate.
// The first argument is accepted for call-site compatibility with older
// tests but is unused; the second argument (email) becomes Username.
func makeUser(t *testing.T, svc *sqlite.UserService, _ string, email string) *photo.User {
	t.Helper()
	u := &photo.User{Username: email, PasswordHash: "$2a$10$placeholder"}
	if err := svc.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return u
}

func TestUserService_CreateAndFind(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	u := makeUser(t, svc, "", "testuser@example.com")

	if u.ID.IsNil() {
		t.Error("expected ID to be set after create")
	}

	found, err := svc.FindUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("find by ID: %v", err)
	}
	if found.Username != u.Username {
		t.Errorf("username = %q, want %q", found.Username, u.Username)
	}

	found2, err := svc.FindUserByUsername(ctx, "testuser@example.com")
	if err != nil {
		t.Fatalf("find by username: %v", err)
	}
	if found2.ID != u.ID {
		t.Error("found wrong user by username")
	}
}

func TestUserService_DuplicateUsername(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	makeUser(t, svc, "", "duplicate@example.com")

	u2 := &photo.User{Username: "duplicate@example.com", PasswordHash: "hash"}
	if err := svc.CreateUser(ctx, u2); photo.ErrorCode(err) != photo.ECONFLICT {
		t.Errorf("expected ECONFLICT, got %q", photo.ErrorCode(err))
	}
}

func TestUserService_InvalidUsername(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	u := &photo.User{Username: "not-an-email", PasswordHash: "hash"}
	if err := svc.CreateUser(ctx, u); photo.ErrorCode(err) != photo.EINVALID {
		t.Errorf("expected EINVALID for non-email username, got %q", photo.ErrorCode(err))
	}
}

func TestUserService_NotFound(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	id := kid.New() // valid ID that doesn't exist in the DB
	_, err := svc.FindUserByID(ctx, id)
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND, got %q", photo.ErrorCode(err))
	}
}

func TestUserService_CountUsers(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	n, err := svc.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count users (empty): %v", err)
	}
	if n != 0 {
		t.Errorf("count = %d, want 0", n)
	}

	makeUser(t, svc, "", "alice@example.com")
	makeUser(t, svc, "", "bob@example.com")

	n, err = svc.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}

func TestUserService_IsAdmin(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	u := &photo.User{Username: "admin@example.com", PasswordHash: "hash", IsAdmin: true}
	if err := svc.CreateUser(ctx, u); err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	found, err := svc.FindUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !found.IsAdmin {
		t.Error("expected IsAdmin = true")
	}
}

func TestUserService_DisplayName(t *testing.T) {
	tests := []struct {
		name      string
		username  string
		firstName string
		lastName  string
		want      string
	}{
		{"both names", "v@example.com", "Vincent", "Smith", "Vincent Smith"},
		{"first only", "v@example.com", "Vincent", "", "Vincent"},
		{"last only", "v@example.com", "", "Smith", "Smith"},
		{"no names falls back to username", "v@example.com", "", "", "v@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &photo.User{Username: tt.username, FirstName: tt.firstName, LastName: tt.lastName}
			if got := u.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

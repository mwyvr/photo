package sqlite_test

import (
	"context"
	"testing"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/sqlite"
)

func makeUser(t *testing.T, svc *sqlite.UserService, username, email string) *photo.User {
	t.Helper()
	u := &photo.User{Username: username, Email: email, PasswordHash: "$2a$10$placeholder"}
	if err := svc.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}
	return u
}

func TestUserService_CreateAndFind(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	u := makeUser(t, svc, "testuser", "test@example.com")

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

	found2, err := svc.FindUserByUsername(ctx, "testuser")
	if err != nil {
		t.Fatalf("find by username: %v", err)
	}
	if found2.ID != u.ID {
		t.Error("found wrong user by username")
	}
}

func TestUserService_CaseInsensitiveUsername(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)

	makeUser(t, svc, "Mike", "mike@example.com")

	_, err := svc.FindUserByUsername(context.Background(), "mike")
	if err != nil {
		t.Errorf("case-insensitive lookup failed: %v", err)
	}
}

func TestUserService_DuplicateUsername(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewUserService(db)
	ctx := context.Background()

	makeUser(t, svc, "duplicate", "a@example.com")

	u2 := &photo.User{Username: "duplicate", Email: "b@example.com", PasswordHash: "hash"}
	if err := svc.CreateUser(ctx, u2); photo.ErrorCode(err) != photo.ECONFLICT {
		t.Errorf("expected ECONFLICT, got %q", photo.ErrorCode(err))
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

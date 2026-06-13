package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/sqlite"
)

func TestInviteService_CreateAndFind(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	inviteSvc := sqlite.NewInviteService(db)
	ctx := context.Background()

	admin := makeUser(t, userSvc, "admin", "admin@example.com")

	inv, err := inviteSvc.CreateInvite(ctx, admin.ID, 24*time.Hour)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if inv.Token == "" {
		t.Error("expected non-empty token")
	}
	if !inv.IsValid() {
		t.Error("new invite should be valid")
	}

	found, err := inviteSvc.FindInviteByToken(ctx, inv.Token)
	if err != nil {
		t.Fatalf("find invite: %v", err)
	}
	if found.ID != inv.ID {
		t.Error("found wrong invite")
	}
}

func TestInviteService_MarkUsed(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	inviteSvc := sqlite.NewInviteService(db)
	ctx := context.Background()

	admin := makeUser(t, userSvc, "admin", "admin@example.com")
	bob := makeUser(t, userSvc, "bob", "bob@example.com")

	inv, _ := inviteSvc.CreateInvite(ctx, admin.ID, 24*time.Hour)

	if err := inviteSvc.MarkInviteUsed(ctx, inv.Token, bob.ID); err != nil {
		t.Fatalf("mark used: %v", err)
	}

	found, _ := inviteSvc.FindInviteByToken(ctx, inv.Token)
	if found.IsValid() {
		t.Error("used invite should not be valid")
	}
	if found.UsedAt == nil {
		t.Error("expected UsedAt to be set")
	}
	if found.UsedBy == nil || *found.UsedBy != bob.ID {
		t.Error("expected UsedBy = bob")
	}

	// Reuse should fail.
	if err := inviteSvc.MarkInviteUsed(ctx, inv.Token, bob.ID); photo.ErrorCode(err) != photo.ECONFLICT {
		t.Errorf("reuse: expected ECONFLICT, got %q", photo.ErrorCode(err))
	}
}

func TestInviteService_Expired(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	inviteSvc := sqlite.NewInviteService(db)
	ctx := context.Background()

	admin := makeUser(t, userSvc, "admin", "admin@example.com")

	// Create an invite that's already expired.
	inv, _ := inviteSvc.CreateInvite(ctx, admin.ID, -1*time.Hour)

	if inv.IsValid() {
		t.Error("expired invite should not be valid")
	}

	bob := makeUser(t, userSvc, "bob", "bob@example.com")
	if err := inviteSvc.MarkInviteUsed(ctx, inv.Token, bob.ID); photo.ErrorCode(err) != photo.ECONFLICT {
		t.Errorf("use expired invite: expected ECONFLICT, got %q", photo.ErrorCode(err))
	}
}

func TestInviteService_FindInvites(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	inviteSvc := sqlite.NewInviteService(db)
	ctx := context.Background()

	admin := makeUser(t, userSvc, "admin", "admin@example.com")
	inviteSvc.CreateInvite(ctx, admin.ID, 24*time.Hour) //nolint
	inviteSvc.CreateInvite(ctx, admin.ID, 24*time.Hour) //nolint

	invites, err := inviteSvc.FindInvites(ctx)
	if err != nil {
		t.Fatalf("find invites: %v", err)
	}
	if len(invites) != 2 {
		t.Errorf("len = %d, want 2", len(invites))
	}
}

func TestInviteService_DeleteInvite(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	inviteSvc := sqlite.NewInviteService(db)
	ctx := context.Background()

	admin := makeUser(t, userSvc, "admin", "admin@example.com")
	inv, _ := inviteSvc.CreateInvite(ctx, admin.ID, 24*time.Hour)

	if err := inviteSvc.DeleteInvite(ctx, inv.Token); err != nil {
		t.Fatalf("delete invite: %v", err)
	}

	_, err := inviteSvc.FindInviteByToken(ctx, inv.Token)
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND after delete, got %q", photo.ErrorCode(err))
	}
}

func TestInviteService_DeleteUsedInviteFails(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	inviteSvc := sqlite.NewInviteService(db)
	ctx := context.Background()

	admin := makeUser(t, userSvc, "admin", "admin@example.com")
	bob := makeUser(t, userSvc, "bob", "bob@example.com")
	inv, _ := inviteSvc.CreateInvite(ctx, admin.ID, 24*time.Hour)
	inviteSvc.MarkInviteUsed(ctx, inv.Token, bob.ID) //nolint

	if err := inviteSvc.DeleteInvite(ctx, inv.Token); photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("delete used invite: expected ENOTFOUND, got %q", photo.ErrorCode(err))
	}
}

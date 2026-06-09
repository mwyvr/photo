package sqlite_test

import (
	"context"
	"testing"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/sqlite"
)

func TestTagService_FindOrCreate(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewTagService(db)
	ctx := context.Background()

	t1, err := svc.FindOrCreateTag(ctx, "travel")
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if t1.Name != "travel" {
		t.Errorf("name = %q, want travel", t1.Name)
	}
	if t1.ID.IsNil() {
		t.Error("expected ID to be set")
	}

	// Second call returns same tag.
	t2, err := svc.FindOrCreateTag(ctx, "travel")
	if err != nil {
		t.Fatalf("find existing tag: %v", err)
	}
	if t2.ID != t1.ID {
		t.Error("expected same ID on second call")
	}
}

func TestTagService_Normalises(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewTagService(db)
	ctx := context.Background()

	t1, _ := svc.FindOrCreateTag(ctx, "Travel")
	t2, _ := svc.FindOrCreateTag(ctx, "TRAVEL")
	if t1.ID != t2.ID {
		t.Error("expected Travel and TRAVEL to resolve to the same tag")
	}
}

func TestTagService_FindByName(t *testing.T) {
	db := newTestDB(t)
	svc := sqlite.NewTagService(db)
	ctx := context.Background()

	svc.FindOrCreateTag(ctx, "france") //nolint

	found, err := svc.FindTagByName(ctx, "france")
	if err != nil {
		t.Fatalf("find by name: %v", err)
	}
	if found.Name != "france" {
		t.Errorf("name = %q, want france", found.Name)
	}

	_, err = svc.FindTagByName(ctx, "nonexistent")
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND, got %q", photo.ErrorCode(err))
	}
}

func TestTagService_AttachDetach(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	tagSvc := sqlite.NewTagService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	p := makePhoto(t, photoSvc, u.ID, "IMG_001.jpg", "hash1")
	tag, _ := tagSvc.FindOrCreateTag(ctx, "travel")

	if err := tagSvc.AttachTag(ctx, p.ID, tag.ID); err != nil {
		t.Fatalf("attach tag: %v", err)
	}

	// Photo should now have the tag.
	found, _ := photoSvc.FindPhotoByID(ctx, p.ID)
	if len(found.Tags) != 1 || found.Tags[0].Name != "travel" {
		t.Errorf("expected photo to have tag 'travel', got %v", found.Tags)
	}

	// Attaching again is a no-op.
	if err := tagSvc.AttachTag(ctx, p.ID, tag.ID); err != nil {
		t.Errorf("duplicate attach should be no-op, got: %v", err)
	}

	// Detach.
	if err := tagSvc.DetachTag(ctx, p.ID, tag.ID); err != nil {
		t.Fatalf("detach tag: %v", err)
	}

	found2, _ := photoSvc.FindPhotoByID(ctx, p.ID)
	if len(found2.Tags) != 0 {
		t.Errorf("expected no tags after detach, got %v", found2.Tags)
	}
}

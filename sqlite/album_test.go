package sqlite_test

import (
	"context"
	"testing"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/sqlite"
)

func TestAlbumService_CreateAndFind(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	albumSvc := sqlite.NewAlbumService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")

	a := &photo.Album{UserID: u.ID, Name: "France 2024", Description: "Summer trip"}
	if err := albumSvc.CreateAlbum(ctx, a); err != nil {
		t.Fatalf("create album: %v", err)
	}
	if a.ID.IsNil() {
		t.Error("expected ID to be set")
	}

	found, err := albumSvc.FindAlbumByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("find album: %v", err)
	}
	if found.Name != "France 2024" {
		t.Errorf("name = %q, want France 2024", found.Name)
	}
}

func TestAlbumService_AddRemovePhotos(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	albumSvc := sqlite.NewAlbumService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	a := &photo.Album{UserID: u.ID, Name: "Test Album"}
	albumSvc.CreateAlbum(ctx, a) //nolint

	p1 := makePhoto(t, photoSvc, u.ID, "p1.jpg", "h1")
	p2 := makePhoto(t, photoSvc, u.ID, "p2.jpg", "h2")
	p3 := makePhoto(t, photoSvc, u.ID, "p3.jpg", "h3")

	for _, p := range []*photo.Photo{p1, p2, p3} {
		if err := albumSvc.AddPhoto(ctx, a.ID, p.ID); err != nil {
			t.Fatalf("add photo %s: %v", p.Filename, err)
		}
	}

	photos, total, err := albumSvc.FindAlbumPhotos(ctx, a.ID, 0, 50)
	if err != nil {
		t.Fatalf("find album photos: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(photos) != 3 {
		t.Errorf("len = %d, want 3", len(photos))
	}

	// Remove one.
	if err := albumSvc.RemovePhoto(ctx, a.ID, p2.ID); err != nil {
		t.Fatalf("remove photo: %v", err)
	}

	_, total2, _ := albumSvc.FindAlbumPhotos(ctx, a.ID, 0, 50)
	if total2 != 2 {
		t.Errorf("total after remove = %d, want 2", total2)
	}
}

func TestAlbumService_PhotoCount(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	albumSvc := sqlite.NewAlbumService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	a := &photo.Album{UserID: u.ID, Name: "Test"}
	albumSvc.CreateAlbum(ctx, a) //nolint

	p := makePhoto(t, photoSvc, u.ID, "p.jpg", "h")
	albumSvc.AddPhoto(ctx, a.ID, p.ID) //nolint

	found, _ := albumSvc.FindAlbumByID(ctx, a.ID)
	if found.PhotoCount != 1 {
		t.Errorf("photo count = %d, want 1", found.PhotoCount)
	}
}

func TestAlbumService_Delete(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	albumSvc := sqlite.NewAlbumService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	a := &photo.Album{UserID: u.ID, Name: "To Delete"}
	albumSvc.CreateAlbum(ctx, a) //nolint

	if err := albumSvc.DeleteAlbum(ctx, a.ID); err != nil {
		t.Fatalf("delete album: %v", err)
	}

	_, err := albumSvc.FindAlbumByID(ctx, a.ID)
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND after delete, got %q", photo.ErrorCode(err))
	}
}

package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/sqlite"
)

func makePhoto(t *testing.T, svc *sqlite.PhotoService, userID kid.ID, filename, sha256 string) *photo.Photo {
	t.Helper()
	p := &photo.Photo{
		UserID:        userID,
		Filename:      filename,
		StoredPath:    "2024/01/" + filename,
		SHA256:        sha256,
		MIMEType:      "image/jpeg",
		FileType:      "JPEG",
		FileSizeBytes: 1024,
		Visibility:    photo.VisibilityHousehold,
	}
	if err := svc.CreatePhoto(context.Background(), p); err != nil {
		t.Fatalf("create photo %q: %v", filename, err)
	}
	return p
}

func TestPhotoService_CreateAndFind(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	p := makePhoto(t, photoSvc, u.ID, "IMG_001.jpg", "abc123")

	if p.ID.IsNil() {
		t.Error("expected photo ID to be set")
	}

	found, err := photoSvc.FindPhotoByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("find by ID: %v", err)
	}
	if found.Filename != p.Filename {
		t.Errorf("filename = %q, want %q", found.Filename, p.Filename)
	}
	if found.SHA256 != p.SHA256 {
		t.Errorf("sha256 = %q, want %q", found.SHA256, p.SHA256)
	}
}

func TestPhotoService_DuplicateSHA256(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	makePhoto(t, photoSvc, u.ID, "IMG_001.jpg", "samehash")

	p2 := &photo.Photo{
		UserID: u.ID, Filename: "IMG_002.jpg",
		StoredPath: "2024/01/IMG_002.jpg", SHA256: "samehash",
		MIMEType: "image/jpeg", FileType: "JPEG", FileSizeBytes: 512,
	}
	if err := photoSvc.CreatePhoto(ctx, p2); photo.ErrorCode(err) != photo.ECONFLICT {
		t.Errorf("expected ECONFLICT for duplicate SHA256, got %q", photo.ErrorCode(err))
	}
}

func TestPhotoService_FindPhotos_FilterByUser(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	alice := makeUser(t, userSvc, "alice", "alice@example.com")
	bob := makeUser(t, userSvc, "bob", "bob@example.com")

	makePhoto(t, photoSvc, alice.ID, "alice1.jpg", "hash-a1")
	makePhoto(t, photoSvc, alice.ID, "alice2.jpg", "hash-a2")
	makePhoto(t, photoSvc, bob.ID, "bob1.jpg", "hash-b1")

	photos, total, err := photoSvc.FindPhotos(ctx, photo.PhotoFilter{UserID: alice.ID})
	if err != nil {
		t.Fatalf("find photos: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(photos) != 2 {
		t.Errorf("len = %d, want 2", len(photos))
	}
}

func TestPhotoService_FindPhotos_FilterBySHA256(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	makePhoto(t, photoSvc, u.ID, "IMG_001.jpg", "uniquehash")
	makePhoto(t, photoSvc, u.ID, "IMG_002.jpg", "otherhash")

	sha := "uniquehash"
	photos, total, err := photoSvc.FindPhotos(ctx, photo.PhotoFilter{SHA256: &sha})
	if err != nil {
		t.Fatalf("find by sha256: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if photos[0].SHA256 != sha {
		t.Errorf("wrong photo returned")
	}
}

func TestPhotoService_FindPhotos_FilterByDateRange(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")

	early := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	p1 := makePhoto(t, photoSvc, u.ID, "old.jpg", "hash-old")
	photoSvc.UpdatePhoto(ctx, p1.ID, photo.PhotoUpdate{}) //nolint — just to exercise path

	// Manually set captured_at by creating with it set.
	p2 := &photo.Photo{
		UserID: u.ID, Filename: "early.jpg", StoredPath: "2023/01/early.jpg",
		SHA256: "hash-early", MIMEType: "image/jpeg", FileType: "JPEG",
		FileSizeBytes: 512, CapturedAt: &early,
	}
	photoSvc.CreatePhoto(ctx, p2) //nolint

	p3 := &photo.Photo{
		UserID: u.ID, Filename: "late.jpg", StoredPath: "2024/06/late.jpg",
		SHA256: "hash-late", MIMEType: "image/jpeg", FileType: "JPEG",
		FileSizeBytes: 512, CapturedAt: &late,
	}
	photoSvc.CreatePhoto(ctx, p3) //nolint

	cutoff := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	_, total, err := photoSvc.FindPhotos(ctx, photo.PhotoFilter{
		UserID:        u.ID,
		CapturedAfter: &cutoff,
	})
	if err != nil {
		t.Fatalf("find with date filter: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 (only the late photo)", total)
	}
}

func TestPhotoService_UpdatePhoto(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	p := makePhoto(t, photoSvc, u.ID, "IMG_001.jpg", "hash1")

	desc := "A lovely sunset"
	loc := "Tokyo, Japan"
	vis := photo.VisibilityPublished

	updated, err := photoSvc.UpdatePhoto(ctx, p.ID, photo.PhotoUpdate{
		Description:  &desc,
		LocationName: &loc,
		Visibility:   &vis,
	})
	if err != nil {
		t.Fatalf("update photo: %v", err)
	}
	if updated.Description != desc {
		t.Errorf("description = %q, want %q", updated.Description, desc)
	}
	if updated.LocationName != loc {
		t.Errorf("location = %q, want %q", updated.LocationName, loc)
	}
	if updated.Visibility != photo.VisibilityPublished {
		t.Errorf("visibility = %q, want %q", updated.Visibility, photo.VisibilityPublished)
	}
}

func TestPhotoService_DeletePhoto(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	u := makeUser(t, userSvc, "alice", "alice@example.com")
	p := makePhoto(t, photoSvc, u.ID, "IMG_001.jpg", "hash1")

	if err := photoSvc.DeletePhoto(ctx, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := photoSvc.FindPhotoByID(ctx, p.ID)
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND after delete, got %q", photo.ErrorCode(err))
	}
}

func TestPhotoService_NotFound(t *testing.T) {
	db := newTestDB(t)
	photoSvc := sqlite.NewPhotoService(db)

	_, err := photoSvc.FindPhotoByID(context.Background(), kid.New())
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND, got %q", photo.ErrorCode(err))
	}
}

func TestPhotoService_FindPhotos_HouseholdVisibility(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	alice := makeUser(t, userSvc, "alice", "alice@example.com")
	bob := makeUser(t, userSvc, "bob", "bob@example.com")

	priv := photo.VisibilityPrivate
	hh := photo.VisibilityHousehold

	// Alice's private photo — only she should see it.
	alicePrivate := makePhoto(t, photoSvc, alice.ID, "alice-private.jpg", "hash-a1")
	if _, err := photoSvc.UpdatePhoto(ctx, alicePrivate.ID, photo.PhotoUpdate{Visibility: &priv}); err != nil {
		t.Fatalf("set private: %v", err)
	}

	// Alice's household photo — any authenticated user should see it.
	aliceHousehold := makePhoto(t, photoSvc, alice.ID, "alice-household.jpg", "hash-a2")
	if _, err := photoSvc.UpdatePhoto(ctx, aliceHousehold.ID, photo.PhotoUpdate{Visibility: &hh}); err != nil {
		t.Fatalf("set household: %v", err)
	}

	// Bob's private photo — only he should see it.
	bobPrivate := makePhoto(t, photoSvc, bob.ID, "bob-private.jpg", "hash-b1")
	if _, err := photoSvc.UpdatePhoto(ctx, bobPrivate.ID, photo.PhotoUpdate{Visibility: &priv}); err != nil {
		t.Fatalf("set private: %v", err)
	}

	// Bob's household photo — any authenticated user should see it.
	bobHousehold := makePhoto(t, photoSvc, bob.ID, "bob-household.jpg", "hash-b2")
	if _, err := photoSvc.UpdatePhoto(ctx, bobHousehold.ID, photo.PhotoUpdate{Visibility: &hh}); err != nil {
		t.Fatalf("set household: %v", err)
	}

	// 1. Alice's own view (no ViewerID) — sees only her own photos.
	photos, total, err := photoSvc.FindPhotos(ctx, photo.PhotoFilter{UserID: alice.ID})
	if err != nil {
		t.Fatalf("find photos (own): %v", err)
	}
	if total != 2 {
		t.Errorf("own: total = %d, want 2", total)
	}

	// 2. Alice as viewer in household mode — sees her own (private+household) + bob's household.
	photos, total, err = photoSvc.FindPhotos(ctx, photo.PhotoFilter{
		UserID:        alice.ID,
		ViewerID:      alice.ID,
		HouseholdMode: true,
	})
	if err != nil {
		t.Fatalf("find photos (household): %v", err)
	}
	if total != 3 {
		t.Errorf("household: total = %d, want 3 (alice×2 + bob's household)", total)
	}
	ids := make(map[kid.ID]bool)
	for _, p := range photos {
		ids[p.ID] = true
	}
	if !ids[alicePrivate.ID] {
		t.Error("expected alice's private photo (own) to be included")
	}
	if !ids[aliceHousehold.ID] {
		t.Error("expected alice's household photo to be included")
	}
	if !ids[bobHousehold.ID] {
		t.Error("expected bob's household photo to be included")
	}
	if ids[bobPrivate.ID] {
		t.Error("did not expect bob's private photo to be included")
	}

	// 3. Unauthenticated (no UserID, no ViewerID) — only published photos.
	photos, total, err = photoSvc.FindPhotos(ctx, photo.PhotoFilter{})
	if err != nil {
		t.Fatalf("find photos (unauthed): %v", err)
	}
	if total != 0 {
		t.Errorf("unauthed: total = %d, want 0 (no published photos)", total)
	}
	_ = photos
	_ = bobPrivate
}

// makePhotoWithGPS creates a photo with GPS coordinates set.
func makePhotoWithGPS(t *testing.T, svc *sqlite.PhotoService, userID kid.ID, filename, sha256 string, lat, lon float64) *photo.Photo {
	t.Helper()
	p := &photo.Photo{
		UserID:        userID,
		Filename:      filename,
		StoredPath:    "2024/01/" + filename,
		SHA256:        sha256,
		MIMEType:      "image/jpeg",
		FileType:      "JPEG",
		FileSizeBytes: 1024,
		Visibility:    photo.VisibilityHousehold,
		GPSLat:        &lat,
		GPSLon:        &lon,
	}
	if err := svc.CreatePhoto(context.Background(), p); err != nil {
		t.Fatalf("create photo %q: %v", filename, err)
	}
	return p
}

func TestPhotoService_FindPhotos_MissingLocation(t *testing.T) {
	db := newTestDB(t)
	userSvc := sqlite.NewUserService(db)
	photoSvc := sqlite.NewPhotoService(db)
	ctx := context.Background()

	alice := makeUser(t, userSvc, "alice", "alice@example.com")

	lat, lon := 49.05, -120.78

	// Photo with GPS but no location name — should match.
	withGPSNoLoc := makePhotoWithGPS(t, photoSvc, alice.ID, "gps-no-location.jpg", "hash-1", lat, lon)

	// Photo with GPS and location name — should not match.
	withGPSAndLoc := makePhotoWithGPS(t, photoSvc, alice.ID, "gps-with-location.jpg", "hash-2", lat, lon)
	loc := "Penticton, Canada"
	if _, err := photoSvc.UpdatePhoto(ctx, withGPSAndLoc.ID, photo.PhotoUpdate{LocationName: &loc}); err != nil {
		t.Fatalf("set location: %v", err)
	}

	// Photo with no GPS at all — should not match.
	makePhoto(t, photoSvc, alice.ID, "no-gps.jpg", "hash-3")

	missing := true
	photos, total, err := photoSvc.FindPhotos(ctx, photo.PhotoFilter{
		UserID:          alice.ID,
		MissingLocation: &missing,
	})
	if err != nil {
		t.Fatalf("find photos (missing location): %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(photos) != 1 || photos[0].ID != withGPSNoLoc.ID {
		t.Error("expected only the GPS-without-location photo")
	}
}

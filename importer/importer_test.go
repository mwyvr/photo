package importer_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/importer"
	"github.com/mwyvr/photo/mock"
)

func makeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("create test file: %v", err)
	}
	return path
}

func TestImportFile_Success(t *testing.T) {
	ctx := context.Background()
	libraryRoot := t.TempDir()
	userID := kid.New()

	capturedAt := time.Date(2023, 6, 15, 10, 30, 0, 0, time.UTC)
	exifMock := &mock.EXIFExtractor{
		ExtractFn: func(_ context.Context, _ string) (*photo.EXIFData, error) {
			return &photo.EXIFData{
				FileType:   "JPEG",
				MIMEType:   "image/jpeg",
				Make:       "Canon",
				Model:      "EOS R5",
				CapturedAt: &capturedAt,
			}, nil
		},
	}

	photoSvc := mock.NewPhotoService()
	imp := importer.New(photoSvc, exifMock, nil, libraryRoot)

	srcPath := makeTestFile(t, "IMG_0001.jpg", "fake jpeg content")
	result := imp.ImportFile(ctx, srcPath, photo.ImportOptions{UserID: userID})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Skipped {
		t.Fatalf("expected import, got skip: %s", result.SkipReason)
	}
	if result.Photo == nil {
		t.Fatal("expected Photo to be set")
	}
	if result.Photo.CameraModel != "EOS R5" {
		t.Errorf("CameraModel = %q, want %q", result.Photo.CameraModel, "EOS R5")
	}

	// Path format is YYYY/MM/<kid-id>.ext
	parts := strings.Split(result.Photo.StoredPath, string(filepath.Separator))
	if len(parts) != 3 {
		t.Fatalf("StoredPath %q: expected 3 parts (YYYY/MM/id.ext), got %d", result.Photo.StoredPath, len(parts))
	}
	if parts[0] != "2023" {
		t.Errorf("year = %q, want 2023", parts[0])
	}
	if parts[1] != "06" {
		t.Errorf("month = %q, want 06", parts[1])
	}
	if !strings.HasSuffix(parts[2], ".jpg") {
		t.Errorf("filename %q should end with .jpg", parts[2])
	}

	// File must physically exist.
	fullDest := filepath.Join(libraryRoot, result.Photo.StoredPath)
	if _, err := os.Stat(fullDest); err != nil {
		t.Errorf("copied file not found at %q: %v", fullDest, err)
	}
}

func TestImportFile_UnsupportedExtension(t *testing.T) {
	// Simulate what exiftool returns for a PDF: a non-image MIME type.
	exifMock := &mock.EXIFExtractor{
		ExtractFn: func(_ context.Context, _ string) (*photo.EXIFData, error) {
			return &photo.EXIFData{FileType: "PDF", MIMEType: "application/pdf"}, nil
		},
	}
	imp := importer.New(mock.NewPhotoService(), exifMock, nil, t.TempDir())
	srcPath := makeTestFile(t, "document.pdf", "not an image")
	result := imp.ImportFile(context.Background(), srcPath, photo.ImportOptions{UserID: kid.New()})

	if !result.Skipped {
		t.Fatal("expected file to be skipped")
	}
	if result.Err != nil {
		t.Fatalf("unexpected error on skip: %v", result.Err)
	}
}

func TestImportFile_Duplicate(t *testing.T) {
	photoSvc := mock.NewPhotoService()
	photoSvc.CreatePhotoFn = func(_ context.Context, _ *photo.Photo) error {
		return photo.Errorf(photo.ECONFLICT, "duplicate sha256")
	}

	imp := importer.New(photoSvc, &mock.EXIFExtractor{}, nil, t.TempDir())
	srcPath := makeTestFile(t, "IMG_0001.jpg", "fake jpeg")
	result := imp.ImportFile(context.Background(), srcPath, photo.ImportOptions{UserID: kid.New()})

	if !result.Skipped {
		t.Fatal("expected duplicate to be skipped")
	}
	if result.SkipReason == "" {
		t.Error("expected a skip reason")
	}
}

func TestImportFile_DryRun(t *testing.T) {
	libraryRoot := t.TempDir()
	photoSvc := mock.NewPhotoService()
	imp := importer.New(photoSvc, &mock.EXIFExtractor{}, nil, libraryRoot)

	srcPath := makeTestFile(t, "IMG_0002.jpg", "fake jpeg")
	result := imp.ImportFile(context.Background(), srcPath, photo.ImportOptions{
		DryRun: true,
		UserID: kid.New(),
	})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Skipped {
		t.Fatalf("dry run should not skip: %s", result.SkipReason)
	}
	if n := len(photoSvc.Photos()); n != 0 {
		t.Errorf("expected 0 photos in store after dry run, got %d", n)
	}
	entries, _ := os.ReadDir(libraryRoot)
	if len(entries) != 0 {
		t.Errorf("expected empty library root after dry run, got %d entries", len(entries))
	}
}

func TestImportDir_MultipleFiles(t *testing.T) {
	ctx := context.Background()
	libraryRoot := t.TempDir()
	srcDir := t.TempDir()

	files := []struct{ name string }{
		{"photo1.jpg"},
		{"photo2.cr2"},
		{"readme.txt"}, // unsupported
		{"photo3.heic"},
	}
	for _, f := range files {
		os.WriteFile(filepath.Join(srcDir, f.name), []byte("content-"+f.name), 0o644)
	}

	imp := importer.New(mock.NewPhotoService(), &mock.EXIFExtractor{}, nil, libraryRoot)
	opts := photo.ImportOptions{UserID: kid.New()}

	results, err := importer.ImportDir(ctx, imp, srcDir, opts, nil)
	if err != nil {
		t.Fatalf("ImportDir error: %v", err)
	}

	var added, skipped int
	for _, r := range results {
		if r.Skipped {
			skipped++
		} else if r.Err == nil {
			added++
		}
	}
	if added != 3 {
		t.Errorf("expected 3 successful imports, got %d", added)
	}
	if skipped != 1 {
		t.Errorf("expected 1 skip (readme.txt), got %d", skipped)
	}
}

func TestImportReader_Success(t *testing.T) {
	ctx := context.Background()
	libraryRoot := t.TempDir()
	userID := kid.New()

	capturedAt := time.Date(2024, 3, 10, 9, 0, 0, 0, time.UTC)
	exifMock := &mock.EXIFExtractor{
		ExtractReaderFn: func(_ context.Context, _ io.Reader, _ string) (*photo.EXIFData, error) {
			return &photo.EXIFData{
				FileType:   "NEF",
				MIMEType:   "image/x-nikon-nef",
				IsRaw:      true,
				Model:      "Nikon Z9",
				CapturedAt: &capturedAt,
			}, nil
		},
	}

	photoSvc := mock.NewPhotoService()
	imp := importer.New(photoSvc, exifMock, nil, libraryRoot)

	data := []byte("fake raw nef content")
	result := imp.ImportReader(ctx, bytes.NewReader(data), "DSC_0001.nef", photo.ImportOptions{UserID: userID})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Skipped {
		t.Fatalf("expected import, got skip: %s", result.SkipReason)
	}
	if result.Photo == nil {
		t.Fatal("expected Photo to be set")
	}
	if result.Photo.CameraModel != "Nikon Z9" {
		t.Errorf("CameraModel = %q, want %q", result.Photo.CameraModel, "Nikon Z9")
	}

	parts := strings.Split(result.Photo.StoredPath, string(filepath.Separator))
	if len(parts) != 3 {
		t.Fatalf("StoredPath %q: expected YYYY/MM/id.ext, got %d parts", result.Photo.StoredPath, len(parts))
	}
	if parts[0] != "2024" {
		t.Errorf("year = %q, want 2024", parts[0])
	}
	if !strings.HasSuffix(parts[2], ".nef") {
		t.Errorf("filename %q should end with .nef", parts[2])
	}
}

func TestImportFile_RawOnly(t *testing.T) {
	jpegMock := &mock.EXIFExtractor{
		ExtractFn: func(_ context.Context, _ string) (*photo.EXIFData, error) {
			return &photo.EXIFData{FileType: "JPEG", MIMEType: "image/jpeg", IsRaw: false}, nil
		},
	}
	imp := importer.New(mock.NewPhotoService(), jpegMock, nil, t.TempDir())
	srcPath := makeTestFile(t, "IMG_0001.jpg", "fake jpeg")
	result := imp.ImportFile(context.Background(), srcPath, photo.ImportOptions{
		UserID:  kid.New(),
		RawOnly: true,
	})
	if !result.Skipped {
		t.Fatal("expected JPEG to be skipped when RawOnly is set")
	}
}

	exifMock := &mock.EXIFExtractor{
		ExtractReaderFn: func(_ context.Context, _ io.Reader, _ string) (*photo.EXIFData, error) {
			return &photo.EXIFData{FileType: "PDF", MIMEType: "application/pdf"}, nil
		},
	}
	imp := importer.New(mock.NewPhotoService(), exifMock, nil, t.TempDir())
	result := imp.ImportReader(context.Background(), bytes.NewReader([]byte("data")), "file.pdf", photo.ImportOptions{UserID: kid.New()})
	if !result.Skipped {
		t.Fatal("expected non-image file to be skipped")
	}
}

package photo

import (
	"context"
	"io"
	"time"
)

// EXIFData holds metadata extracted from a photo file by EXIFExtractor.
type EXIFData struct {
	Make         string
	Model        string
	LensModel    string
	FocalLength  string
	Aperture     string
	ShutterSpeed string
	ISO          int
	GPSLat       *float64
	GPSLon       *float64
	CapturedAt   *time.Time
	// Raw holds the complete exiftool JSON output for the blob column.
	Raw string
}

// EXIFExtractor extracts EXIF metadata from a file or reader.
// The implementation lives in exif/ and shells out to exiftool.
type EXIFExtractor interface {
	// Extract reads EXIF from the file at path.
	Extract(ctx context.Context, path string) (*EXIFData, error)

	// ExtractReader reads EXIF from r, using filename as a format hint.
	ExtractReader(ctx context.Context, r io.Reader, filename string) (*EXIFData, error)
}

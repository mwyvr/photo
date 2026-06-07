// Package exif implements photo.EXIFExtractor by shelling out to the
// exiftool command-line program. exiftool is the only tool with reliable
// support for RAW formats (CR2, NEF, ARW, etc.) and HEIC across both
// Linux and macOS.
//
// exiftool must be installed and on PATH. Call CheckDependency() at startup
// to give the user a clear error message rather than a cryptic exec failure.
package exif

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mwyvr/photo"
)

// rawFileTypes is the set of exiftool FileType values that represent camera
// RAW formats. These are matched case-insensitively against the FileType tag.
// Source: https://exiftool.org/supported_extensions.html
var rawFileTypes = map[string]struct{}{
	"3FR": {}, // Hasselblad
	"ARW": {}, // Sony
	"CR2": {}, // Canon
	"CR3": {}, // Canon
	"CRW": {}, // Canon (older)
	"DCR": {}, // Kodak
	"DNG": {}, // Adobe / Leica / various
	"ERF": {}, // Epson
	"FFF": {}, // Hasselblad
	"GPR": {}, // GoPro
	"IIQ": {}, // Phase One
	"K25": {}, // Kodak
	"KDC": {}, // Kodak
	"MEF": {}, // Mamiya
	"MOS": {}, // Leaf
	"MRW": {}, // Minolta
	"NEF": {}, // Nikon
	"NRW": {}, // Nikon (compact)
	"ORF": {}, // Olympus
	"PEF": {}, // Pentax
	"PTX": {}, // Pentax
	"RAF": {}, // Fujifilm
	"RAW": {}, // Leica / Panasonic
	"RW2": {}, // Panasonic
	"RWL": {}, // Leica
	"SR2": {}, // Sony
	"SRF": {}, // Sony
	"SRW": {}, // Samsung
	"X3F": {}, // Sigma
}

// isRawFileType returns true if the given exiftool FileType value is a RAW format.
func isRawFileType(fileType string) bool {
	_, ok := rawFileTypes[strings.ToUpper(fileType)]
	return ok
}


// We try each in order until one parses.
var knownTimeLayouts = []string{
	"2006:01:02 15:04:05",         // standard EXIF
	"2006:01:02 15:04:05-07:00",   // with timezone offset
	"2006:01:02 15:04:05Z",        // with UTC marker
	"2006-01-02T15:04:05",         // ISO 8601 without TZ
	"2006-01-02T15:04:05-07:00",   // ISO 8601 with TZ
	"2006-01-02T15:04:05Z",        // ISO 8601 UTC
}

// Extractor implements photo.EXIFExtractor using exiftool.
type Extractor struct {
	// ExiftoolPath is the path to the exiftool binary.
	// If empty, "exiftool" is looked up on PATH at call time.
	ExiftoolPath string
}

// NewExtractor returns a new Extractor. exiftoolPath may be empty to use
// PATH lookup, or an absolute path (e.g. "/usr/local/bin/exiftool").
func NewExtractor(exiftoolPath string) *Extractor {
	return &Extractor{ExiftoolPath: exiftoolPath}
}

// CheckDependency verifies that exiftool is available and executable.
// Call this once at program startup to give the user a clear error message.
func (e *Extractor) CheckDependency() error {
	path, err := e.resolvedPath()
	if err != nil {
		return fmt.Errorf(
			"exiftool not found: install it with:\n"+
				"  macOS:  brew install exiftool\n"+
				"  Debian: sudo apt install libimage-exiftool-perl\n"+
				"  RHEL:   sudo dnf install perl-Image-ExifTool",
		)
	}

	// Run a no-op invocation to confirm it's actually executable.
	cmd := exec.Command(path, "-ver")
	if out, err := cmd.Output(); err != nil {
		return fmt.Errorf("exiftool at %q is not executable: %w", path, err)
	} else {
		version := strings.TrimSpace(string(out))
		_ = version // could log this if desired
	}
	return nil
}

// Extract reads EXIF metadata from the file at path using exiftool.
// Returns ENOTFOUND if the file does not exist.
// Returns EINTERNAL if exiftool fails for any other reason.
// A file with no EXIF data returns an empty EXIFData, not an error.
func (e *Extractor) Extract(ctx context.Context, path string) (*photo.EXIFData, error) {
	exiftoolPath, err := e.resolvedPath()
	if err != nil {
		return nil, photo.Errorf(photo.EINTERNAL,
			"exiftool not found; run 'photo config check' for install instructions")
	}

	// -json        → output as JSON array
	// -q           → quiet: suppress progress output
	// -coordFormat → GPS coords as decimal degrees (not degrees/minutes/seconds)
	// -d           → datetime format string
	cmd := exec.CommandContext(ctx, exiftoolPath,
		"-json",
		"-q",
		"-coordFormat", "%.8f",
		"-d", "%Y:%m:%d %H:%M:%S",
		path,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Exit code 1 with a "File not found" message means the file is missing.
		if strings.Contains(stderr.String(), "File not found") ||
			strings.Contains(stderr.String(), "No such file") {
			return nil, photo.Errorf(photo.ENOTFOUND, "file not found: %s", path)
		}
		return nil, photo.Errorf(photo.EINTERNAL,
			"exiftool failed on %q: %s", path, strings.TrimSpace(stderr.String()))
	}

	// exiftool returns a JSON array; we only ever pass one file so we take [0].
	var raw []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, photo.Errorf(photo.EINTERNAL,
			"exiftool: parse JSON for %q: %v", path, err)
	}
	if len(raw) == 0 {
		// No metadata at all — return empty struct, not an error.
		return &photo.EXIFData{}, nil
	}

	tags := raw[0]

	data := &photo.EXIFData{}
	data.FileType = stringTag(tags, "FileType")
	data.MIMEType = stringTag(tags, "MIMEType")
	data.IsRaw = isRawFileType(data.FileType)
	data.Make = stringTag(tags, "Make")
	data.Model = stringTag(tags, "Model")
	data.LensModel = firstString(tags, "LensModel", "LensID", "Lens")
	data.FocalLength = stringTag(tags, "FocalLength")
	data.Aperture = firstString(tags, "Aperture", "FNumber", "ApertureValue")
	data.ShutterSpeed = firstString(tags, "ShutterSpeed", "ExposureTime", "ShutterSpeedValue")
	data.ISO = intTag(tags, "ISO")
	data.CapturedAt = parseTime(tags)
	data.GPSLat, data.GPSLon = parseGPS(tags)

	// Store the full raw JSON for the blob column.
	rawJSON, _ := json.Marshal(tags)
	data.Raw = string(rawJSON)

	return data, nil
}

// ExtractReader reads EXIF metadata from r by writing its contents to a
// temporary file and calling Extract. The temp file is removed afterward.
// filename is used only as a suffix for the temp file so exiftool can infer
// the format from the extension (e.g. "upload.cr2").
func (e *Extractor) ExtractReader(ctx context.Context, r io.Reader, filename string) (*photo.EXIFData, error) {
	// Derive the extension from filename for format detection.
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = ".tmp"
	}

	// Write to a temp file. os.CreateTemp gives us a unique name.
	tmp, err := os.CreateTemp("", "photo-exif-*"+ext)
	if err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "exif: create temp file: %v", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return nil, photo.Errorf(photo.EINTERNAL, "exif: write temp file: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "exif: close temp file: %v", err)
	}

	return e.Extract(ctx, tmp.Name())
}


func (e *Extractor) resolvedPath() (string, error) {
	if e.ExiftoolPath != "" {
		return e.ExiftoolPath, nil
	}
	return exec.LookPath("exiftool")
}

// --- tag extraction helpers -------------------------------------------------

// stringTag returns the string value of a tag, or "" if absent or wrong type.
func stringTag(tags map[string]interface{}, key string) string {
	v, ok := tags[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// firstString returns the value of the first key in keys that is non-empty.
func firstString(tags map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s := stringTag(tags, k); s != "" {
			return s
		}
	}
	return ""
}

// intTag returns the integer value of a tag, or 0 if absent or unparseable.
func intTag(tags map[string]interface{}, key string) int {
	v, ok := tags[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(math.Round(n))
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

// parseTime tries several EXIF datetime keys and several layouts to find
// a parseable timestamp. Returns nil if none can be parsed.
func parseTime(tags map[string]interface{}) *time.Time {
	// Prefer DateTimeOriginal (shutter press time) over CreateDate (write time).
	for _, key := range []string{
		"DateTimeOriginal",
		"CreateDate",
		"ModifyDate",
		"DateTime",
		"GPSDateTime",
	} {
		s := stringTag(tags, key)
		if s == "" || s == "0000:00:00 00:00:00" {
			continue
		}
		for _, layout := range knownTimeLayouts {
			if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
				t = t.UTC()
				return &t
			}
		}
	}
	return nil
}

// parseGPS extracts decimal-degree GPS coordinates from the exiftool output.
// exiftool is invoked with -coordFormat "%.8f" so values arrive as floats or
// as float-formatted strings. Returns (nil, nil) if no valid coords found.
func parseGPS(tags map[string]interface{}) (*float64, *float64) {
	// With -coordFormat the keys are GPSLatitude / GPSLongitude as numbers.
	lat, latOK := floatTag(tags, "GPSLatitude")
	lon, lonOK := floatTag(tags, "GPSLongitude")
	if !latOK || !lonOK {
		return nil, nil
	}

	// Guard against degenerate 0,0 values which appear when GPS is present in
	// the tag structure but holds no actual fix.
	if lat == 0 && lon == 0 {
		return nil, nil
	}

	return &lat, &lon
}

// floatTag extracts a float64 value from a tag map. The value may be a
// float64 (from JSON number) or a string (e.g. "35.6895140 N").
// Compass suffixes (N/S/E/W) are handled: S and W negate the value.
func floatTag(tags map[string]interface{}, key string) (float64, bool) {
	v, ok := tags[key]
	if !ok {
		return 0, false
	}

	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}

		// Handle "35.6895140 N" style strings.
		negative := false
		if strings.HasSuffix(s, " S") || strings.HasSuffix(s, " W") {
			negative = true
			s = s[:len(s)-2]
		} else if strings.HasSuffix(s, " N") || strings.HasSuffix(s, " E") {
			s = s[:len(s)-2]
		}
		s = strings.TrimSpace(s)

		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		if negative {
			f = -f
		}
		return f, true
	}
	return 0, false
}

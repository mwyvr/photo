package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func runShow(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	showEXIF := fs.Bool("exif", false, "print raw EXIF JSON from exiftool")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo show [--exif] <photo-id>

Display full details for a single photo.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck
	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	p, err := c.getPhoto(ctx, fs.Arg(0))
	if err != nil {
		return fmt.Errorf("show: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintf(w, "ID:\t%s\n", p.ID)
	fmt.Fprintf(w, "File:\t%s\n", p.Filename)
	fmt.Fprintf(w, "Path:\t%s\n", p.StoredPath)
	fmt.Fprintf(w, "Type:\t%s\n", orDash(p.FileType))
	fmt.Fprintf(w, "MIME:\t%s\n", p.MIMEType)
	fmt.Fprintf(w, "RAW:\t%v\n", p.IsRaw)
	fmt.Fprintf(w, "Size:\t%s\n", formatBytes(p.FileSizeBytes))

	fmt.Fprintln(w, "\n── Camera ───────────────────────────")
	fmt.Fprintf(w, "Make:\t%s\n", orDash(p.CameraMake))
	fmt.Fprintf(w, "Model:\t%s\n", orDash(p.CameraModel))
	fmt.Fprintf(w, "Lens:\t%s\n", orDash(p.LensModel))

	fmt.Fprintln(w, "\n── Exposure ─────────────────────────")
	fmt.Fprintf(w, "Focal length:\t%s\n", orDash(p.FocalLength))
	fmt.Fprintf(w, "Aperture:\t%s\n", orDash(p.Aperture))
	fmt.Fprintf(w, "Shutter:\t%s\n", orDash(p.ShutterSpeed))
	if p.ISO != 0 {
		fmt.Fprintf(w, "ISO:\t%d\n", p.ISO)
	} else {
		fmt.Fprintf(w, "ISO:\t—\n")
	}

	fmt.Fprintln(w, "\n── Time & Location ─────────────────")
	if p.CapturedAt != nil {
		fmt.Fprintf(w, "Captured:\t%s\n", p.CapturedAt.Format("2006-01-02 15:04:05 UTC"))
	} else {
		fmt.Fprintf(w, "Captured:\t—\n")
	}
	if p.GPSLat != nil && p.GPSLon != nil {
		fmt.Fprintf(w, "GPS:\t%.6f, %.6f\n", *p.GPSLat, *p.GPSLon)
	}
	fmt.Fprintf(w, "Location:\t%s\n", orDash(p.LocationName))

	fmt.Fprintln(w, "\n── Tags & Notes ────────────────────")
	var tagNames []string
	for _, t := range p.Tags {
		tagNames = append(tagNames, t.Name)
	}
	fmt.Fprintf(w, "Tags:\t%s\n", orDash(strings.Join(tagNames, ", ")))
	if p.Description != "" {
		fmt.Fprintf(w, "Description:\t%s\n", p.Description)
	}

	w.Flush()

	if *showEXIF {
		if p.EXIFRaw == "" {
			fmt.Println("\nNo EXIF data available.")
		} else {
			fmt.Println("\n── EXIF (raw) ───────────────────────")
			// Pretty-print the stored JSON blob.
			var raw interface{}
			if err := json.Unmarshal([]byte(p.EXIFRaw), &raw); err != nil {
				fmt.Println(p.EXIFRaw)
			} else {
				pretty, _ := json.MarshalIndent(raw, "", "  ")
				fmt.Println(string(pretty))
			}
		}
	}

	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

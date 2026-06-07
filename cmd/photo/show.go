package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func runShow(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo show <photo-id>

Display full details for a single photo.
`)
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
	fmt.Fprintf(w, "Model:\t%s\n", orDash(p.CameraModel))

	fmt.Fprintln(w, "\n── Time & Location ─────────────────")
	if p.CapturedAt != nil {
		fmt.Fprintf(w, "Captured:\t%s\n", p.CapturedAt.Format("2006-01-02 15:04:05 UTC"))
	} else {
		fmt.Fprintf(w, "Captured:\t—\n")
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

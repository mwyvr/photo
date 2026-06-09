package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// datePatterns are path component patterns that should NOT become tags.
// These represent date or year directory names commonly produced by
// Lightroom and other photo tools during export.
var datePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\d{4}$`),              // 2024
	regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`), // 2025-08-01
	regexp.MustCompile(`^\d{4}-\d{2}$`),        // 2025-08
	regexp.MustCompile(`^\d{2}-\d{4}$`),        // 08-2025
	regexp.MustCompile(`^\d{8}$`),              // 20250801
}

// isDateComponent returns true if the path component looks like a date or year.
func isDateComponent(s string) bool {
	for _, p := range datePatterns {
		if p.MatchString(s) {
			return true
		}
	}
	return false
}

// tagsFromPath derives tags from the directory components between root and
// the file. Date-shaped components are excluded. Files directly under root
// produce no tags.
//
// Examples (root = ~/Pictures/Publish):
//
//	~/Pictures/Publish/photo.jpg              → []
//	~/Pictures/Publish/travel/photo.jpg       → ["travel"]
//	~/Pictures/Publish/travel/france/photo.jpg → ["travel", "france"]
//	~/Pictures/Publish/2025/travel/photo.jpg  → ["travel"]
//	~/Pictures/Publish/2025/2025-08-01/p.jpg  → []
func tagsFromPath(root, filePath string) []string {
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return nil
	}

	// Split into components and drop the filename (last element).
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) <= 1 {
		return nil // file is directly under root
	}
	parts = parts[:len(parts)-1] // remove filename

	var tags []string
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if isDateComponent(part) {
			continue
		}
		tags = append(tags, strings.ToLower(part))
	}
	return tags
}

// runPublish handles 'photo publish <path>'.
//
// Walks the directory, derives tags from path components, pre-flight checks
// each file against the server, uploads only new files, marks them published.
// Exits non-zero if any file fails — caller's script can decide what to do.
func runPublish(ctx context.Context, c *client, cfg *Config, args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "show what would be uploaded without making any changes")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo publish [--dry-run] <path>

Upload all photos under <path> to the library. Tags are derived automatically
from the directory structure — e.g. Publish/travel/france/photo.jpg is tagged
'travel' and 'france'. Date and year directory names are ignored.

All uploaded photos are marked as published (publicly visible).
Files already in the library are skipped cheaply via a SHA-256 pre-flight check.
Files that fail to upload are reported; the command exits non-zero on any failure.

Local files are NOT deleted by this command. Use the exit code in a script
to decide whether to remove them after a successful run.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	root, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("cannot access %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", root)
	}

	if *dryRun {
		fmt.Println("Dry run — no files will be uploaded.")
	}

	paths, err := collectFiles(root)
	if err != nil {
		return fmt.Errorf("collect files: %w", err)
	}

	var uploaded, skipped, errored int
	var failedPaths []string

	for _, p := range paths {
		base := filepath.Base(p)
		tags := tagsFromPath(root, p)

		if *dryRun {
			tagStr := ""
			if len(tags) > 0 {
				tagStr = " [" + strings.Join(tags, ", ") + "]"
			}
			fmt.Printf("  publish  %-40s%s\n", base, tagStr)
			uploaded++
			continue
		}

		ph, err := c.uploadPhotoOpts(ctx, p, false, true, tags)
		if err != nil {
			if isAlreadyExists(err) {
				fmt.Printf("  skip     %-40s already in library\n", base)
				skipped++
				continue
			}
			fmt.Fprintf(os.Stderr, "  error    %-40s %v\n", base, err)
			errored++
			failedPaths = append(failedPaths, p)
			continue
		}

		// Warn if a RAW file ended up in the publish directory — the server
		// will have overridden published=true to false for it.
		if ph.IsRaw {
			fmt.Fprintf(os.Stderr, "  warn     %-40s RAW file — not published (use 'photo add' for RAW)\n", base)
			skipped++
			uploaded--
			continue
		}

		tagStr := ""
		if len(tags) > 0 {
			tagStr = " [" + strings.Join(tags, ", ") + "]"
		}
		if ph.LocationName != "" {
			fmt.Printf("  publish  %-40s %s%s\n", base, ph.LocationName, tagStr)
		} else {
			fmt.Printf("  publish  %-40s%s\n", base, tagStr)
		}
		uploaded++
	}

	fmt.Println()
	verb := "published"
	if *dryRun {
		verb = "would publish"
	}
	fmt.Printf("Done. %d %s, %d skipped", uploaded, verb, skipped)
	if errored > 0 {
		fmt.Printf(", %d errors", errored)
	}
	fmt.Println(".")

	if errored > 0 {
		fmt.Fprintf(os.Stderr, "\nFailed files:\n")
		for _, p := range failedPaths {
			fmt.Fprintf(os.Stderr, "  %s\n", p)
		}
		return fmt.Errorf("%d file(s) failed to upload", errored)
	}
	return nil
}

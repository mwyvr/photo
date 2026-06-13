package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

// runBackup handles 'photo backup [output-path]'.
// Downloads a consistent database snapshot from the server.
// Admin only — the server returns 403 for non-admin users.
func runBackup(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo backup [output-path]

Download a complete database backup from the server. Admin only.

If output-path is not given, writes to library-YYYY-MM-DD.db
in the current directory.

`)
	}
	fs.Parse(args) //nolint:errcheck

	outPath := fmt.Sprintf("library-%s.db", time.Now().UTC().Format("2006-01-02"))
	if fs.NArg() > 0 {
		outPath = fs.Arg(0)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	if err := c.backup(ctx, f); err != nil {
		os.Remove(outPath) //nolint:errcheck
		return fmt.Errorf("backup: %w", err)
	}

	info, err := f.Stat()
	if err == nil {
		fmt.Printf("Backup written to %s (%s).\n", outPath, formatBytes(info.Size()))
	} else {
		fmt.Printf("Backup written to %s.\n", outPath)
	}
	return nil
}

// formatBytes is defined in show.go.

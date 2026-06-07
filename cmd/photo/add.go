package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mwyvr/photo/importer"
)

func runAdd(ctx context.Context, c *client, cfg *Config, args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "show what would be imported without uploading")
	rawOnly := fs.Bool("raw-only", false, "skip non-RAW images (JPEG, PNG, HEIC, etc.)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo add [--dry-run] [--raw-only] <path>

Import a photo file or a directory tree. Directories are scanned recursively.
Files are uploaded to the photod server via multipart POST.
Duplicates are skipped automatically.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	srcPath := fs.Arg(0)
	if *dryRun {
		fmt.Println("Dry run — no files will be uploaded.")
	}

	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("cannot access %q: %w", srcPath, err)
	}

	var paths []string
	if info.IsDir() {
		paths, err = collectFiles(srcPath)
		if err != nil {
			return err
		}
	} else {
		paths = []string{srcPath}
	}

	var added, skipped, errored int
	for _, p := range paths {
		base := filepath.Base(p)
		ext := strings.ToLower(filepath.Ext(p))

		if *dryRun {
			if _, ok := importer.SupportedExtensions[ext]; !ok {
				fmt.Printf("  skip   %-40s unsupported type\n", base)
				skipped++
			} else {
				fmt.Printf("  add    %-40s (dry run)\n", base)
				added++
			}
			continue
		}

		ph, err := c.uploadPhoto(ctx, p, *rawOnly)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  error  %-40s %v\n", base, err)
			errored++
			continue
		}
		if ph.LocationName != "" {
			fmt.Printf("  add    %-40s [%s]\n", base, ph.LocationName)
		} else {
			fmt.Printf("  add    %-40s %s\n", base, ph.StoredPath)
		}
		added++
	}

	fmt.Println()
	verb := "uploaded"
	if *dryRun {
		verb = "would upload"
	}
	fmt.Printf("Done. %d %s, %d skipped", added, verb, skipped)
	if errored > 0 {
		fmt.Printf(", %d errors", errored)
	}
	fmt.Println(".")
	return nil
}

func collectFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
}

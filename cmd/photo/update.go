package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// runUpdate handles 'photo update <photo-id> --description <text>'.
func runUpdate(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	description := fs.String("description", "", "set the photo description")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo update [flags] <photo-id>

Update mutable fields on a photo.

Example:
  photo update --description "Sunset over the harbour" 06bq7xhnr03mlz6r

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}
	if *description == "" {
		return fmt.Errorf("at least one field to update is required (--description)")
	}

	id := fs.Arg(0)
	p, err := c.updatePhoto(ctx, id, *description)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	fmt.Printf("Updated photo %s.\n", p.ID)
	return nil
}

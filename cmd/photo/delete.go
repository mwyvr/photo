package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// runDelete handles 'photo delete <photo-id>'.
func runDelete(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	force := fs.Bool("force", false, "skip confirmation prompt")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo delete [--force] <photo-id>

Remove a photo record from the library. The file on disk is also deleted.
You will be prompted to confirm unless --force is given.

Example:
  photo delete 06bq7xhnr03mlz6r
  photo delete --force 06bq7xhnr03mlz6r
`)
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	id := fs.Arg(0)

	// Fetch the photo first so we can show the user what they're deleting.
	p, err := c.getPhoto(ctx, id)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	if !*force {
		fmt.Printf("Delete %s (%s)?  This cannot be undone. [y/N] ", p.Filename, p.StoredPath)
		var answer string
		fmt.Fscanln(os.Stdin, &answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := c.deletePhoto(ctx, id); err != nil {
		return fmt.Errorf("delete: %w", err)
	}

	fmt.Printf("Deleted %s.\n", p.Filename)
	return nil
}

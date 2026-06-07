package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mwyvr/photo"
)

func runTag(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("tag", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo tag <photo-id> <tag>

Attach a tag to a photo. The tag is created if it does not exist.

Example:
  photo tag 06bq7xhnr03mlz6r travel
`)
	}
	fs.Parse(args) //nolint:errcheck
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(1)
	}

	photoID := fs.Arg(0)
	tagName := photo.NormalizeTagName(fs.Arg(1))

	if err := c.attachTag(ctx, photoID, tagName); err != nil {
		return fmt.Errorf("tag: %w", err)
	}
	fmt.Printf("Tagged photo %s with %q.\n", photoID, tagName)
	return nil
}

func runUntag(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("untag", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo untag <photo-id> <tag>

Remove a tag from a photo.

Example:
  photo untag 06bq7xhnr03mlz6r travel
`)
	}
	fs.Parse(args) //nolint:errcheck
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(1)
	}

	photoID := fs.Arg(0)
	tagName := photo.NormalizeTagName(fs.Arg(1))

	if err := c.detachTag(ctx, photoID, tagName); err != nil {
		return fmt.Errorf("untag: %w", err)
	}
	fmt.Printf("Removed tag %q from photo %s.\n", tagName, photoID)
	return nil
}

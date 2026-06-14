package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

func runShare(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("share", flag.ExitOnError)
	revoke := fs.Bool("revoke", false, "revoke the share link for this photo")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo share [--revoke] <photo-id>

Generate or revoke a share link for a photo.

A share link allows anyone with the URL to view the photo without logging in,
regardless of its visibility setting. Share links are revocable: generating
a new token invalidates the previous one.

Examples:
  photo share 06bq7xhnr03mlz6r
  photo share --revoke 06bq7xhnr03mlz6r

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}
	id := fs.Arg(0)

	if *revoke {
		if err := c.revokePhotoShare(ctx, id); err != nil {
			return fmt.Errorf("share revoke: %w", err)
		}
		fmt.Println("Share link revoked.")
		return nil
	}

	token, err := c.generatePhotoShare(ctx, id)
	if err != nil {
		return fmt.Errorf("share: %w", err)
	}

	fmt.Printf("Share link: %s/s/%s\n", c.baseURL, token)
	fmt.Println("Anyone with this link can view the photo without logging in.")
	fmt.Println("Revoke it at any time with: photo share --revoke", id)
	return nil
}

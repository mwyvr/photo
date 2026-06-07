package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// runRegeocode handles 'photo regeocode <photo-id> [--location <name>]'.
func runRegeocode(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("regeocode", flag.ExitOnError)
	location := fs.String("location", "", "set location name manually instead of using GPS")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo regeocode [--location <name>] <photo-id>

Set or update the location name for a photo.

Without --location, the photo's GPS coordinates are sent to the geocoding
service to resolve a place name. Fails if the photo has no GPS data.

With --location, the provided string is stored directly. Use this for photos
without GPS data or to correct an inaccurate geocode result.

Examples:
  photo regeocode 06bq7xhnr03mlz6r
  photo regeocode --location "Dawson Creek, Canada" 06bq7xhnr03mlz6r

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
	p, err := c.regeocode(ctx, id, *location)
	if err != nil {
		return fmt.Errorf("regeocode: %w", err)
	}

	if p.LocationName != "" {
		fmt.Printf("Location set to %q.\n", p.LocationName)
	} else {
		fmt.Println("Location cleared.")
	}
	return nil
}

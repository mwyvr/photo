package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// runRegeocode handles 'photo regeocode <photo-id> [--location <name>]'
// and 'photo regeocode --all-missing'.
func runRegeocode(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("regeocode", flag.ExitOnError)
	location := fs.String("location", "", "set location name manually instead of using GPS")
	allMissing := fs.Bool("all-missing", false, "regeocode all of your photos that have GPS but no location name")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo regeocode [--location <name>] <photo-id>
       photo regeocode --all-missing

Set or update the location name for a photo.

Without --location, the photo's GPS coordinates are sent to the geocoding
service to resolve a place name. Fails if the photo has no GPS data.

With --location, the provided string is stored directly. Use this for photos
without GPS data or to correct an inaccurate geocode result.

With --all-missing, every photo you own that has GPS coordinates but no
location name is regeocoded automatically. This may take a while — the
geocoding service is rate-limited to one request per second.

Examples:
  photo regeocode 06bq7xhnr03mlz6r
  photo regeocode --location "Dawson Creek, Canada" 06bq7xhnr03mlz6r
  photo regeocode --all-missing

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if *allMissing {
		if fs.NArg() != 0 {
			return fmt.Errorf("--all-missing does not take a photo ID argument")
		}
		if *location != "" {
			return fmt.Errorf("--all-missing cannot be combined with --location")
		}
		return runRegeocodeAllMissing(ctx, c)
	}

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

func runRegeocodeAllMissing(ctx context.Context, c *client) error {
	fmt.Println("Finding photos with GPS but no location name...")
	fmt.Println("This may take a while — the geocoding service allows one request per second.")
	fmt.Println()

	result, err := c.regeocodeMissing(ctx)
	if err != nil {
		return fmt.Errorf("regeocode --all-missing: %w", err)
	}

	if result.Total == 0 {
		fmt.Println("No photos need regeocoding.")
		return nil
	}

	fmt.Printf("Updated %d of %d photo(s).\n", result.Updated, result.Total)
	if len(result.Failures) > 0 {
		fmt.Printf("\n%d failure(s):\n", len(result.Failures))
		for _, f := range result.Failures {
			fmt.Printf("  %s: %s\n", f.PhotoID, f.Error)
		}
	}
	return nil
}

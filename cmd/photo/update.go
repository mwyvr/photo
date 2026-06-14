package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

// runUpdate handles 'photo update <photo-id> [flags]'.
func runUpdate(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	description := fs.String("description", "", "set the photo description")
	location := fs.String("location", "", "set the location name manually")
	visibility := fs.String("visibility", "", "set visibility: private, household, or published")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo update [flags] <photo-id>

Update mutable fields on a photo. At least one flag is required.

Examples:
  photo update --description "Sunset over the harbour" 06bq7xhnr03mlz6r
  photo update --location "Dawson Creek, Canada" 06bq7xhnr03mlz6r
  photo update --published=true 06bq7xhnr03mlz6r
  photo update --published=false 06bq7xhnr03mlz6r

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	if *description == "" && *location == "" && *visibility == "" {
		return fmt.Errorf("at least one field to update is required (--description, --location, --visibility)")
	}

	if *visibility != "" {
		switch *visibility {
		case "private", "household", "published":
			// valid
		default:
			return fmt.Errorf("--visibility must be \"private\", \"household\", or \"published\", got %q", *visibility)
		}
	}

	id := fs.Arg(0)
	p, err := c.updatePhoto(ctx, id, *description, *location, *visibility)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}

	fmt.Printf("Updated photo %s.\n", p.ID)
	if *description != "" {
		fmt.Printf("  Description: %s\n", p.Description)
	}
	if *location != "" {
		fmt.Printf("  Location:    %s\n", p.LocationName)
	}
	if *visibility != "" {
		fmt.Printf("  Visibility:  %s\n", p.Visibility)
	}
	return nil
}

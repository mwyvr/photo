package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func runSearch(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)

	var tags multiFlag
	fs.Var(&tags, "tag", "filter by tag (repeatable; all must match)")
	location := fs.String("location", "", "filter by city or country (substring match)")
	after    := fs.String("after", "", "only photos taken on or after YYYY-MM-DD")
	before   := fs.String("before", "", "only photos taken on or before YYYY-MM-DD")
	limit    := fs.Int("limit", 50, "maximum results")
	offset   := fs.Int("offset", 0, "skip this many results")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo search [flags]

Search photos by date range, location, or tag. All filters are ANDed.

Examples:
  photo search --location Tokyo
  photo search --tag travel --tag sunset
  photo search --after 2023-01-01 --before 2023-12-31

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	resp, err := c.listPhotos(ctx, searchParams{
		Location: *location,
		After:    *after,
		Before:   *before,
		Tags:     tags,
		Limit:    *limit,
		Offset:   *offset,
	})
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(resp.Photos) == 0 {
		fmt.Println("No photos found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tDate\tCamera\tLocation\tTags")
	fmt.Fprintln(w, "──\t────\t──────\t────────\t────")
	for _, p := range resp.Photos {
		date := "unknown"
		if p.CapturedAt != nil {
			date = p.CapturedAt.Format("2006-01-02")
		}
		camera := orDash(p.CameraModel)
		location := orDash(p.LocationName)
		var tagNames []string
		for _, t := range p.Tags {
			tagNames = append(tagNames, t.Name)
		}
		tagStr := orDash(strings.Join(tagNames, ", "))
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, date, camera, location, tagStr)
	}
	w.Flush()

	shown := *offset + len(resp.Photos)
	if resp.Total > shown {
		fmt.Printf("\nShowing %d–%d of %d. Use --offset %d to see more.\n",
			*offset+1, shown, resp.Total, shown)
	} else {
		fmt.Printf("\n%d photo(s) found.\n", resp.Total)
	}
	return nil
}

// multiFlag accumulates repeated flag values.
type multiFlag []string

func (f *multiFlag) String() string  { return strings.Join(*f, ", ") }
func (f *multiFlag) Set(v string) error { *f = append(*f, v); return nil }

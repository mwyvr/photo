package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

func runStatus(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo status

Display statistics for your own photos.
`)
	}
	fs.Parse(args) //nolint:errcheck

	st, err := c.getStatus(ctx)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	return printStatus(st)
}

// printStatus writes a statusJSON in human-readable tabular form.
func printStatus(st *statusJSON) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "── Photos ───────────────────────────")
	fmt.Fprintf(w, "Total:\t%d\n", st.TotalPhotos)
	fmt.Fprintf(w, "RAW:\t%d\n", st.TotalRAW)
	fmt.Fprintf(w, "Non-RAW:\t%d\n", st.TotalNonRAW)
	fmt.Fprintf(w, "Private:\t%d\n", st.TotalPrivate)
	fmt.Fprintf(w, "Household:\t%d\n", st.TotalHousehold)
	fmt.Fprintf(w, "Published:\t%d\n", st.TotalPublished)
	fmt.Fprintf(w, "Total size:\t%s\n", formatBytes(int64(st.TotalSizeBytes)))

	fmt.Fprintln(w, "\n── Metadata ─────────────────────────")
	fmt.Fprintf(w, "With location:\t%d\n", st.WithLocation)
	fmt.Fprintf(w, "Without location:\t%d\n", st.WithoutLocation)
	fmt.Fprintf(w, "With GPS:\t%d\n", st.WithGPS)
	fmt.Fprintf(w, "With description:\t%d\n", st.WithDescription)

	fmt.Fprintln(w, "\n── Library ──────────────────────────")
	fmt.Fprintf(w, "Tags:\t%d\n", st.TotalTags)
	fmt.Fprintf(w, "Albums:\t%d\n", st.TotalAlbums)
	if st.OldestCapturedAt != nil {
		fmt.Fprintf(w, "Oldest photo:\t%s\n", st.OldestCapturedAt.Format("2006-01-02"))
	}
	if st.NewestCapturedAt != nil {
		fmt.Fprintf(w, "Newest photo:\t%s\n", st.NewestCapturedAt.Format("2006-01-02"))
	}
	if st.OldestCapturedAt != nil && st.NewestCapturedAt != nil {
		span := st.NewestCapturedAt.Sub(*st.OldestCapturedAt)
		years := int(span.Hours() / 24 / 365)
		months := int(span.Hours()/24/30) % 12
		fmt.Fprintf(w, "Date span:\t%dy %dm\n", years, months)
	}

	return w.Flush()
}

// statusJSON mirrors photo.LibraryStatus for JSON decoding in the client.
type statusJSON struct {
	TotalPhotos      int        `json:"totalPhotos"`
	TotalRAW         int        `json:"totalRaw"`
	TotalNonRAW      int        `json:"totalNonRaw"`
	TotalPrivate     int        `json:"totalPrivate"`
	TotalHousehold   int        `json:"totalHousehold"`
	TotalPublished   int        `json:"totalPublished"`
	TotalSizeBytes   int64      `json:"totalSizeBytes"`
	WithLocation     int        `json:"withLocation"`
	WithoutLocation  int        `json:"withoutLocation"`
	WithGPS          int        `json:"withGps"`
	WithDescription  int        `json:"withDescription"`
	TotalTags        int        `json:"totalTags"`
	TotalAlbums      int        `json:"totalAlbums"`
	OldestCapturedAt *time.Time `json:"oldestCapturedAt"`
	NewestCapturedAt *time.Time `json:"newestCapturedAt"`
}

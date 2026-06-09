package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// albumJSON mirrors photo.Album for JSON decoding in the client.
type albumJSON struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	CoverPhotoID *string    `json:"coverPhotoId,omitempty"`
	PhotoCount   int        `json:"photoCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type listAlbumsResponse struct {
	Albums []albumJSON `json:"albums"`
	Total  int         `json:"total"`
}

// runAlbum dispatches album subcommands.
//
//	photo album list
//	photo album create <name>
//	photo album show <id>
//	photo album delete <id>
//	photo album add <album-id> <photo-id>
//	photo album remove <album-id> <photo-id>
//	photo album move <album-id> <photo-id> after <other-photo-id>
//	photo album move <album-id> <photo-id> first
//	photo album cover <album-id> <photo-id>
func runAlbum(ctx context.Context, c *client, args []string) error {
	if len(args) == 0 {
		albumUsage()
		os.Exit(1)
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		return runAlbumList(ctx, c, rest)
	case "create":
		return runAlbumCreate(ctx, c, rest)
	case "show":
		return runAlbumShow(ctx, c, rest)
	case "delete":
		return runAlbumDelete(ctx, c, rest)
	case "add":
		return runAlbumAdd(ctx, c, rest)
	case "remove":
		return runAlbumRemove(ctx, c, rest)
	case "move":
		return runAlbumMove(ctx, c, rest)
	case "cover":
		return runAlbumCover(ctx, c, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown album subcommand %q\n\n", sub)
		albumUsage()
		os.Exit(1)
		return nil
	}
}

func albumUsage() {
	fmt.Fprint(os.Stderr, `Usage: photo album <subcommand>

Subcommands:
  list                          List all albums
  create <name>                 Create a new album
  show   <album-id>             Show album details and photo list
  delete <album-id>             Delete an album (photos are not deleted)
  add    <album-id> <photo-id>  Add a photo to an album
  remove <album-id> <photo-id>  Remove a photo from an album
  move   <album-id> <photo-id> after <other-photo-id>
                                Move a photo after another in the album
  move   <album-id> <photo-id> first
                                Move a photo to the beginning of the album
  cover  <album-id> <photo-id>  Set the album cover photo

`)
}

func runAlbumList(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("album list", flag.ExitOnError)
	fs.Parse(args) //nolint:errcheck

	var resp listAlbumsResponse
	if err := c.do(ctx, "GET", "/api/v1/albums", nil, &resp); err != nil {
		return fmt.Errorf("album list: %w", err)
	}

	if len(resp.Albums) == 0 {
		fmt.Println("No albums.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tName\tPhotos\tCreated")
	fmt.Fprintln(w, "──\t────\t──────\t───────")
	for _, a := range resp.Albums {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			a.ID, a.Name, a.PhotoCount,
			a.CreatedAt.Format("2006-01-02"),
		)
	}
	w.Flush()
	fmt.Printf("\n%d album(s).\n", resp.Total)
	return nil
}

func runAlbumCreate(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("album create", flag.ExitOnError)
	desc := fs.String("description", "", "album description")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo album create [--description <text>] <name>

Create a new album.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(1)
	}

	name := strings.Join(fs.Args(), " ")
	body := map[string]string{"name": name, "description": *desc}

	var a albumJSON
	if err := c.do(ctx, "POST", "/api/v1/albums", body, &a); err != nil {
		return fmt.Errorf("album create: %w", err)
	}
	fmt.Printf("Created album %q (%s).\n", a.Name, a.ID)
	return nil
}

func runAlbumShow(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("album show", flag.ExitOnError)
	fs.Parse(args) //nolint:errcheck
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: photo album show <album-id>")
		os.Exit(1)
	}

	id := fs.Arg(0)
	var a albumJSON
	if err := c.do(ctx, "GET", "/api/v1/albums/"+id, nil, &a); err != nil {
		return fmt.Errorf("album show: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "ID:\t%s\n", a.ID)
	fmt.Fprintf(w, "Name:\t%s\n", a.Name)
	if a.Description != "" {
		fmt.Fprintf(w, "Description:\t%s\n", a.Description)
	}
	if a.CoverPhotoID != nil {
		fmt.Fprintf(w, "Cover photo:\t%s\n", *a.CoverPhotoID)
	}
	fmt.Fprintf(w, "Photos:\t%d\n", a.PhotoCount)
	fmt.Fprintf(w, "Created:\t%s\n", a.CreatedAt.Format("2006-01-02"))
	w.Flush()

	if a.PhotoCount == 0 {
		return nil
	}

	// List photos in the album.
	fmt.Println()
	var photosResp struct {
		Photos []photoJSON `json:"photos"`
		Total  int         `json:"total"`
	}
	if err := c.do(ctx, "GET", "/api/v1/albums/"+id+"/photos?limit=50", nil, &photosResp); err != nil {
		return fmt.Errorf("album photos: %w", err)
	}
	pw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(pw, "Photo ID\tDate\tFile")
	fmt.Fprintln(pw, "────────\t────\t────")
	for _, p := range photosResp.Photos {
		date := "—"
		if p.CapturedAt != nil {
			date = p.CapturedAt.Format("2006-01-02")
		}
		fmt.Fprintf(pw, "%s\t%s\t%s\n", p.ID, date, p.Filename)
	}
	pw.Flush()
	if photosResp.Total > 50 {
		fmt.Printf("\nShowing 50 of %d photos.\n", photosResp.Total)
	}
	return nil
}

func runAlbumDelete(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("album delete", flag.ExitOnError)
	force := fs.Bool("force", false, "skip confirmation")
	fs.Parse(args) //nolint:errcheck
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: photo album delete [--force] <album-id>")
		os.Exit(1)
	}

	id := fs.Arg(0)
	var a albumJSON
	if err := c.do(ctx, "GET", "/api/v1/albums/"+id, nil, &a); err != nil {
		return fmt.Errorf("album delete: %w", err)
	}

	if !*force {
		fmt.Printf("Delete album %q (%d photos)? Photos will NOT be deleted. [y/N] ", a.Name, a.PhotoCount)
		var answer string
		fmt.Fscanln(os.Stdin, &answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	if err := c.do(ctx, "DELETE", "/api/v1/albums/"+id, nil, nil); err != nil {
		return fmt.Errorf("album delete: %w", err)
	}
	fmt.Printf("Deleted album %q.\n", a.Name)
	return nil
}

func runAlbumAdd(ctx context.Context, c *client, args []string) error {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: photo album add <album-id> <photo-id>")
		os.Exit(1)
	}
	albumID, photoID := args[0], args[1]
	if err := c.do(ctx, "POST", fmt.Sprintf("/api/v1/albums/%s/photos/%s", albumID, photoID), nil, nil); err != nil {
		return fmt.Errorf("album add: %w", err)
	}
	fmt.Printf("Added photo %s to album %s.\n", photoID, albumID)
	return nil
}

func runAlbumRemove(ctx context.Context, c *client, args []string) error {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: photo album remove <album-id> <photo-id>")
		os.Exit(1)
	}
	albumID, photoID := args[0], args[1]
	if err := c.do(ctx, "DELETE", fmt.Sprintf("/api/v1/albums/%s/photos/%s", albumID, photoID), nil, nil); err != nil {
		return fmt.Errorf("album remove: %w", err)
	}
	fmt.Printf("Removed photo %s from album %s.\n", photoID, albumID)
	return nil
}

func runAlbumMove(ctx context.Context, c *client, args []string) error {
	// Usage: album move <album-id> <photo-id> after <other-id>
	//        album move <album-id> <photo-id> first
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: photo album move <album-id> <photo-id> after <other-photo-id>")
		fmt.Fprintln(os.Stderr, "       photo album move <album-id> <photo-id> first")
		os.Exit(1)
	}

	albumID, photoID, position := args[0], args[1], args[2]

	var afterPhotoID string
	if position == "after" {
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: photo album move <album-id> <photo-id> after <other-photo-id>")
			os.Exit(1)
		}
		afterPhotoID = args[3]
	} else if position != "first" {
		fmt.Fprintf(os.Stderr, "unknown position %q; use 'after <photo-id>' or 'first'\n", position)
		os.Exit(1)
	}

	body := map[string]string{"afterPhotoId": afterPhotoID}
	if err := c.do(ctx, "POST",
		fmt.Sprintf("/api/v1/albums/%s/photos/%s/move", albumID, photoID),
		body, nil,
	); err != nil {
		return fmt.Errorf("album move: %w", err)
	}
	fmt.Printf("Moved photo %s.\n", photoID)
	return nil
}

func runAlbumCover(ctx context.Context, c *client, args []string) error {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: photo album cover <album-id> <photo-id>")
		os.Exit(1)
	}
	albumID, photoID := args[0], args[1]
	if err := c.do(ctx, "PUT",
		fmt.Sprintf("/api/v1/albums/%s/cover/%s", albumID, photoID),
		nil, nil,
	); err != nil {
		return fmt.Errorf("album cover: %w", err)
	}
	fmt.Printf("Set cover photo for album %s.\n", albumID)
	return nil
}

// Command photo is the CLI client for the photo library server (photod).
//
// Usage:
//
//	photo <command> [arguments]
//
// Commands:
//
//	register   Create a new account (invite token required after first user)
//	login      Authenticate and store a session token
//	logout     Invalidate the current session token
//	add        Import a file or directory into the library
//	delete     Delete a photo from the library
//	update     Update photo description
//	regeocode  Set or update a photo's location
//	list       List all photos (alias for search)
//	search     Search photos by date, location, or tag
//	tag        Attach a tag to a photo
//	untag      Remove a tag from a photo
//	show       Display details for a single photo
//	admin      Manage invites (admin only)
//	backup     Download a database backup (admin only)
//	config     View or update client configuration
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	cmd, rest := args[0], args[1:]

	// config, register, and login work without a stored token.
	switch cmd {
	case "config":
		return runConfig(rest)
	case "register":
		return runRegister(ctx, rest)
	case "login":
		return runLogin(ctx, rest)
	}

	// All other commands require a config with a stored token.
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("%w\nRun 'photo config set server <url>' then 'photo login'", err)
	}

	client := newClient(cfg.ServerURL, cfg.Token)

	var cmdErr error
	switch cmd {
	case "logout":
		cmdErr = runLogout(ctx, client, cfg)
	case "status":
		cmdErr = runStatus(ctx, client, rest)
	case "add":
		cmdErr = runAdd(ctx, client, cfg, rest)
	case "publish":
		cmdErr = runPublish(ctx, client, cfg, rest)
	case "delete":
		cmdErr = runDelete(ctx, client, rest)
	case "update":
		cmdErr = runUpdate(ctx, client, rest)
	case "share":
		cmdErr = runShare(ctx, client, rest)
	case "regeocode":
		cmdErr = runRegeocode(ctx, client, rest)
	case "album":
		cmdErr = runAlbum(ctx, client, rest)
	case "search", "list":
		cmdErr = runSearch(ctx, client, rest)
	case "tag":
		cmdErr = runTag(ctx, client, rest)
	case "untag":
		cmdErr = runUntag(ctx, client, rest)
	case "show":
		cmdErr = runShow(ctx, client, rest)
	case "admin":
		cmdErr = runAdmin(ctx, client, rest)
	case "backup":
		cmdErr = runBackup(ctx, client, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", cmd)
		printUsage()
		os.Exit(1)
		return nil
	}

	if cmdErr == errUnauthorized {
		// Clear the stale token so the user isn't stuck.
		cfg.Token = ""
		cfg.Username = ""
		_ = saveConfig(cfg)
		return fmt.Errorf("session expired — run 'photo login' to authenticate")
	}
	return cmdErr
}

func printUsage() {
	fmt.Fprint(os.Stderr, `photo — photo library CLI client

Usage:
  photo <command> [arguments]

Commands:
  register   Create a new account on the server
  login      Authenticate and store a session token
  logout     Invalidate the current session
  status     Show library statistics
  add        Import a file or directory (originals untouched)
  publish    Upload a publish directory, deriving tags from path
  delete     Delete a photo from the library
  update     Update photo description
  regeocode  Set or update a photo's location
  album      Manage albums (list, create, show, delete, add, remove, move, cover)
  list       List all photos (alias for search)
  search     Search photos by date, location, or tag
  tag        Attach a tag to a photo
  untag      Remove a tag from a photo
  show       Display full details for a photo
  admin      Manage invites (admin only)
  backup     Download a database backup (admin only)
  config     View or update client configuration

Run 'photo <command> -help' for command-specific usage.
`)
}

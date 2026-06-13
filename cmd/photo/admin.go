package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

// runAdmin dispatches 'photo admin <subcommand>'.
func runAdmin(ctx context.Context, c *client, args []string) error {
	if len(args) == 0 {
		printAdminUsage()
		return nil
	}
	sub, rest := args[0], args[1:]

	switch sub {
	case "invite-create":
		return runInviteCreate(ctx, c, rest)
	case "invite-list":
		return runInviteList(ctx, c, rest)
	case "invite-revoke":
		return runInviteRevoke(ctx, c, rest)
	case "status":
		return runAdminStatusCmd(ctx, c, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown admin command: %q\n\n", sub)
		printAdminUsage()
		os.Exit(1)
		return nil
	}
}

func printAdminUsage() {
	fmt.Fprint(os.Stderr, `Usage: photo admin <subcommand>

Subcommands:
  invite-create   Generate a new invite token for registration
  invite-list     List all invite tokens and their status
  invite-revoke   Revoke an unused invite token
  status          Display system-wide library statistics (all users)

Run 'photo admin <subcommand> -help' for subcommand-specific usage.
`)
}

// runAdminStatusCmd handles 'photo admin status'.
func runAdminStatusCmd(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo admin status

Display system-wide library statistics across all users. Admin only.
`)
	}
	fs.Parse(args) //nolint:errcheck

	st, err := c.getAdminStatus(ctx)
	if err != nil {
		return fmt.Errorf("admin status: %w", err)
	}
	return printStatus(st)
}

// runInviteCreate handles 'photo admin invite-create'.
func runInviteCreate(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("invite-create", flag.ExitOnError)
	ttl := fs.Int("ttl-hours", 168, "invite validity period in hours (default 7 days)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo admin invite-create [-ttl-hours N]

Generate a new single-use invite token. Share the registration link with
the person you're inviting — they open it in a browser and fill out a form.

`)
	}
	fs.Parse(args) //nolint:errcheck

	inv, err := c.inviteCreate(ctx, *ttl)
	if err != nil {
		return fmt.Errorf("create invite: %w", err)
	}

	fmt.Printf("Invite token: %s\n", inv.Token)
	fmt.Printf("Expires:      %s\n", inv.ExpiresAt)
	fmt.Println()
	fmt.Println("Share this registration link with the person you're inviting:")
	fmt.Printf("  %s/register?token=%s\n", c.baseURL, inv.Token)
	fmt.Println()
	fmt.Println("Or, for command-line registration:")
	fmt.Printf("  photo register %s\n", inv.Token)
	return nil
}

// runInviteList handles 'photo admin invite-list'.
func runInviteList(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("invite-list", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo admin invite-list

List all invite tokens, showing creation date, expiry, and usage status.

`)
	}
	fs.Parse(args) //nolint:errcheck

	invites, err := c.inviteList(ctx)
	if err != nil {
		return fmt.Errorf("list invites: %w", err)
	}

	if len(invites) == 0 {
		fmt.Println("No invites found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Token\tCreated\tExpires\tStatus")
	fmt.Fprintln(w, "─────\t───────\t───────\t──────")
	for _, inv := range invites {
		status := "unused"
		if inv.UsedAt != nil {
			status = "used"
		} else if expired(inv.ExpiresAt) {
			status = "expired"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			inv.Token, formatDate(inv.CreatedAt), formatDate(inv.ExpiresAt), status,
		)
	}
	w.Flush()
	return nil
}

// runInviteRevoke handles 'photo admin invite-revoke <token>'.
func runInviteRevoke(ctx context.Context, c *client, args []string) error {
	fs := flag.NewFlagSet("invite-revoke", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo admin invite-revoke <token>

Revoke an unused invite token. Already-used invites cannot be revoked.

`)
	}
	fs.Parse(args) //nolint:errcheck

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(1)
	}

	token := fs.Arg(0)
	if err := c.inviteRevoke(ctx, token); err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	fmt.Printf("Revoked invite %s.\n", token)
	return nil
}

// --- helpers -----------------------------------------------------------------

func expired(rfc3339 string) bool {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return false
	}
	return time.Now().After(t)
}

func formatDate(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Format("2006-01-02 15:04")
}

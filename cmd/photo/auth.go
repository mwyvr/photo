package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// runRegister handles 'photo register'.
func runRegister(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo register

Create a new account on the photod server.
You will be prompted for username, email, and password.
A session token is stored in the config file on success.

`)
	}
	fs.Parse(args) //nolint:errcheck

	cfg := loadOrEmptyConfig()
	if cfg.ServerURL == "" {
		return fmt.Errorf("server URL not set; run 'photo config set server <url>' first")
	}

	username, err := prompt("Username: ")
	if err != nil {
		return err
	}
	email, err := prompt("Email: ")
	if err != nil {
		return err
	}
	password, err := promptPassword("Password (min 8 chars): ")
	if err != nil {
		return err
	}
	confirm, err := promptPassword("Confirm password: ")
	if err != nil {
		return err
	}
	if password != confirm {
		return fmt.Errorf("passwords do not match")
	}

	client := newClient(cfg.ServerURL, "")
	resp, err := client.register(ctx, username, email, password)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	cfg.Token = resp.Token
	cfg.Username = resp.User.Username
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Registered and logged in as %s.\n", resp.User.Username)
	return nil
}

// runLogin handles 'photo login'.
func runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo login

Authenticate with the photod server.
The session token is stored in the config file.

`)
	}
	fs.Parse(args) //nolint:errcheck

	cfg := loadOrEmptyConfig()
	if cfg.ServerURL == "" {
		return fmt.Errorf("server URL not set; run 'photo config set server <url>' first")
	}

	username, err := prompt("Username: ")
	if err != nil {
		return err
	}
	password, err := promptPassword("Password: ")
	if err != nil {
		return err
	}

	client := newClient(cfg.ServerURL, "")
	resp, err := client.login(ctx, username, password)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	cfg.Token = resp.Token
	cfg.Username = resp.User.Username
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("Logged in as %s.\n", resp.User.Username)
	return nil
}

// runLogout handles 'photo logout'.
func runLogout(ctx context.Context, c *client, cfg *Config) error {
	if err := c.logout(ctx); err != nil {
		// Log the error but clear the token locally regardless.
		fmt.Fprintf(os.Stderr, "warning: server logout failed: %v\n", err)
	}

	cfg.Token = ""
	cfg.Username = ""
	if err := saveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Println("Logged out.")
	return nil
}

// --- prompt helpers ---------------------------------------------------------

func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	var s string
	if _, err := fmt.Fscanln(os.Stdin, &s); err != nil {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(s), nil
}

// promptPassword reads a password without echoing it to the terminal.
// Falls back to plain Fscanln if the terminal isn't available (e.g. in tests).
func promptPassword(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)

	// term.ReadPassword suppresses echo and works on Linux and macOS.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr) // newline after hidden input
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return strings.TrimSpace(string(pw)), nil
	}

	// Not a terminal (e.g. piped input in scripts).
	var s string
	if _, err := fmt.Fscanln(os.Stdin, &s); err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(s), nil
}

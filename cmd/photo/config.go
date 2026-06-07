package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
)

// Config holds the CLI client's persisted configuration.
type Config struct {
	ServerURL string `json:"serverURL"`
	Token     string `json:"token,omitempty"`
	Username  string `json:"username,omitempty"`
}

func configPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "photo", "config.json")
	}
	home, _ := os.UserHomeDir()
	macos := filepath.Join(home, "Library", "Application Support")
	if info, err := os.Stat(macos); err == nil && info.IsDir() {
		return filepath.Join(macos, "photo", "config.json")
	}
	return filepath.Join(home, ".config", "photo", "config.json")
}

func loadConfig() (*Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no config found at %s", path)
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("serverURL not set; run 'photo config set server <url>'")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("not logged in; run 'photo login'")
	}
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600) // 0600: token is sensitive
}

func loadOrEmptyConfig() *Config {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return &Config{}
	}
	var cfg Config
	json.Unmarshal(data, &cfg) //nolint:errcheck
	return &cfg
}

func runConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: photo config <subcommand>

Subcommands:
  show              Print current configuration
  set server <url>  Set the photod server URL

`)
	}
	fs.Parse(args) //nolint:errcheck

	switch fs.Arg(0) {
	case "", "show":
		return configShow()
	case "set":
		if fs.NArg() < 3 || fs.Arg(1) != "server" {
			return fmt.Errorf("usage: photo config set server <url>")
		}
		return configSetServer(fs.Arg(2))
	default:
		return fmt.Errorf("unknown config subcommand %q", fs.Arg(0))
	}
}

func configShow() error {
	path := configPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "No configuration found.")
		fmt.Fprintln(os.Stderr, "Run 'photo config set server <url>' to get started.")
		return nil
	}
	var cfg Config
	json.Unmarshal(data, &cfg) //nolint:errcheck

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Config file:\t%s\n", path)
	fmt.Fprintf(w, "Server URL:\t%s\n", orDash(cfg.ServerURL))
	fmt.Fprintf(w, "Logged in as:\t%s\n", orDash(cfg.Username))
	if cfg.Token != "" {
		n := len(cfg.Token)
		if n > 16 {
			n = 16
		}
		fmt.Fprintf(w, "Token:\t%s…\n", cfg.Token[:n])
	} else {
		fmt.Fprintf(w, "Token:\t—\n")
	}
	return w.Flush()
}

func configSetServer(serverURL string) error {
	cfg := loadOrEmptyConfig()
	cfg.ServerURL = serverURL
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Server URL set to: %s\n", serverURL)
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Command photod is the photo library HTTP API server.
//
// Usage:
//
//	photod [--config <path>] [--addr <host:port>]
//
// On first run, photod generates a JWT secret and writes a config file.
// The config file is created at the XDG config path if --config is not given.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	photohttp "github.com/mwyvr/photo/http"
	htmlui "github.com/mwyvr/photo/http/html"
	"github.com/mwyvr/photo/exif"
	"github.com/mwyvr/photo/geocode"
	"github.com/mwyvr/photo/importer"
	"github.com/mwyvr/photo/sqlite"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		log.Fatalf("photod: %v", err)
	}
}

func run(ctx context.Context) error {
	var cfgPath, addr string
	flag.StringVar(&cfgPath, "config", "", "path to config file (default: platform config dir)")
	flag.StringVar(&addr, "addr", "", "listen address (overrides config)")
	flag.Parse()

	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}

	cfg, err := loadOrInitConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if addr != "" {
		cfg.Addr = addr
	}

	// Open database.
	db := sqlite.NewDB(cfg.DBPath())
	if err := db.Open(); err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Build services.
	userSvc := sqlite.NewUserService(db)
	sessionSvc := sqlite.NewSessionService(db)
	photoSvc := sqlite.NewPhotoService(db)
	tagSvc := sqlite.NewTagService(db)
	statusSvc := sqlite.NewStatusService(db)
	backupSvc := sqlite.NewBackupService(db)
	inviteSvc := sqlite.NewInviteService(db)
	albumSvc := sqlite.NewAlbumService(db)

	// Populate slugs for any albums created before migration 0006.
	if err := albumSvc.MigrateExistingSlugs(ctx); err != nil {
		return fmt.Errorf("migrate album slugs: %w", err)
	}

	// Build EXIF extractor.
	extractor := exif.NewExtractor("")
	if err := extractor.CheckDependency(); err != nil {
		return fmt.Errorf("exiftool: %w", err)
	}

	// Build geocoder.
	geocoder := geocode.NewNominatimGeocoder(
		fmt.Sprintf("photo-manager/%s (github.com/mwyvr/photo)", version()),
	)

	// Build importer.
	imp := importer.New(photoSvc, extractor, geocoder, cfg.LibraryRoot)

	// Wire and start HTTP server.
	srv := photohttp.New(cfg.Addr)
	srv.UserService = userSvc
	srv.SessionService = sessionSvc
	srv.PhotoService = photoSvc
	srv.TagService = tagSvc
	srv.StatusService = statusSvc
	srv.BackupService = backupSvc
	srv.InviteService = inviteSvc
	srv.AlbumService = albumSvc
	srv.Importer = imp
	srv.Geocoder = geocoder
	srv.JWTSecret = cfg.JWTSecret
	srv.LibraryRoot = cfg.LibraryRoot
	srv.PublishDefault = cfg.PublishDefault
	srv.HouseholdMode = cfg.HouseholdMode
	srv.TrustedProxy = cfg.TrustedProxy

	// Build and register HTML UI.
	ui, err := htmlui.New()
	if err != nil {
		return fmt.Errorf("html ui: %w", err)
	}
	ui.PhotoService = photoSvc
	ui.Importer = imp
	ui.PublishDefault = cfg.PublishDefault
	ui.AlbumService = albumSvc
	ui.SessionService = sessionSvc
	ui.UserService = userSvc
	ui.StatusService = statusSvc
	ui.BackupService = backupSvc
	ui.InviteService = inviteSvc
	ui.JWTSecret = cfg.JWTSecret
	ui.LibraryRoot = cfg.LibraryRoot
	ui.TrustedProxy = cfg.TrustedProxy
	ui.PublicBaseURL = cfg.PublicBaseURL
	ui.HouseholdMode = cfg.HouseholdMode
	ui.RegisterRoutes(srv.Router())

	log.Printf("photod config:          %s", cfgPath)
	log.Printf("photod library:         %s", cfg.LibraryRoot)
	log.Printf("photod database:        %s", cfg.DBPath())
	log.Printf("photod publish default: %v (RAW always false)", cfg.PublishDefault)
	log.Printf("photod household mode:  %v", cfg.HouseholdMode)
	if cfg.PublicBaseURL != "" {
		log.Printf("photod public base URL: %s (RSS feed enabled at /feed.xml)", cfg.PublicBaseURL)
	} else {
		log.Printf("photod public base URL: (not set; RSS feed disabled)")
	}
	log.Printf("photod web UI:          http://%s/", cfg.Addr)
	log.Printf("photod API:             http://%s/api/v1/", cfg.Addr)
	return srv.ListenAndServe(ctx)
}

// --- config -----------------------------------------------------------------

// ServerConfig holds photod's persisted configuration.
type ServerConfig struct {
	// Addr is the listen address, e.g. "127.0.0.1:4040".
	Addr string `json:"addr"`

	// LibraryRoot is the directory where imported photos are stored.
	LibraryRoot string `json:"libraryRoot"`

	// JWTSecret is a hex-encoded 32-byte random secret used to sign JWTs.
	// Generated once on first run and never changes.
	JWTSecretHex string `json:"jwtSecret"`

	// PublishDefault controls the default public visibility of uploaded photos.
	// When true, non-RAW photos are marked published on import unless explicitly
	// overridden. RAW files always default to unpublished regardless of this setting.
	// Defaults to false (private) on first run.
	PublishDefault bool `json:"publishDefault"`

	// TrustedProxy is the IP address of the reverse proxy (Mox, Caddy, etc.).
	// When set, X-Forwarded-For is only trusted when the request comes from
	// this address. Prevents clients from forging their IP for rate limit bypass.
	// Use "127.0.0.1" when running behind a local proxy.
	// Leave empty to trust X-Forwarded-For from any source (less secure).
	TrustedProxy string `json:"trustedProxy,omitempty"`

	// PublicBaseURL is the externally-visible base URL of this server,
	// e.g. "https://photos.yourdomain.com" (no trailing slash). Used to
	// build absolute links in the RSS feed. If empty, the feed is disabled.
	PublicBaseURL string `json:"publicBaseUrl,omitempty"`

	// HouseholdMode, when true, makes household-visibility photos visible
	// to all authenticated users — suitable for couples or small groups
	// sharing a library. Newly uploaded photos default to household visibility.
	// Defaults to true for new installs.
	HouseholdMode bool `json:"householdMode"`

	// JWTSecret is the decoded form of JWTSecretHex. Not persisted.
	JWTSecret []byte `json:"-"`
}

// DBPath returns the path to the SQLite database file.
func (c *ServerConfig) DBPath() string {
	return filepath.Join(c.LibraryRoot, ".photo", "library.db")
}

// loadOrInitConfig reads the config file at path, creating it with defaults
// if it does not yet exist.
func loadOrInitConfig(path string) (*ServerConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return initConfig(path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg ServerConfig
	if err := jsonUnmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	secret, err := hex.DecodeString(cfg.JWTSecretHex)
	if err != nil || len(secret) != 32 {
		return nil, fmt.Errorf("invalid jwtSecret in config; delete %s to regenerate", path)
	}
	cfg.JWTSecret = secret

	if cfg.LibraryRoot == "" {
		return nil, fmt.Errorf("libraryRoot not set in %s", path)
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:4040"
	}

	// Ensure directories exist even if the config predates them or they were removed.
	if err := os.MkdirAll(filepath.Join(cfg.LibraryRoot, ".photo"), 0o755); err != nil {
		return nil, fmt.Errorf("create library directory: %w", err)
	}

	return &cfg, nil
}

// initConfig creates a new config file with generated defaults.
func initConfig(path string) (*ServerConfig, error) {
	home, _ := os.UserHomeDir()
	libraryRoot := filepath.Join(home, "Photos")

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate JWT secret: %w", err)
	}

	cfg := &ServerConfig{
		Addr:          "127.0.0.1:4040",
		LibraryRoot:   libraryRoot,
		JWTSecretHex:  hex.EncodeToString(secret),
		JWTSecret:     secret,
		HouseholdMode: true,
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(libraryRoot, ".photo"), 0o755); err != nil {
		return nil, fmt.Errorf("create library dir: %w", err)
	}

	data, err := jsonMarshalIndent(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write config %s: %w", path, err)
	}

	log.Printf("photod: created config at %s", path)
	log.Printf("photod: library root: %s", libraryRoot)
	log.Printf("photod: JWT secret generated and stored")
	return cfg, nil
}

// defaultConfigPath returns the platform-appropriate config file path.
func defaultConfigPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "photod", "config.json")
	}
	home, _ := os.UserHomeDir()
	// macOS
	macos := filepath.Join(home, "Library", "Application Support")
	if info, err := os.Stat(macos); err == nil && info.IsDir() {
		return filepath.Join(macos, "photod", "config.json")
	}
	// Linux
	return filepath.Join(home, ".config", "photod", "config.json")
}

func version() string {
	if Version != "" {
		return Version
	}
	return "dev"
}

// Version and Commit are set at build time via -ldflags.
var (
	Version string
	Commit  string
)

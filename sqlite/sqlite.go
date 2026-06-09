// Package sqlite implements the photo domain interfaces using SQLite.
// All primary keys are TEXT columns storing 16-character kid IDs.
// kid.ID implements driver.Valuer and sql.Scanner so it can be used
// directly in Exec and Scan calls without any wrapper.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/mwyvr/photo"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB wraps the sql.DB connection and carries shared configuration.
type DB struct {
	DSN string
	Now func() time.Time
	db  *sql.DB
}

// NewDB returns a new DB for the given DSN. Call Open() before using it.
func NewDB(dsn string) *DB {
	return &DB{DSN: dsn, Now: time.Now}
}

// Open opens the connection, configures pragmas, and runs pending migrations.
func (db *DB) Open() (err error) {
	if db.DSN == "" {
		return fmt.Errorf("sqlite: DSN is required")
	}

	// Append busy_timeout to the DSN so SQLite waits up to 5s for a write
	// lock rather than immediately returning SQLITE_BUSY.
	dsn := db.DSN
	if dsn != ":memory:" {
		if strings.Contains(dsn, "?") {
			dsn += "&_busy_timeout=5000"
		} else {
			dsn += "?_busy_timeout=5000"
		}
	}

	if db.db, err = sql.Open("sqlite3", dsn); err != nil {
		return fmt.Errorf("sqlite: open %q: %w", db.DSN, err)
	}

	// Limit to one open connection so concurrent goroutines queue rather
	// than racing for the write lock. Reads use WAL and are unaffected.
	db.db.SetMaxOpenConns(1)

	if _, err = db.db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("sqlite: enable WAL: %w", err)
	}
	if _, err = db.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("sqlite: enable foreign keys: %w", err)
	}
	if _, err = db.db.Exec(`PRAGMA synchronous = NORMAL`); err != nil {
		return fmt.Errorf("sqlite: set synchronous: %w", err)
	}
	return db.migrate()
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.db != nil {
		return db.db.Close()
	}
	return nil
}

// BeginTx starts a transaction. The returned Tx captures the current time
// so all writes within a transaction share one consistent timestamp.
func (db *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := db.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{
		Tx:  tx,
		db:  db,
		now: db.Now().UTC().Truncate(time.Second),
	}, nil
}

func (db *DB) migrate() error {
	if _, err := db.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		name := entry.Name()
		var count int
		if err := db.db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name,
		).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}
		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err = db.db.Exec(string(data)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err = db.db.Exec(
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
			name, time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// Tx wraps sql.Tx with a fixed timestamp for the duration of the transaction.
type Tx struct {
	*sql.Tx
	db  *DB
	now time.Time
}

// nowStr returns the transaction's timestamp as an RFC3339 string.
func (tx *Tx) nowStr() string {
	return tx.now.Format(time.RFC3339)
}

// NullTime handles nullable DATETIME columns.
type NullTime time.Time

func (n *NullTime) Scan(value interface{}) error {
	if value == nil {
		*n = NullTime(time.Time{})
		return nil
	}
	switch v := value.(type) {
	case string:
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return fmt.Errorf("NullTime: parse %q: %w", v, err)
		}
		*n = NullTime(t)
	case time.Time:
		*n = NullTime(v)
	default:
		return fmt.Errorf("NullTime: unsupported type %T", value)
	}
	return nil
}

func (n *NullTime) Value() (driver.Value, error) {
	t := time.Time(*n)
	if t.IsZero() {
		return nil, nil
	}
	return t.UTC().Format(time.RFC3339), nil
}

func (n *NullTime) Time() time.Time { return time.Time(*n) }

// FormatLimitOffset builds a SQL LIMIT/OFFSET clause.
func FormatLimitOffset(limit, offset int) string {
	if limit > 0 && offset > 0 {
		return fmt.Sprintf("LIMIT %d OFFSET %d", limit, offset)
	} else if limit > 0 {
		return fmt.Sprintf("LIMIT %d", limit)
	} else if offset > 0 {
		return fmt.Sprintf("OFFSET %d", offset)
	}
	return ""
}

// FormatError maps SQLite constraint errors to application errors.
func FormatError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return photo.Errorf(photo.ECONFLICT, "resource already exists")
	}
	return err
}

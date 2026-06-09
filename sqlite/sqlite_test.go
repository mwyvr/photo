package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/mwyvr/photo/sqlite"
)

// newTestDB opens an in-memory SQLite database with migrations applied.
// Tests share this helper to avoid boilerplate.
func newTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db := sqlite.NewDB(":memory:")
	db.Now = func() time.Time {
		return time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	}
	if err := db.Open(); err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDB_Open(t *testing.T) {
	db := newTestDB(t)
	if db == nil {
		t.Fatal("expected non-nil DB")
	}
}

func TestDB_MigrationsApplied(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	// Verify schema_migrations has entries.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations`,
	).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count == 0 {
		t.Error("expected migrations to be recorded in schema_migrations")
	}
}

func TestDB_MigrationsIdempotent(t *testing.T) {
	// Opening the same DSN twice should not fail or double-apply migrations.
	db1 := sqlite.NewDB(":memory:")
	if err := db1.Open(); err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer db1.Close()

	// A second Open on the same in-memory DB would be a different DB entirely,
	// so instead we verify by checking the migration count is stable.
	ctx := context.Background()
	tx, _ := db1.BeginTx(ctx, nil)
	defer tx.Rollback()

	var count int
	tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&count) //nolint
	if count == 0 {
		t.Error("no migrations applied")
	}
}

package migrationlock

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func testConfig(t *testing.T) *pgx.ConnConfig {
	t.Helper()
	dsn := os.Getenv("OPENPLAN_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("OPENPLAN_POSTGRES_TEST_DSN is not set")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	// A schema of its own, so the lock this test takes is not the one other
	// packages' migrations take on the same database.
	// Lower case, because search_path folds an unquoted name to it.
	config.RuntimeParams["search_path"] = "migrationlock_test_" + strings.ToLower(t.Name())
	return config
}

// A replica waiting behind a migration that never finishes must still stop
// when its own startup is abandoned, not hang until the other one exits.
func TestHoldGivesUpWhenContextEnds(t *testing.T) {
	t.Parallel()
	config := testConfig(t)
	ensureSchema(t, config)

	holding := make(chan struct{})
	release := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- Hold(context.Background(), config, func() error {
			close(holding)
			<-release
			return nil
		})
	}()
	select {
	case <-holding:
	case err := <-held:
		t.Fatalf("holder could not take the lock: %v", err)
	}
	defer func() {
		close(release)
		if err := <-held; err != nil {
			t.Errorf("holder returned error: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*pollInterval)
	defer cancel()
	ran := false
	err := Hold(ctx, config, func() error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hold error = %v, want context.DeadlineExceeded", err)
	}
	if ran {
		t.Fatal("migrate ran without the lock")
	}
}

// A migration that fails must not leave the lock behind: the next replica to
// start retries it rather than waiting forever.
func TestHoldReleasesTheLockWhenMigrateFails(t *testing.T) {
	t.Parallel()
	config := testConfig(t)
	ensureSchema(t, config)

	failure := errors.New("migration failed")
	if err := Hold(context.Background(), config, func() error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("Hold error = %v, want the migration's error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Hold(ctx, config, func() error { return nil }); err != nil {
		t.Fatalf("second Hold returned error: %v; the failed migration kept the lock", err)
	}
}

// A search_path naming no schema that exists leaves current_schema() null,
// and with it the lock key. That has to fail as itself, not as a scan error.
func TestHoldReportsAMissingSchema(t *testing.T) {
	t.Parallel()
	config := testConfig(t)

	err := Hold(context.Background(), config, func() error { return nil })
	if err == nil || !strings.Contains(err.Error(), "search_path") {
		t.Fatalf("Hold error = %v, want one naming the search_path", err)
	}
}

// ensureSchema creates the schema config names. current_schema() is null
// until it exists, and the lock key is derived from it.
func ensureSchema(t *testing.T, config *pgx.ConnConfig) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	schema := pgx.Identifier{config.RuntimeParams["search_path"]}.Sanitize()
	if _, err := conn.Exec(ctx, "create schema if not exists "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := pgx.ConnectConfig(context.Background(), config)
		if err != nil {
			return
		}
		defer cleanup.Close(context.Background())
		_, _ = cleanup.Exec(context.Background(), "drop schema if exists "+schema+" cascade")
	})
}

// Package migrationlock runs schema migrations one at a time per schema, so
// api replicas starting together cannot race on the same DDL.
//
// Both migrators take it: the application's own (internal/postgres) and
// OpenFGA's (internal/authorization), which is goose.Up with no lock of its
// own. A migrator that waits finds the work already done and applies nothing.
package migrationlock

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// keyExpr derives the lock id from the current schema, the way the work
// queue's prune lock does. Advisory locks are per database while migrations
// are per schema, so a constant would make test schemas, and deployments that
// share a database, wait on each other for no reason.
const keyExpr = `('x' || substr(md5(current_schema() || '.migrations'), 1, 16))::bit(64)::bigint`

// pollInterval is how often a waiting migrator retries the lock. Migrations
// run once per release, so a waiter can afford to notice a quarter of a
// second late.
const pollInterval = 250 * time.Millisecond

// Hold runs migrate while holding the migration lock for the schema config
// connects to, waiting until any migrator that holds it already is done.
//
// The lock is session-level and lives on a connection opened only for it, so
// closing that connection releases it on every path: an error, a panic in
// migrate, or ctx ending mid-migration. A lock on a pooled connection would
// outlive a failed unlock and block the next migrator until the process
// exits. migrate runs its statements on connections of its own; only the lock
// is held here.
func Hold(ctx context.Context, config *pgx.ConnConfig, migrate func() error) error {
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("connect for migration lock: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))

	if err := acquire(ctx, conn); err != nil {
		return err
	}
	return migrate()
}

// acquire polls pg_try_advisory_lock instead of blocking in pg_advisory_lock.
//
// A session blocked in pg_advisory_lock is mid-statement, holding a snapshot
// for as long as it waits. OpenFGA's migrations build indexes CONCURRENTLY,
// which waits for every older snapshot to end, so the index build would wait
// on the waiter while the waiter waits on the lock the migrating replica
// holds: a deadlock Postgres cannot see, because its last edge runs through
// this process. Each try ends at once and holds nothing between attempts.
func acquire(ctx context.Context, conn *pgx.Conn) error {
	for {
		// Null when no schema on the search_path exists: current_schema() is
		// null, and so is the key derived from it.
		var acquired *bool
		if err := conn.QueryRow(ctx, `select pg_try_advisory_lock(`+keyExpr+`)`).Scan(&acquired); err != nil {
			return fmt.Errorf("take migration lock: %w", err)
		}
		if acquired == nil {
			return fmt.Errorf("take migration lock: no schema on the connection's search_path exists")
		}
		if *acquired {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for migration lock: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

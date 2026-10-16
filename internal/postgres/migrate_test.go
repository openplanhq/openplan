package postgres

import (
	"context"
	"path"
	"testing"
)

// Two api replicas starting together both find every migration unapplied.
// Without one migrator at a time the second one's DDL collides with the
// first's: concurrent `create table if not exists` races on the catalog, and a
// migration both apply fails on whichever commits second.
func TestMigrateIsSafeToRunConcurrently(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openTestPool(t, ctx)

	const migrators = 8
	start := make(chan struct{})
	errs := make(chan error, migrators)
	for range migrators {
		go func() {
			<-start
			errs <- Migrate(ctx, pool)
		}()
	}
	close(start)
	for range migrators {
		if err := <-errs; err != nil {
			t.Errorf("concurrent Migrate returned error: %v", err)
		}
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	want := 0
	for _, entry := range entries {
		if !entry.IsDir() && path.Ext(entry.Name()) == ".sql" {
			want++
		}
	}
	var recorded int
	if err := pool.QueryRow(ctx, `select count(*) from schema_migrations`).Scan(&recorded); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if recorded != want {
		t.Fatalf("schema_migrations has %d rows, want %d: one per migration, each applied once", recorded, want)
	}
}

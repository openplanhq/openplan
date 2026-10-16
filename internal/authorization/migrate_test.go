package authorization_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/authorization"
)

// OpenFGA's runner is goose.Up with no lock of its own, so two api replicas
// starting against a fresh database would each try to create goose's version
// table and apply the same migrations.
func TestMigrateIsSafeToRunConcurrently(t *testing.T) {
	t.Parallel()

	dsn := emptyTestDatabase(t)
	const migrators = 4
	start := make(chan struct{})
	errs := make(chan error, migrators)
	for range migrators {
		go func() {
			<-start
			errs <- authorization.Migrate(context.Background(), dsn)
		}()
	}
	close(start)
	for range migrators {
		require.NoError(t, <-errs)
	}

	// The schema New requires is there, so every migrator finished the job
	// rather than one finishing and the rest bailing out early.
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	auth, err := authorization.New(context.Background(), pool, "concurrent-migrate")
	require.NoError(t, err)
	auth.Close()
}

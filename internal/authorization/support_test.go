package authorization_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/authorization"
)

// testPool gates the Postgres tests: without a database there is nothing worth
// asserting, because the deliverable is what Postgres does at commit time.
//
// Migrating here rather than in newTestDatastore is where the DSN is: Migrate
// takes a URL, not a pool, because OpenFGA's runner opens its own connection.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("OPENPLAN_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set OPENPLAN_POSTGRES_TEST_DSN (or run `make differential-test`)")
	}
	require.NoError(t, authorization.Migrate(context.Background(), dsn))
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// newTestStoreID keeps runs from colliding: these tests write real rows and
// never clean up, so a fixed id fails the second run with a duplicate tuple.
func newTestStoreID(t *testing.T) string {
	return "test-" + randomHex(t, 16)
}

// newTestStoreName is newTestStoreID for paths that resolve a store by name. A
// fixed name would leave one store holding several models across runs, which
// bootstrap then refuses as ambiguous.
func newTestStoreName(t *testing.T) string {
	return "test-" + randomHex(t, 8)
}

func randomHex(t *testing.T, size int) string {
	t.Helper()
	raw := make([]byte, size)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return hex.EncodeToString(raw)
}

func newTestDatastore(t *testing.T, pool *pgxpool.Pool) *authorization.Datastore {
	t.Helper()
	store, err := authorization.NewDatastoreForTest(pool)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	return store
}

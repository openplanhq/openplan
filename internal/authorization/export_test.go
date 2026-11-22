package authorization

import (
	"github.com/jackc/pgx/v5/pgxpool"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
)

// NewDatastoreForTest exposes the transaction-aware datastore on its own, so
// the storage tests can exercise Write's two paths without booting a server.
func NewDatastoreForTest(pool *pgxpool.Pool) (*Datastore, error) {
	return newDatastore(pool)
}

// DesiredModelForTest exposes the repository's model as bootstrap compares
// it, so a test can plant a stored model that matches it.
func DesiredModelForTest() (*openfgav1.AuthorizationModel, error) {
	return desiredModel()
}

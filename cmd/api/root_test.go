package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/memory"

	"github.com/vishu42/openplan/internal/authorization"
)

// A fresh install is administrable before it serves: the root tuple for the
// configured identity-provider subject exists by the time the server starts.
func TestRunSeedsTheRootTupleForTheConfiguredSubject(t *testing.T) {
	t.Parallel()

	deps := newRecordingAPIDependencies(t)
	if err := runWithDependencies(context.Background(), apiTestEnv, deps.apiDependencies); err != nil {
		t.Fatalf("runWithDependencies returned error: %v", err)
	}

	// Asserted through the engine rather than a record of the call, so what is
	// checked is the state the model evaluates.
	held, err := deps.authorizer.Can(context.Background(), apiTestEnv("OPENPLAN_ROOT_SUBJECT"),
		authorization.RelationRoot, authorization.Platform)
	if err != nil {
		t.Fatalf("Can() error = %v", err)
	}
	if !held {
		t.Fatal("the root relationship was not seeded for OPENPLAN_ROOT_SUBJECT")
	}
}

// Fail closed: an install that cannot be administered must not serve. Every
// route would answer 403 and nothing would say why.
func TestRunRefusesToStartWhenRootSeedingFails(t *testing.T) {
	t.Parallel()

	deps := newRecordingAPIDependencies(t)
	datastore := &failingWrites{OpenFGADatastore: memory.New()}
	auth, err := authorization.NewWithDatastore(context.Background(), datastore, "openplan-test")
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	t.Cleanup(auth.Close)
	deps.authorizer = auth
	datastore.failing = true

	err = runWithDependencies(context.Background(), apiTestEnv, deps.apiDependencies)
	if err == nil {
		t.Fatal("runWithDependencies served despite failing to seed root")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Fatalf("error = %v, want it to name the root seeding failure", err)
	}
	if deps.serverHandler != nil {
		t.Fatal("the server was started before root was seeded")
	}
}

// failingWrites refuses every tuple write once flipped, after the engine has
// resolved its store and model.
type failingWrites struct {
	storage.OpenFGADatastore
	failing bool
}

func (d *failingWrites) Write(ctx context.Context, store string, deletes storage.Deletes, writes storage.Writes, opts ...storage.TupleWriteOption) error {
	if d.failing {
		return errors.New("connection refused")
	}
	return d.OpenFGADatastore.Write(ctx, store, deletes, writes, opts...)
}

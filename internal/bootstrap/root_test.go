package bootstrap

import (
	"context"
	"errors"
	"testing"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/openfga/openfga/pkg/storage"
	"github.com/openfga/openfga/pkg/storage/memory"

	"github.com/vishu42/openplan/internal/authorization"
)

// A Dex sub: base64url(protobuf{user_id, conn_id}). Opaque, and a valid tuple
// token.
const testRootSubject = "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs"

// newAuthorization runs a real engine over memory. Seeding is what these tests
// vary, rather than a fake's boolean: SeedRoot is add-only, so what matters is
// whether the root relationship is already present.
func newAuthorization(t *testing.T) *authorization.Authorization {
	t.Helper()
	auth, err := authorization.NewWithDatastore(context.Background(), memory.New(), "openplan-test")
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	t.Cleanup(auth.Close)
	return auth
}

// failAfterBootstrap wraps a working datastore and starts failing every tuple
// read and write once bootstrap has finished.
//
// The store and the model must be resolvable for the Authorization to exist at
// all, so the failure cannot be present from the start. Flipping it afterwards
// is how a provider outage during seeding is simulated -- and SeedRoot must
// fail closed on one rather than carry on.
type failAfterBootstrap struct {
	storage.OpenFGADatastore
	failing bool
	err     error
}

func (d *failAfterBootstrap) ReadUserTuple(ctx context.Context, store string, filter storage.ReadUserTupleFilter, options storage.ReadUserTupleOptions) (*openfgav1.Tuple, error) {
	if d.failing {
		return nil, d.err
	}
	return d.OpenFGADatastore.ReadUserTuple(ctx, store, filter, options)
}

func (d *failAfterBootstrap) ReadUsersetTuples(ctx context.Context, store string, filter storage.ReadUsersetTuplesFilter, options storage.ReadUsersetTuplesOptions) (storage.TupleIterator, error) {
	if d.failing {
		return nil, d.err
	}
	return d.OpenFGADatastore.ReadUsersetTuples(ctx, store, filter, options)
}

func (d *failAfterBootstrap) ReadStartingWithUser(ctx context.Context, store string, filter storage.ReadStartingWithUserFilter, options storage.ReadStartingWithUserOptions) (storage.TupleIterator, error) {
	if d.failing {
		return nil, d.err
	}
	return d.OpenFGADatastore.ReadStartingWithUser(ctx, store, filter, options)
}

func (d *failAfterBootstrap) Write(ctx context.Context, store string, deletes storage.Deletes, writes storage.Writes, opts ...storage.TupleWriteOption) error {
	if d.failing {
		return d.err
	}
	return d.OpenFGADatastore.Write(ctx, store, deletes, writes, opts...)
}

var errOutage = errors.New("connection refused")

// newFailingAuthorization boots normally and then fails every tuple operation.
func newFailingAuthorization(t *testing.T, err error) *authorization.Authorization {
	t.Helper()
	datastore := &failAfterBootstrap{OpenFGADatastore: memory.New(), err: err}
	auth, err2 := authorization.NewWithDatastore(context.Background(), datastore, "openplan-test")
	if err2 != nil {
		t.Fatalf("build authorization: %v", err2)
	}
	t.Cleanup(auth.Close)
	datastore.failing = true
	return auth
}

// seedRootRelationship writes the root relationship, so a test can start from a
// store where it already stands.
func seedRootRelationship(t *testing.T, auth *authorization.Authorization, sub string) *authorization.Authorization {
	t.Helper()
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatalf("SubjectFromOIDCSub: %v", err)
	}
	relationship, err := authorization.NewStructuralRelationship(subject, authorization.Platform, authorization.RelationRoot)
	if err != nil {
		t.Fatalf("NewStructuralRelationship: %v", err)
	}
	if err := auth.Grant(context.Background(), relationship); err != nil {
		t.Fatalf("seed root relationship: %v", err)
	}
	return auth
}

// rootIsSeeded reports whether the root relationship exists on the platform.
func rootIsSeeded(t *testing.T, auth *authorization.Authorization, sub string) bool {
	t.Helper()
	held, err := auth.Can(context.Background(), sub, authorization.RelationRoot, authorization.Platform)
	if err != nil {
		t.Fatalf("check root relationship: %v", err)
	}
	return held
}

func TestSeedRootWritesTheRootTuple(t *testing.T) {
	authorizer := newAuthorization(t)

	if err := SeedRoot(context.Background(), authorizer, testRootSubject); err != nil {
		t.Fatalf("SeedRoot returned error: %v", err)
	}

	// The relationship is asserted through the engine rather than through a
	// record of the call, so what is checked is the state the model evaluates.
	if !rootIsSeeded(t, authorizer, testRootSubject) {
		t.Fatalf("root relationship missing for %s", testRootSubject)
	}
	// root is structural rather than grantable, which is what keeps the grant
	// API from writing it. NewGrant must keep refusing it.
	rootSubject, err := authorization.SubjectFromOIDCSub(testRootSubject)
	if err != nil {
		t.Fatalf("SubjectFromOIDCSub: %v", err)
	}
	if _, err := authorization.NewGrant(rootSubject, authorization.Platform, authorization.RelationRoot); err == nil {
		t.Fatal("NewGrant(root) succeeded; the grant API must not be able to write the root relationship")
	}
}

// The relationship is checked for before it is written, which is what makes the
// reconcile add-only rather than a write that happens to be idempotent.
//
// Seeding twice is the assertion: OpenFGA rejects a write of a tuple that
// already exists, so a second run that wrote unconditionally would fail.
func TestSeedRootChecksTheTupleBeforeWriting(t *testing.T) {
	authorizer := newAuthorization(t)

	for attempt := 1; attempt <= 2; attempt++ {
		if err := SeedRoot(context.Background(), authorizer, testRootSubject); err != nil {
			t.Fatalf("SeedRoot attempt %d returned error: %v", attempt, err)
		}
	}
	if !rootIsSeeded(t, authorizer, testRootSubject) {
		t.Fatal("root relationship missing after two seeds")
	}
}

// Add-only: changing OPENPLAN_ROOT_SUBJECT adds a root and leaves the old one
// standing. Removing a root is a deliberate act, not a side effect of a boot.
func TestSeedRootLeavesAPreviousRootInPlace(t *testing.T) {
	const previous = "previous-root"
	authorizer := seedRootRelationship(t, newAuthorization(t), previous)

	if err := SeedRoot(context.Background(), authorizer, testRootSubject); err != nil {
		t.Fatalf("SeedRoot returned error: %v", err)
	}
	if !rootIsSeeded(t, authorizer, previous) {
		t.Fatal("seeding a new root removed the previous one")
	}
	if !rootIsSeeded(t, authorizer, testRootSubject) {
		t.Fatal("the configured root was not seeded")
	}
}

// Fail closed. Running with no reachable administrator is the worse failure,
// so a seeding error stops the boot rather than being logged past.
func TestSeedRootFailsClosed(t *testing.T) {
	err := SeedRoot(context.Background(), newFailingAuthorization(t, errOutage), testRootSubject)
	if err == nil {
		t.Fatal("SeedRoot succeeded with authorization unreachable")
	}
}

// The sub becomes user:<sub> in a tuple, and authorization refuses ':', '#'
// and '*' there. A sub that cannot be an OpenFGA subject would seed nothing
// usable, so it stops the boot instead.
func TestSeedRootRejectsASubjectThatCannotBeATupleToken(t *testing.T) {
	for _, subject := range []string{"", "local:root", "local#root", "local*root", "local root"} {
		if err := SeedRoot(context.Background(), newAuthorization(t), subject); err == nil {
			t.Fatalf("SeedRoot accepted the unusable subject %q", subject)
		}
	}
}

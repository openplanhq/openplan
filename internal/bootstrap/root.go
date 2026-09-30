// Package bootstrap holds the reconciliation a fresh install needs before it
// can be administered at all.
package bootstrap

import (
	"context"
	"fmt"

	"github.com/vishu42/openplan/internal/authorization"
)

// SeedRoot makes a fresh install administrable: it writes the
// {user:<sub>, root, platform:openplan} tuple for the identity provider's sub
// configured as root.
//
// Root is not a bypass and not a special code path. It is an identity-provider
// user plus an ordinary tuple, so every authorization question about it is
// still answered by OpenFGA. openplan holds no credential for it: whoever the
// provider signs in with that sub is root. What makes it root is the
// relation, and `root` sits outside the grantable set
// (internal/authorization/relations.go), so the grant API cannot revoke it —
// stated honestly rather than offered as a delete the next boot would
// silently undo.
//
// Add-only. A changed subject adds a root and leaves the previous one in
// place: removing a root is a deliberate act, not a side effect of a restart.
//
// Every failure stops the boot. Running with no reachable administrator is the
// worse outcome, and it is silent: every route answers 403 and nothing says why.
func SeedRoot(ctx context.Context, auth *authorization.Authorization, subject string) error {
	rootSubject, err := authorization.SubjectFromOIDCSub(subject)
	if err != nil {
		return fmt.Errorf("seed root: %w", err)
	}

	// Checked before written so the reconcile is genuinely add-only rather
	// than a write that happens to be idempotent — and so a boot against a
	// store that already has it does no write at all.
	//
	// Seeding runs at startup, outside any unit of work, so this takes the
	// datastore's non-transactional path. That path exists for exactly this.
	held, err := auth.Can(ctx, rootSubject.ID(), authorization.RelationRoot, authorization.Platform)
	if err != nil {
		return fmt.Errorf("seed root: check root relationship: %w", err)
	}
	if held {
		return nil
	}

	// NewStructuralRelationship rather than NewGrant: `root` is not
	// grantable, and that refusal is the only thing standing between the
	// grant API and this tuple, so seeding goes through the separate door
	// instead of the refusal being relaxed.
	relationship, err := authorization.NewStructuralRelationship(rootSubject, authorization.Platform, authorization.RelationRoot)
	if err != nil {
		return fmt.Errorf("seed root: build root relationship: %w", err)
	}
	if err := auth.Grant(ctx, relationship); err != nil {
		return fmt.Errorf("seed root: write root relationship: %w", err)
	}
	return nil
}

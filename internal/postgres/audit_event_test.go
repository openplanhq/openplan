package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/openfga/openfga/pkg/storage/memory"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

// A refused write has to be able to say what was attempted, whichever path it
// is written through: refusals go through the store, approvals through a
// transaction.
func TestAppendAuditEventStoresWhatWasAttempted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openMigratedTestPool(t, ctx)
	store := NewStore(pool)

	event := domain.SecurityAuditEvent{
		ActorSubject: "user:actor",
		Action:       domain.AuditActionFailedAccessAttempt,
		TenantID:     "tenant_123",
		Outcome:      domain.AuditOutcomeFailure,
		Operation:    domain.AuditOperationApproveRun,
		Reason:       domain.AuditReasonForbidden,
		TargetID:     "run_123",
	}

	writes := map[string]func() error{
		"store": func() error { return store.AppendAuditEvent(ctx, event) },
		"transaction": func() error {
			return store.InTx(ctx, func(ctx context.Context, repository app.TxRepo, _ queue.Enqueuer) error {
				return repository.AppendAuditEvent(ctx, event)
			})
		},
	}
	for name, write := range writes {
		if err := write(); err != nil {
			t.Fatalf("%s: AppendAuditEvent returned error: %v", name, err)
		}
		var operation, reason, targetID string
		if err := pool.QueryRow(ctx, `
			SELECT operation, reason, target_id
			FROM security_audit_log ORDER BY id DESC LIMIT 1
		`).Scan(&operation, &reason, &targetID); err != nil {
			t.Fatalf("%s: read audit event: %v", name, err)
		}
		if operation != "approve_run" || reason != "forbidden" || targetID != "run_123" {
			t.Fatalf("%s: operation, reason, target_id = %q, %q, %q", name, operation, reason, targetID)
		}
	}
}

// A last-owner rejection fails its transaction, which rolls back everything in it,
// so only an event written outside it survives. A fake unit of work can't show
// that: this runs the real rollback.
func TestLastOwnerRejectionSurvivesTheRollback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, pool := newQueueStore(t, ctx)
	auth, err := authorization.NewWithDatastore(ctx, memory.New(), "openplan-test")
	if err != nil {
		t.Fatalf("build authorization: %v", err)
	}
	t.Cleanup(auth.Close)

	const sub, stackID = "owner_sub", "stack_last_owner"
	subject, err := authorization.SubjectFromOIDCSub(sub)
	if err != nil {
		t.Fatal(err)
	}
	object, err := authorization.ObjectFromID(authorization.TypeStack, stackID)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := authorization.NewGrant(subject, object, authorization.RelationOwner)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Grant(ctx, grant); err != nil {
		t.Fatalf("grant owner: %v", err)
	}

	service := app.NewService(app.Service{Authorization: auth, Work: store, Audit: store})
	err = service.RevokeStackRole(
		authentication.ContextWithPrincipal(ctx, authentication.Principal{Subject: sub}),
		app.RevokeStackRoleCommand{TenantID: "tenant_123", StackID: stackID, UserSub: sub},
	)
	if !errors.Is(err, app.ErrLastOwner) {
		t.Fatalf("error = %v, want ErrLastOwner", err)
	}

	grants, err := auth.ListGrants(ctx, object)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 || grants[0].Relation() != authorization.RelationOwner {
		t.Fatalf("grants = %+v, want the owner still there", grants)
	}
	var rejections, others int
	if err := pool.QueryRow(ctx, `select
		count(*) filter (where reason = 'last_owner' and outcome = 'failure'),
		count(*) filter (where reason <> 'last_owner')
		from security_audit_log where stack_id = $1`, stackID).Scan(&rejections, &others); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if rejections != 1 || others != 0 {
		t.Fatalf("rejections = %d, others = %d, want 1 and 0", rejections, others)
	}
}

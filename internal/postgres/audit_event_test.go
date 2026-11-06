package postgres

import (
	"context"
	"testing"

	"github.com/vishu42/openplan/internal/app"
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

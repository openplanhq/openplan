package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/vishu42/openplan/internal/domain"
)

// recordPlanChanges stores a finished plan's resource changes, replacing any a
// retried FinishPlan stored before, and records what the list is on the run.
// Nil changes records that the plan's changes could not be listed.
func recordPlanChanges(ctx context.Context, tx pgx.Tx, tenantID domain.TenantID, runID domain.TemplateRunID, changes *domain.PlanChanges) error {
	if _, err := tx.Exec(ctx, `
		delete from template_run_resource_changes where tenant_id = $1 and run_id = $2
	`, tenantID, runID); err != nil {
		return fmt.Errorf("clear plan changes: %w", err)
	}

	if changes == nil {
		if _, err := tx.Exec(ctx, `
			update template_runs set plan_changes_state = $3, plan_changes_total = null
			where tenant_id = $1 and id = $2
		`, tenantID, runID, domain.PlanChangesUnavailable); err != nil {
			return fmt.Errorf("record plan changes unavailable: %w", err)
		}
		return nil
	}

	rows := make([][]any, len(changes.Changes))
	for i, change := range changes.Changes {
		rows[i] = []any{
			tenantID, runID, i,
			change.Address, change.PreviousAddress, change.ModuleAddress,
			change.Mode, change.Type, change.Name, change.ProviderName, change.Deposed,
			string(change.Action), change.CreateBeforeDestroy, change.ActionReason, change.ImportingID,
			jsonbValue(change.ReplacePaths), jsonbValue(change.Before), jsonbValue(change.After),
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"template_run_resource_changes"}, []string{
		"tenant_id", "run_id", "position",
		"address", "previous_address", "module_address",
		"mode", "type", "name", "provider_name", "deposed",
		"action", "create_before_destroy", "action_reason", "importing_id",
		"replace_paths", "before", "after",
	}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("record plan changes: %w", err)
	}

	state := domain.PlanChangesComplete
	if changes.Truncated {
		state = domain.PlanChangesTruncated
	}
	if _, err := tx.Exec(ctx, `
		update template_runs set plan_changes_state = $3, plan_changes_total = $4
		where tenant_id = $1 and id = $2
	`, tenantID, runID, state, changes.Total); err != nil {
		return fmt.Errorf("record plan changes state: %w", err)
	}
	return nil
}

// jsonbValue is a value for a jsonb column: SQL null when there is none,
// otherwise the JSON text as it is.
func jsonbValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

// ListTemplateRunResourceChangesPage returns up to limit of a run's resource
// changes in reading order, starting after position afterPosition; -1 starts
// at the first.
func (store *Store) ListTemplateRunResourceChangesPage(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, afterPosition int, limit int) ([]domain.PlanResourceChange, error) {
	rows, err := store.pool.Query(ctx, `
		select
			position, address, previous_address, module_address,
			mode, type, name, provider_name, deposed,
			action, create_before_destroy, action_reason, importing_id,
			replace_paths, before, after
		from template_run_resource_changes
		where tenant_id = $1 and run_id = $2 and position > $3
		order by position
		limit $4
	`, tenantID, runID, afterPosition, limit)
	if err != nil {
		return nil, fmt.Errorf("list plan changes: %w", err)
	}
	defer rows.Close()

	changes := []domain.PlanResourceChange{}
	for rows.Next() {
		var change domain.PlanResourceChange
		var action string
		var replacePaths, before, after []byte
		if err := rows.Scan(
			&change.Position, &change.Address, &change.PreviousAddress, &change.ModuleAddress,
			&change.Mode, &change.Type, &change.Name, &change.ProviderName, &change.Deposed,
			&action, &change.CreateBeforeDestroy, &change.ActionReason, &change.ImportingID,
			&replacePaths, &before, &after,
		); err != nil {
			return nil, fmt.Errorf("scan plan change: %w", err)
		}
		change.Action = domain.PlanChangeAction(action)
		change.ReplacePaths = rawOrNil(replacePaths)
		change.Before = rawOrNil(before)
		change.After = rawOrNil(after)
		changes = append(changes, change)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list plan changes: %w", err)
	}
	return changes, nil
}

func rawOrNil(value []byte) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	return json.RawMessage(value)
}

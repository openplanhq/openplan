package app

import (
	"context"
	"fmt"

	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

// PlanChangeRepository reads the resource changes recorded for a run's plan.
type PlanChangeRepository interface {
	// ListTemplateRunResourceChangesPage returns up to limit changes in
	// reading order after position afterPosition; -1 starts at the first.
	ListTemplateRunResourceChangesPage(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, afterPosition int, limit int) ([]domain.PlanResourceChange, error)
}

// ListTemplateRunResourceChangesCommand asks for one page of a run's plan's
// resource changes.
type ListTemplateRunResourceChangesCommand struct {
	TenantID domain.TenantID
	RunID    domain.TemplateRunID
	Page     PageRequest
}

// planChangeCursor is a position in a run's resource changes, which are read
// in the order the run's plan was sorted in.
type planChangeCursor struct {
	Position int `json:"p"`
}

func (cursor planChangeCursor) Valid() bool { return cursor.Position >= 0 }

// planChangesList names the resource changes list's cursors.
const planChangesList = "template-run-changes"

// ListTemplateRunResourceChanges returns a page of the resource changes a
// run's plan makes, removals first, to anyone who can view the run. A run
// whose plan has not finished, or had no changes, has none; the run itself
// says whether its list is complete, cut short or unavailable.
func (service *Service) ListTemplateRunResourceChanges(ctx context.Context, command ListTemplateRunResourceChangesCommand) (Page[domain.PlanResourceChange], error) {
	if err := validateGetTemplateRunCommand(GetTemplateRunCommand{TenantID: command.TenantID, RunID: command.RunID}); err != nil {
		return Page[domain.PlanResourceChange]{}, err
	}
	if err := validatePageRequest(command.Page); err != nil {
		return Page[domain.PlanResourceChange]{}, err
	}
	after, err := DecodeCursor[planChangeCursor](planChangesList, command.Page.Cursor)
	if err != nil {
		return Page[domain.PlanResourceChange]{}, err
	}
	if _, err := service.authorizedTemplateRun(ctx, command.TenantID, command.RunID, authorization.RelationCanView); err != nil {
		return Page[domain.PlanResourceChange]{}, err
	}

	afterPosition := -1
	if after != nil {
		afterPosition = after.Position
	}
	changes, err := service.PlanChanges.ListTemplateRunResourceChangesPage(ctx, command.TenantID, command.RunID, afterPosition, command.Page.Limit+1)
	if err != nil {
		return Page[domain.PlanResourceChange]{}, fmt.Errorf("list template run resource changes: %w", err)
	}
	return pageFrom(changes, command.Page.Limit, planChangesList, func(change domain.PlanResourceChange) cursorKey {
		return planChangeCursor{Position: change.Position}
	}), nil
}

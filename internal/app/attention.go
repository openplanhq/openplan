package app

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/vishu42/openplan/internal/domain"
)

// StackOverviewRepository reads every stack's templates and runs at once, for
// the views that look across a tenant: the stacks index and the attention
// page.
type StackOverviewRepository interface {
	// ListTenantStackTemplates returns every installed template in the
	// tenant that is not destroyed.
	ListTenantStackTemplates(ctx context.Context, tenantID domain.TenantID) ([]domain.StackTemplate, error)
	// ListTemplateRunsByStatus returns the tenant's runs in one status, most
	// recent first.
	ListTemplateRunsByStatus(ctx context.Context, tenantID domain.TenantID, status domain.TemplateRunStatus) ([]domain.TemplateRun, error)
}

// AttentionKind is why an installed template needs a person.
type AttentionKind string

const (
	// AttentionWaitingApproval is a plan with changes waiting for someone to
	// approve or discard it.
	AttentionWaitingApproval AttentionKind = "waiting_approval"
	// AttentionDestroyFailed is a destroy that stopped before it finished, so
	// some of the template's resources may still exist.
	AttentionDestroyFailed AttentionKind = "destroy_failed"
)

// AttentionItem is one installed template that needs a person.
type AttentionItem struct {
	Kind          AttentionKind
	Stack         domain.Stack
	StackTemplate StackTemplateView
	// Run is the plan waiting for approval, or the destroy that failed. Nil
	// for a failed template with no destroy run on record.
	Run *domain.TemplateRun
	// At is when the item began to need a person: when the plan finished, or
	// when the destroy stopped. Zero when neither is known.
	At time.Time
}

// ListAttentionCommand asks for everything that needs a person across the
// stacks the caller can view.
type ListAttentionCommand struct {
	TenantID domain.TenantID
}

// StackSummary is a stack as the stacks index lists it.
type StackSummary struct {
	Stack domain.Stack
	// TemplateCount counts the stack's installed templates that are not
	// destroyed.
	TemplateCount int
}

// ListAttention returns the plans waiting for approval and the failed destroys
// on every stack the caller can view: waiting plans first, then failures,
// each newest first. Viewing is enough; approving is checked where it happens.
func (service *Service) ListAttention(ctx context.Context, command ListAttentionCommand) ([]AttentionItem, error) {
	if command.TenantID == "" {
		return nil, fmt.Errorf("%w: tenant id is required", ErrInvalidCommand)
	}
	stacks, templates, err := service.visibleStackTemplates(ctx, command.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list attention: %w", err)
	}
	items := []AttentionItem{}
	if len(templates) == 0 {
		return items, nil
	}

	waiting, err := service.StackOverview.ListTemplateRunsByStatus(ctx, command.TenantID, domain.TemplateRunWaitingApproval)
	if err != nil {
		return nil, fmt.Errorf("list attention: list waiting runs: %w", err)
	}
	items = append(items, waitingItems(stacks, templates, waiting)...)

	failed, err := service.failedDestroyItems(ctx, command.TenantID, stacks, templates)
	if err != nil {
		return nil, fmt.Errorf("list attention: %w", err)
	}
	items = append(items, failed...)

	views := make([]*StackTemplateView, len(items))
	var runs []*domain.TemplateRun
	for i := range items {
		views[i] = &items[i].StackTemplate
		if items[i].Run != nil {
			runs = append(runs, items[i].Run)
		}
	}
	if err := service.labelStackTemplates(ctx, command.TenantID, views...); err != nil {
		return nil, fmt.Errorf("list attention: %w", err)
	}
	if len(runs) > 0 {
		if err := service.labelTriggerActors(ctx, runs...); err != nil {
			return nil, fmt.Errorf("list attention: %w", err)
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind == AttentionWaitingApproval
		}
		if !items[i].At.Equal(items[j].At) {
			return items[i].At.After(items[j].At)
		}
		return items[i].StackTemplate.ID < items[j].StackTemplate.ID
	})
	return items, nil
}

// waitingItems turns the tenant's waiting-approval runs into attention items,
// skipping any run whose stack template is not among the visible ones.
func waitingItems(
	stacks map[domain.StackID]domain.Stack,
	templates map[domain.StackTemplateID]domain.StackTemplate,
	runs []domain.TemplateRun,
) []AttentionItem {
	items := []AttentionItem{}
	for i := range runs {
		run := runs[i]
		stackTemplate, ok := templates[run.StackTemplateID]
		if !ok {
			continue
		}
		// The component records when the plan was parked, which is when the
		// person started waiting; the run's own creation is earlier.
		at := run.CreatedAt
		if stackTemplate.PendingPlanRunID == run.ID && !stackTemplate.PendingPlanAt.IsZero() {
			at = stackTemplate.PendingPlanAt
		}
		items = append(items, AttentionItem{
			Kind:          AttentionWaitingApproval,
			Stack:         stacks[stackTemplate.StackID],
			StackTemplate: StackTemplateView{StackTemplate: stackTemplate},
			Run:           &run,
			At:            at,
		})
	}
	return items
}

// failedDestroyItems returns one item per visible template whose destroy
// failed, each carrying that template's most recent destroy run when it has
// one.
func (service *Service) failedDestroyItems(
	ctx context.Context,
	tenantID domain.TenantID,
	stacks map[domain.StackID]domain.Stack,
	templates map[domain.StackTemplateID]domain.StackTemplate,
) ([]AttentionItem, error) {
	items := []AttentionItem{}
	for _, stackTemplate := range templates {
		if stackTemplate.Lifecycle != domain.StackTemplateFailed {
			continue
		}
		item := AttentionItem{
			Kind:          AttentionDestroyFailed,
			Stack:         stacks[stackTemplate.StackID],
			StackTemplate: StackTemplateView{StackTemplate: stackTemplate},
		}
		destroy, err := service.latestDestroyRun(ctx, tenantID, stackTemplate.ID)
		if err != nil {
			return nil, err
		}
		if destroy != nil {
			item.Run = destroy
			// A destroy that never completed has no completion time; the
			// run's creation is the closest thing to when it needs attention.
			item.At = destroy.CompletedAt
			if item.At.IsZero() {
				item.At = destroy.CreatedAt
			}
		}
		items = append(items, item)
	}
	return items, nil
}

// ListStackSummaries lists the stacks ListStacks lists, each with how many
// templates it has installed.
func (service *Service) ListStackSummaries(ctx context.Context, command ListStacksCommand) ([]StackSummary, error) {
	stacks, err := service.ListStacks(ctx, command)
	if err != nil {
		return nil, err
	}
	summaries := make([]StackSummary, 0, len(stacks))
	if len(stacks) == 0 {
		return summaries, nil
	}

	templates, err := service.StackOverview.ListTenantStackTemplates(ctx, command.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list stacks: list templates: %w", err)
	}
	counts := make(map[domain.StackID]int, len(stacks))
	for _, stackTemplate := range templates {
		counts[stackTemplate.StackID]++
	}
	for _, stack := range stacks {
		summaries = append(summaries, StackSummary{Stack: stack, TemplateCount: counts[stack.ID]})
	}
	return summaries, nil
}

// visibleStackTemplates returns the stacks the caller can view, by ID, and
// the installed templates on them, by ID.
func (service *Service) visibleStackTemplates(ctx context.Context, tenantID domain.TenantID) (map[domain.StackID]domain.Stack, map[domain.StackTemplateID]domain.StackTemplate, error) {
	stacks, err := listAccessibleStacks(ctx, service.Authorization, service.Stacks, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("list stacks: %w", err)
	}
	visible := make(map[domain.StackID]domain.Stack, len(stacks))
	for _, stack := range stacks {
		visible[stack.ID] = stack
	}
	templates := map[domain.StackTemplateID]domain.StackTemplate{}
	if len(visible) == 0 {
		return visible, templates, nil
	}

	all, err := service.StackOverview.ListTenantStackTemplates(ctx, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("list templates: %w", err)
	}
	for _, stackTemplate := range all {
		if _, ok := visible[stackTemplate.StackID]; ok {
			templates[stackTemplate.ID] = stackTemplate
		}
	}
	return visible, templates, nil
}

// latestDestroyRun returns the stack template's most recent destroy run, or
// nil when it has none.
func (service *Service) latestDestroyRun(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) (*domain.TemplateRun, error) {
	runs, err := service.TemplateRuns.ListTemplateRuns(ctx, tenantID, stackTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list runs of %s: %w", stackTemplateID, err)
	}
	for i := range runs {
		if runs[i].Operation == domain.OperationDestroy {
			return &runs[i], nil
		}
	}
	return nil, nil
}

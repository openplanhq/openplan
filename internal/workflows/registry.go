package workflows

import "github.com/vishu42/openplan/internal/domain"

// Registration is a workflow and the name a worker registers it under, which
// is the name every recorded history of it carries.
type Registration struct {
	Name     string
	Workflow any
}

// Registered lists every workflow the control worker runs. The api registers
// each one from here, and the replay test replays recorded histories of each
// one against the code as it is now, so a workflow cannot be added to one
// without the other.
func Registered() []Registration {
	return []Registration{
		{Name: domain.TemplatePlanWorkflowName, Workflow: TemplatePlanWorkflow},
		{Name: domain.TemplateApplyWorkflowName, Workflow: TemplateApplyWorkflow},
		{Name: domain.TemplateSyncWorkflowName, Workflow: TemplateSyncWorkflow},
	}
}

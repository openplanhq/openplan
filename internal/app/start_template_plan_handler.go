//nolint:dupl // One small handler per queue kind; the siblings differ only in types.
package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

const KindStartTemplatePlan queue.Kind = "start_template_plan"

// StartTemplatePlanPayload is the complete input required to start a run's plan workflow.
// It intentionally has no JSON tags: persisted JSON uses its Go field names.
type StartTemplatePlanPayload domain.TemplateRunWorkflowInput

var StartTemplatePlanSpec = queue.Spec{Kind: KindStartTemplatePlan, Mode: queue.ModeJob, Key: startTemplatePlanKey}

func startTemplatePlanKey(payload json.RawMessage) (string, error) {
	var parsed StartTemplatePlanPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", fmt.Errorf("decode start template plan payload: %w", err)
	}
	if parsed.TenantID == "" || parsed.RunID == "" {
		return "", fmt.Errorf("decode start template plan payload: tenant ID and run ID are required")
	}
	return "run:" + string(parsed.TenantID) + "/" + string(parsed.RunID), nil
}

type StartTemplatePlanHandler struct{ dispatcher WorkflowDispatcher }

func NewStartTemplatePlanHandler(dispatcher WorkflowDispatcher) *StartTemplatePlanHandler {
	return &StartTemplatePlanHandler{dispatcher: dispatcher}
}

func (handler *StartTemplatePlanHandler) Spec() queue.Spec { return StartTemplatePlanSpec }

func (handler *StartTemplatePlanHandler) Deliver(ctx context.Context, item queue.Item) ([]queue.Request, error) {
	var payload StartTemplatePlanPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return nil, fmt.Errorf("decode start template plan payload: %w", err)
	}
	if err := handler.dispatcher.StartTemplatePlan(ctx, domain.TemplateRunWorkflowInput(payload)); err != nil {
		return nil, err
	}
	return nil, nil
}

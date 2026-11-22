package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/queue"
)

const (
	KindStartTemplatePlan  queue.Kind = "start_template_plan"
	KindStartTemplateApply queue.Kind = "start_template_apply"
	KindStartTemplateSync  queue.Kind = "start_template_sync"
)

// The payloads are the complete input of the workflow each kind starts. They
// intentionally have no JSON tags: persisted JSON uses their Go field names.
type (
	// StartTemplatePlanPayload starts a run's plan workflow.
	StartTemplatePlanPayload domain.TemplateRunWorkflowInput
	// StartTemplateApplyPayload starts the workflow that applies an approved
	// run's saved plan.
	StartTemplateApplyPayload domain.TemplateRunWorkflowInput
	// StartTemplateSyncPayload starts a template sync.
	StartTemplateSyncPayload domain.TemplateSyncWorkflowInput
)

// startWorkflowRequest is the queue request that starts kind's workflow for
// actor. Callers build it before their transaction opens, so a payload that
// fails to encode writes nothing.
func startWorkflowRequest(kind queue.Kind, payload any, actor domain.UserID, tenantID domain.TenantID) (queue.Request, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return queue.Request{}, fmt.Errorf("encode %s payload: %w", kind, err)
	}
	return queue.Request{Kind: kind, Payload: encoded, ActorSubject: string(actor), TenantID: string(tenantID)}, nil
}

// The dedupe keys are frozen: queued items are stored under them.
var (
	StartTemplatePlanSpec = startWorkflowSpec(KindStartTemplatePlan, func(payload StartTemplatePlanPayload) (string, string, string) {
		return "run", string(payload.TenantID), string(payload.RunID)
	})
	StartTemplateApplySpec = startWorkflowSpec(KindStartTemplateApply, func(payload StartTemplateApplyPayload) (string, string, string) {
		return "run", string(payload.TenantID), string(payload.RunID)
	})
	StartTemplateSyncSpec = startWorkflowSpec(KindStartTemplateSync, func(payload StartTemplateSyncPayload) (string, string, string) {
		return "sync", string(payload.TenantID), string(payload.SyncID)
	})
)

func NewStartTemplatePlanHandler(dispatcher WorkflowDispatcher) queue.Handler {
	return startWorkflowHandler[StartTemplatePlanPayload]{spec: StartTemplatePlanSpec, start: func(ctx context.Context, payload StartTemplatePlanPayload) error {
		return dispatcher.StartTemplatePlan(ctx, domain.TemplateRunWorkflowInput(payload))
	}}
}

// NewStartTemplateApplyHandler starts the apply workflow for an approved run.
// Starting is idempotent on the workflow ID, so a redelivered intent finds the
// workflow already started rather than starting a second one.
func NewStartTemplateApplyHandler(dispatcher WorkflowDispatcher) queue.Handler {
	return startWorkflowHandler[StartTemplateApplyPayload]{spec: StartTemplateApplySpec, start: func(ctx context.Context, payload StartTemplateApplyPayload) error {
		return dispatcher.StartTemplateApply(ctx, domain.TemplateRunWorkflowInput(payload))
	}}
}

func NewStartTemplateSyncHandler(dispatcher WorkflowDispatcher) queue.Handler {
	return startWorkflowHandler[StartTemplateSyncPayload]{spec: StartTemplateSyncSpec, start: func(ctx context.Context, payload StartTemplateSyncPayload) error {
		return dispatcher.StartTemplateSync(ctx, domain.TemplateSyncWorkflowInput(payload))
	}}
}

// startWorkflowHandler delivers one kind of start-workflow intent: it decodes
// the queued payload and hands it to the dispatcher.
type startWorkflowHandler[Payload any] struct {
	spec  queue.Spec
	start func(context.Context, Payload) error
}

func (handler startWorkflowHandler[Payload]) Spec() queue.Spec { return handler.spec }

func (handler startWorkflowHandler[Payload]) Deliver(ctx context.Context, item queue.Item) ([]queue.Request, error) {
	payload, err := decodeStartPayload[Payload](handler.spec.Kind, item.Payload)
	if err != nil {
		return nil, err
	}
	return nil, handler.start(ctx, payload)
}

// startWorkflowSpec builds a job spec whose dedupe key is
// "<prefix>:<tenant>/<id>", from the parts key names in the payload.
func startWorkflowSpec[Payload any](kind queue.Kind, key func(Payload) (prefix, tenantID, id string)) queue.Spec {
	return queue.Spec{Kind: kind, Mode: queue.ModeJob, Key: func(raw json.RawMessage) (string, error) {
		payload, err := decodeStartPayload[Payload](kind, raw)
		if err != nil {
			return "", err
		}
		prefix, tenantID, id := key(payload)
		if tenantID == "" || id == "" {
			return "", fmt.Errorf("decode %s payload: tenant ID and %s ID are required", describeKind(kind), prefix)
		}
		return prefix + ":" + tenantID + "/" + id, nil
	}}
}

func decodeStartPayload[Payload any](kind queue.Kind, raw json.RawMessage) (Payload, error) {
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("decode %s payload: %w", describeKind(kind), err)
	}
	return payload, nil
}

// describeKind names a kind in an error: start_template_plan → start template plan.
func describeKind(kind queue.Kind) string {
	return strings.ReplaceAll(string(kind), "_", " ")
}

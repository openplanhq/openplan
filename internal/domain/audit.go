package domain

// The security audit trail.

import (
	"time"
)

// AuditAction identifies a security-relevant authorization event.
type AuditAction string

const (
	AuditActionGrant               AuditAction = "grant"
	AuditActionRevoke              AuditAction = "revoke"
	AuditActionFailedAccessAttempt AuditAction = "failed_access_attempt"
	AuditActionApprovalGranted     AuditAction = "approval_granted"
	AuditActionApprovalRejected    AuditAction = "approval_rejected"
)

// AuditOutcome reports whether the audited operation succeeded or failed.
type AuditOutcome string

const (
	AuditOutcomeSuccess AuditOutcome = "success"
	AuditOutcomeFailure AuditOutcome = "failure"
)

// AuditOperation names the write a failure event records an attempt at, so the
// event can say what was tried. Its values are stored, so renaming a use case
// doesn't change them.
type AuditOperation string

const (
	AuditOperationRegisterTemplate            AuditOperation = "register_template"
	AuditOperationStartTemplateSync           AuditOperation = "start_template_sync"
	AuditOperationChangeTemplateRef           AuditOperation = "change_template_ref"
	AuditOperationDeleteTemplate              AuditOperation = "delete_template"
	AuditOperationCreateStack                 AuditOperation = "create_stack"
	AuditOperationAssignPlatformRole          AuditOperation = "assign_platform_role"
	AuditOperationRevokePlatformRole          AuditOperation = "revoke_platform_role"
	AuditOperationAddTemplateToStack          AuditOperation = "add_template_to_stack"
	AuditOperationAssignStackRole             AuditOperation = "assign_stack_role"
	AuditOperationRevokeStackRole             AuditOperation = "revoke_stack_role"
	AuditOperationCreateCredential            AuditOperation = "create_credential" //nolint:gosec // names an operation, holds no secret
	AuditOperationDeleteCredential            AuditOperation = "delete_credential" //nolint:gosec // names an operation, holds no secret
	AuditOperationStartTemplateRun            AuditOperation = "start_template_run"
	AuditOperationUpdateStackTemplateConfig   AuditOperation = "update_stack_template_config"
	AuditOperationChangeStackTemplateRevision AuditOperation = "change_stack_template_revision"
	AuditOperationApproveRun                  AuditOperation = "approve_run"
	AuditOperationDiscardRun                  AuditOperation = "discard_run"
)

// AuditReason is the stable reason a failure event records. Forbidden and not
// found are refusals; the rest are rejections, where permission passed and the
// state of things said no.
type AuditReason string

const (
	AuditReasonForbidden        AuditReason = "forbidden"
	AuditReasonNotFound         AuditReason = "not_found"
	AuditReasonLastOwner        AuditReason = "last_owner"
	AuditReasonRunNotApprovable AuditReason = "run_not_approvable"
	AuditReasonPlanStale        AuditReason = "plan_stale"
)

// SecurityAuditEvent is one append-only authorization audit record. Operation,
// Reason and TargetID are set on failure events only: on a success, Action
// already says what happened.
type SecurityAuditEvent struct {
	ID            int64          `json:"id"`
	RecordedAt    time.Time      `json:"recorded_at"`
	ActorSubject  string         `json:"actor_subject"`
	Action        AuditAction    `json:"action"`
	TargetUser    string         `json:"target_user"`
	TenantID      TenantID       `json:"tenant_id"`
	StackID       StackID        `json:"stack_id"`
	OldRole       string         `json:"old_role"`
	NewRole       string         `json:"new_role"`
	Outcome       AuditOutcome   `json:"outcome"`
	CorrelationID string         `json:"correlation_id"`
	Operation     AuditOperation `json:"operation"`
	Reason        AuditReason    `json:"reason"`
	TargetID      string         `json:"target_id"`
}

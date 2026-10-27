import type { StackCapabilities } from "../auth/types";

// How a template's latest sync reads, as the server derives it: syncing
// until it ends or turns stalled (still running 30 minutes after it was
// requested, never stored); up_to_date when the ref still named a commit the
// template had, synced when it named a new one.
export type TemplateSyncStatus =
  | "syncing"
  | "stalled"
  | "up_to_date"
  | "synced"
  | "invalid"
  | "failed";

export type TemplateRevisionStatus =
  | "pending_validation"
  | "validating"
  | "active"
  | "invalid";

// A run's lifecycle state. What a running run is doing is its step.
export type TemplateRunStatus =
  | "queued"
  | "running"
  | "waiting_approval"
  | "approved"
  | "completed"
  | "failed"
  | "canceled";

// What a run is doing, or, once it ended, what it was doing last. Nothing
// about what the run may do next depends on it.
export type TemplateRunStep =
  | "waiting_for_executor"
  | "preparing_workspace"
  | "fetching_source"
  | "restoring_plan"
  | "initializing"
  | "selecting_workspace"
  | "planning"
  | "saving_plan"
  | "applying";

// "apply" survives only on runs from before saved plans; a run is started as a
// plan or a destroy, and approving its plan is what applies it.
export type Operation = "plan" | "apply" | "destroy";

// Every API error is an RFC 9457 problem document. type is the contract:
// branch on it, never on detail. These mirror the table in
// internal/api/problem.go, and a status that says it all is "about:blank".
const problemBase = "https://openplan.run/problems/";

export const problemTypes = {
  malformedBody: `${problemBase}malformed-body`,
  invalidQuery: `${problemBase}invalid-query`,
  validationFailed: `${problemBase}validation-failed`,
  runInFlight: `${problemBase}run-in-flight`,
  planStale: `${problemBase}plan-stale`,
  runNotApprovable: `${problemBase}run-not-approvable`,
  runNotDiscardable: `${problemBase}run-not-discardable`,
  stackTemplateNotRunnable: `${problemBase}stack-template-not-runnable`,
  stackTemplateNotEditable: `${problemBase}stack-template-not-editable`,
  stackTemplateRevisionNotChangeable: `${problemBase}stack-template-revision-not-changeable`,
  revisionNotInstallable: `${problemBase}revision-not-installable`,
  templateInUse: `${problemBase}template-in-use`,
  duplicateSlug: `${problemBase}duplicate-slug`,
  duplicateStackTemplateKey: `${problemBase}duplicate-stack-template-key`,
  duplicateCredentialName: `${problemBase}duplicate-credential-name`,
  lastOwner: `${problemBase}last-owner`,
  credentialEncryptionUnavailable: `${problemBase}credential-encryption-unavailable`,
  queueUnavailable: `${problemBase}queue-unavailable`
} as const;

// What is wrong with one field; stable codes, like the types.
export type ProblemIssue =
  | "required"
  | "unknown"
  | "null"
  | "not_object"
  | "invalid_format"
  | "invalid"
  | "unsupported"
  | "apply_only"
  | "different_template"
  | "not_signed_in"
  | "too_long"
  | "out_of_range"
  | "duplicate";

export interface ProblemFieldError {
  // A JSON Pointer into the request body, e.g. "#/config/region", or the
  // name of a query parameter; never both.
  pointer?: string;
  parameter?: string;
  issue: ProblemIssue;
  detail: string;
}

export interface ApiProblem {
  type: string;
  title: string;
  status: number;
  detail?: string;
  request_id?: string;
  errors?: ProblemFieldError[];
}

// A template's newest sync. completed_at is left out until it ends.
export interface TemplateLatestSync {
  status: TemplateSyncStatus;
  error_summary: string;
  resolved_commit_sha: string;
  requested_at: string;
  completed_at?: string;
}

// A registered source: one repository, ref and root path. Name, description
// and tags are its latest revision's; before a sync records one, the name is
// the server's fallback and latest_template_revision_id is "".
export interface Template {
  id: string;
  tenant_id: string;
  repo_owner: string;
  repo_name: string;
  source_ref: string;
  root_path: string;
  name: string;
  description: string;
  tags: string[];
  latest_template_revision_id: string;
  // Null until a sync is requested.
  latest_sync: TemplateLatestSync | null;
  created_at: string;
}

export interface TemplateRevision {
  id: string;
  tenant_id: string;
  template_id: string;
  repo_owner: string;
  repo_name: string;
  source_ref: string;
  resolved_commit_sha: string;
  root_path: string;
  name: string;
  description: string;
  tags: string[];
  status: TemplateRevisionStatus;
  created_at: string;
}

export interface TemplateVariable {
  template_revision_id: string;
  name: string;
  type_expression: string;
  description: string;
  required: boolean;
  has_default: boolean;
  sensitive: boolean;
  has_validation: boolean;
}

export interface Stack {
  id: string;
  tenant_id: string;
  name: string;
  slug: string;
  // True when the slug is the one the name gives, as for a stack created
  // without one; screens show the slug only when someone chose it.
  slug_is_derived: boolean;
  tags: Record<string, string>;
  default_credential_ids: string[];
  created_by: string;
  created_at: string;
  // Per-stack authorization booleans resolved by the backend for AUTH-017; see auth/types.ts.
  effective_capabilities: StackCapabilities;
}

// A stack as the stacks index lists it.
export interface StackListItem extends Stack {
  // Installed templates that are not destroyed.
  template_count: number;
}

export interface StackTemplate {
  id: string;
  stack_id: string;
  key: string;
  template_id: string;
  desired_template_revision_id: string;
  last_applied_template_revision_id: string;
  // The desired revision's ref, resolved by the server per request. A label,
  // not stack template state — an installed template does not own a ref.
  source_ref: string;
  workspace_name: string;
  display_name: string;
  config: Record<string, unknown>;
  last_applied_run_id: string;
  last_applied_at?: string;
  pending_plan_run_id: string;
  pending_plan_at?: string;
  plan_state: PlanState;
  live_state: LiveState;
  created_by: string;
  lifecycle: string;
}

// Both states are computed by the server from snapshots the client never sees.
// "matches" on plan_state is the only condition under which an apply runs what
// was reviewed; anything else means the plan describes something other than
// desired state.
export type PlanState = "none" | "stale" | "matches";
export type LiveState = "never" | "differs" | "matches";

export interface StackView {
  stack: Stack;
  templates: StackTemplate[];
}

export interface CredentialMetadata {
  id: string;
  name: string;
  scope: "stack" | "stack_template";
  stack_id?: string;
  stack_template_id?: string;
  /** Whether the value is write-only. The server never returns a sensitive value. */
  sensitive: boolean;
  /** Present only when `sensitive` is false. */
  value?: string;
  created_at: string;
}

/** One new credential. The server treats a missing `sensitive` as true. */
export interface CredentialRequest {
  name: string;
  value: string;
  sensitive: boolean;
}

export interface PlanSummary {
  add: number;
  change: number;
  destroy: number;
}

export type PlanChangesState = "complete" | "truncated" | "unavailable";

export interface PlanChanges {
  state: PlanChangesState;
  // Every change the plan has, listed or not. Null when unavailable.
  total: number | null;
}

// What a plan does to one resource. The API passes an action it doesn't name
// through as tofu's own words, so any string can arrive.
export type PlanChangeAction = "create" | "update" | "replace" | "destroy" | "forget" | "import" | "move" | (string & {});

// One resource a run's plan changes. before and after are its values with
// sensitive ones already "(sensitive)" and unknown ones "(known after apply)".
export interface PlanResourceChange {
  address: string;
  previous_address: string;
  module_address: string;
  mode: string;
  type: string;
  name: string;
  provider_name: string;
  deposed: string;
  action: PlanChangeAction;
  create_before_destroy: boolean;
  action_reason: string;
  importing_id: string;
  replace_paths: (string | number)[][] | null;
  before: unknown;
  after: unknown;
}

export interface TemplateRun {
  id: string;
  tenant_id: string;
  stack_template_id: string;
  template_revision_id: string;
  operation: Operation;
  source_ref: string;
  resolved_commit_sha: string;
  status: TemplateRunStatus;
  // The latest step of the workflow that started last. Empty until that
  // workflow starts a step, so also between an apply's claim and its first
  // step, and for an apply that failed before its claim.
  step: TemplateRunStep | "";
  trigger_actor: string;
  // Who started the run, for people to read: their name, or trigger_actor
  // itself when openplan has no name for that subject.
  trigger_actor_display_name: string;
  created_at: string;
  completed_at?: string;
  error_summary: string;
  // What the failed command printed, its first error block: "" when the run
  // did not fail in a command. error_summary still wraps it.
  command_error: string;
  // Counts runs within one stack template, from 1. Shown as "Run #N".
  run_number: number;
  // An apply run that applies straight away, with no saved plan and no
  // approval.
  auto_approve: boolean;
  // What the plan would change, or, for an auto-approved apply, what it
  // changed. Null until a plan with changes, or the apply, finishes.
  plan_summary: PlanSummary | null;
  // What the run's list of resource changes is, once a plan with changes has
  // finished: every change, the first of total, or none although the plan
  // had changes (the plan log has them). Null otherwise.
  plan_changes: PlanChanges | null;
  // The workflows that carried the run, in the order they started: its plan,
  // its apply, or one of them. Only the run's own endpoint returns them.
  executions?: TemplateRunExecution[];
}

export type RunPhase = "plan" | "apply";

// One workflow that carried a run. The fields the UI reads; the API returns
// its status, steps and summary too.
export interface TemplateRunExecution {
  phase: RunPhase;
  // Who started it: the requester for a plan or an auto-approved apply, the
  // approver for an approved apply.
  actor: string;
  actor_display_name: string;
  started_at: string;
}

// Why an installed template needs a person: a plan waiting to be approved or
// discarded, a destroy that stopped before it finished, or a plan or apply
// that failed and is still the template's latest run (its run's operation
// says which; any newer run clears it).
export type AttentionKind = "waiting_approval" | "destroy_failed" | "run_failed";

export interface AttentionItem {
  kind: AttentionKind;
  // When it began to need a person: when the plan finished, or when the
  // destroy, plan or apply stopped. Absent when unknown.
  at?: string;
  stack: { id: string; name: string; slug: string };
  stack_template: { id: string; workspace_name: string; display_name: string };
  // The plan waiting for approval, the destroy that failed, or the plan or
  // apply that failed. Null for a failed template with no destroy run on
  // record.
  run: TemplateRun | null;
}

export interface TemplateRunLog {
  tenant_id: string;
  run_id: string;
  // The log's name, unique within the run: the key it is read under, not a
  // label. Show command_line under its run_phase instead.
  name: string;
  run_phase: RunPhase;
  // Its place among the phase's logs, from 1. The list arrives in run order.
  seq: number;
  command: string;
  // What ran, for people to read: "tofu init", "tofu plan -destroy".
  command_line: string;
  // A log has a row from the moment its command starts: live while it runs,
  // then complete once its output is stored, or lost if it never will be.
  state: TemplateRunLogState;
  // Present only when state is "lost".
  lost_reason?: TemplateRunLogLostReason;
  // Present only when state is "complete": the API omits them otherwise, and
  // omits size_bytes for an empty log, so read it as `?? 0`.
  content_type?: string;
  size_bytes?: number;
  uploaded_at?: string;
}

export type TemplateRunLogState = "live" | "complete" | "lost";

export type TemplateRunLogLostReason = "upload_failed" | "executor_gone" | "run_terminated" | "record_failed";

export interface GrantView {
  user_sub: string;
  role: string;
  display_name: string;
  email: string;
}


// A row of the local identity projection: what the user's ID token asserted at
// their last sign-in. `sub` is the only stable key and the value a grant is
// written against; the other two are display data.
export interface UserProfile {
  sub: string;
  display_name: string;
  email: string;
}

// Every list endpoint answers this. A paged list sends next_cursor until its
// last page, where it is null; send it back as ?cursor= for the next page.
// The small lists (a run's logs, a stack's grants and credentials, attention,
// a revision's variables) are one page, with next_cursor always null.
export interface ListPage<T> {
  items: T[];
  next_cursor: string | null;
}

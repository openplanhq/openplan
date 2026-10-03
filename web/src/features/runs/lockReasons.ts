import { isTerminalRunStatus } from "../../api/polling";
import { useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import { tenantID } from "../../config";

/** What decides whether a template's controls can be used now. */
export interface LockState {
  canOperate: boolean;
  lifecycle: string;
  /** Where the template's runs query is. */
  runs: "pending" | "error" | "success";
  /** The template's unfinished run, once its runs have loaded. */
  activeRun: TemplateRun | null;
}

// A run snapshots desired state when it starts, so until it finishes or is
// discarded, nothing may change the config or the revision, and no other run
// may start. The server refuses; the controls say why before anyone tries.
export function useLockState(stackId: string, stackTemplate: Pick<StackTemplate, "id" | "lifecycle">): LockState {
  const canOperate = useStackCapabilities(stackId)?.canOperate === true;
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  const activeRun = runsQuery.status === "success" ? runsQuery.data.find((run) => !isTerminalRunStatus(run.status)) ?? null : null;
  return { canOperate, lifecycle: stackTemplate.lifecycle, runs: runsQuery.status, activeRun };
}

const DESTROYING = "Destroy in progress.";
const DESTROY_FAILED = "A template whose destroy failed cannot start runs.";
const RUNS_LOADING = "Loading runs…";
const RUNS_FAILED = "This template's runs could not be loaded.";

/** The sentence a control locked by a run shows. */
export function runInFlightReason(run: TemplateRun, change: string): string {
  const action = run.status === "waiting_approval" ? "Apply or discard" : "Wait for";
  return `${action} run #${run.run_number} before ${change}.`;
}

// Each function below returns why its control cannot be used now, or "" when
// it can. The order is the same everywhere: who you are, then what the
// template is, then whether its runs are known, then the run in flight.

/** Plan and Apply. */
export function startRunLockReason(state: LockState): string {
  if (!state.canOperate) return "Starting a run requires operator access.";
  if (state.lifecycle === "destroying") return DESTROYING;
  if (state.lifecycle === "failed") return DESTROY_FAILED;
  if (state.lifecycle !== "active") return "Only an active template can start runs.";
  if (state.runs === "pending") return RUNS_LOADING;
  if (state.runs === "error") return RUNS_FAILED;
  if (state.activeRun) return runInFlightReason(state.activeRun, "starting another run");
  return "";
}

/** The sentence beside Plan and Apply: why they are locked, else what they do. */
export function runActionsNote(state: LockState): string {
  return startRunLockReason(state) || "Plan shows what would change. Apply saves a plan that waits for approval.";
}

/**
 * Why a template in this lifecycle cannot change its config or its revision,
 * or "" when it can. The server refuses both for any template that is not
 * active.
 */
export function lifecycleLockReason(lifecycle: string, change: "config" | "revision"): string {
  const what = change === "config" ? "its config" : "revision";
  if (lifecycle === "active") return "";
  if (lifecycle === "destroying") return DESTROYING;
  if (lifecycle === "failed") return `A template whose destroy failed cannot change ${what}.`;
  return `Only an active template can change ${what}.`;
}

/** Save variables, and the fields above it. */
export function variablesLockReason(state: LockState): string {
  if (!state.canOperate) return "Editing requires operator access.";
  const lifecycleReason = lifecycleLockReason(state.lifecycle, "config");
  if (lifecycleReason) return lifecycleReason;
  if (state.activeRun) return runInFlightReason(state.activeRun, "changing the config");
  return "";
}

/** Change revision. */
export function revisionLockReason(state: LockState): string {
  if (!state.canOperate) return "Changing the revision requires operator access.";
  const lifecycleReason = lifecycleLockReason(state.lifecycle, "revision");
  if (lifecycleReason) return lifecycleReason;
  if (state.activeRun) return runInFlightReason(state.activeRun, "changing the revision");
  return "";
}

/** Destroy, which starts a run, so it waits for the runs like Plan does. */
export function destroyLockReason(state: LockState): string {
  if (!state.canOperate) return "Destroying requires operator access.";
  if (state.lifecycle === "destroying") return DESTROYING;
  if (state.lifecycle === "failed") return DESTROY_FAILED;
  if (state.lifecycle !== "active") return "Only an active template can be destroyed.";
  if (state.runs === "pending") return RUNS_LOADING;
  if (state.runs === "error") return RUNS_FAILED;
  if (state.activeRun) return runInFlightReason(state.activeRun, "destroying");
  return "";
}

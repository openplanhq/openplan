import { isTerminalRunStatus } from "../../api/polling";
import { useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import { useTenantID } from "../../auth/useTenantID";

/** What decides whether a template's controls can be used now. */
interface LockState {
  canOperate: boolean;
  lifecycle: string;
  /** Whether the template's runs are known. */
  runs: "pending" | "error" | "success";
  /** The template's unfinished run, once its runs have loaded. */
  activeRun: TemplateRun | null;
}

// A run snapshots desired state when it starts, so until it finishes or is
// discarded, nothing may change the config or the revision, and no other run
// may start. The server refuses; the controls say why before anyone tries.
export function useLockState(stackId: string, stackTemplate: Pick<StackTemplate, "id" | "lifecycle">): LockState {
  const tenantID = useTenantID();
  const canOperate = useStackCapabilities(stackId)?.can_operate === true;
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  // A failed refetch keeps the runs it had, so a run known to be in flight
  // still locks, and the runs still count as known.
  const activeRun = runsQuery.data?.find((run) => !isTerminalRunStatus(run.status)) ?? null;
  return { canOperate, lifecycle: stackTemplate.lifecycle, runs: runsQuery.data ? "success" : runsQuery.status, activeRun };
}

const RUNS_LOADING = "Loading runs…";
const RUNS_FAILED = "This template's runs could not be loaded.";
// On the Runs tab the history under the toolbar already says the runs failed
// and offers Retry, so the toolbar says only what that means for its buttons.
const RUNS_FAILED_TOOLBAR = "Plan and Apply wait until the runs load.";

/** The sentence a control locked by a run shows. */
function runInFlightReason(run: TemplateRun, change: string): string {
  const action = run.status === "waiting_approval" ? "Apply or discard" : "Wait for";
  return `${action} run #${run.run_number} before ${change}.`;
}

/**
 * Why a template in this lifecycle cannot make this change, or "" when it
 * can. The server refuses every one for any template that is not active.
 */
export function lifecycleLockReason(lifecycle: string, change: "run" | "destroy" | "config" | "revision"): string {
  // A destroy is a run, so a failed destroy refuses it as it refuses any run.
  const what = { run: "start runs", destroy: "start runs", config: "change its config", revision: "change revision" }[change];
  if (lifecycle === "active") return "";
  if (lifecycle === "destroying") return "Destroy in progress.";
  if (lifecycle === "failed") return `A template whose destroy failed cannot ${what}.`;
  return `Only an active template can ${change === "destroy" ? "be destroyed" : what}.`;
}

// What sets one control's lock apart; lockReason runs the same check for all.
interface LockedControl {
  access: string;
  change: Parameters<typeof lifecycleLockReason>[1];
  /** Ends runInFlightReason's sentence: "… before <inFlight>." */
  inFlight: string;
  /** Only the controls that start a run wait for the runs to be known; this
      is what they say when the runs could not be loaded. */
  runsFailed?: string;
}

// Why a control cannot be used now, or "" when it can. One function so the
// order holds everywhere: who you are, then what the template is, then
// whether its runs are known, then the run in flight.
function lockReason(state: LockState, control: LockedControl): string {
  if (!state.canOperate) return control.access;
  const lifecycleReason = lifecycleLockReason(state.lifecycle, control.change);
  if (lifecycleReason) return lifecycleReason;
  if (control.runsFailed && state.runs === "pending") return RUNS_LOADING;
  if (control.runsFailed && state.runs === "error") return control.runsFailed;
  if (state.activeRun) return runInFlightReason(state.activeRun, control.inFlight);
  return "";
}

/** Plan and Apply. */
export function startRunLockReason(state: LockState): string {
  return lockReason(state, { access: "Starting a run requires operator access.", change: "run", inFlight: "starting another run", runsFailed: RUNS_FAILED_TOOLBAR });
}

/** Save variables, and the fields above it. */
export function variablesLockReason(state: LockState): string {
  return lockReason(state, { access: "Editing requires operator access.", change: "config", inFlight: "changing the config" });
}

/** Change revision. */
export function revisionLockReason(state: LockState): string {
  return lockReason(state, { access: "Changing the revision requires operator access.", change: "revision", inFlight: "changing the revision" });
}

/** Destroy, which starts a run, so it waits for the runs like Plan does. */
export function destroyLockReason(state: LockState): string {
  return lockReason(state, { access: "Destroying requires operator access.", change: "destroy", inFlight: "destroying", runsFailed: RUNS_FAILED });
}

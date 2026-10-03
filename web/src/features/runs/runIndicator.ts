import { Ban, Check, Equal, FileText, Hourglass, LoaderCircle, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { TemplateRun, TemplateRunStep } from "../../api/types";
import type { StatusIndicator } from "../../shared/StatusLabel";

export type RunFields = Pick<TemplateRun, "operation" | "status" | "step" | "plan_summary" | "auto_approve">;

// runHeadline says where a run is, in words, with the operation folded in so
// no separate Type is needed: "destroy waiting for approval" rather than
// destroy + waiting_approval. Every destroy label says destroy, since the
// label is the only place the runs table names the operation. The words are
// lowercase, as openplan UI's status words are in rows and beside a title.
//
// A saved plan that someone discarded ends canceled, and reads as discarded;
// nothing else ends canceled, since a running run cannot be stopped.
//
// A plan run only ever plans, so it reads as planning until it ends. An
// auto-approved apply run never plans on its own, so it reads as applying
// throughout. Any other run has plan counts only once a plan with changes has
// finished, so one that has them and is still working is applying, and one
// that failed with them failed applying.
function runHeadline(run: RunFields): string {
  if (run.operation === "plan") {
    return planRunHeadline(run);
  }
  if (run.auto_approve) {
    return autoApprovedRunHeadline(run);
  }

  const destroy = run.operation === "destroy";
  const planned = run.plan_summary !== null && run.plan_summary !== undefined;

  switch (run.status) {
    case "waiting_approval":
      return destroy ? "destroy waiting for approval" : "waiting for approval";
    case "approved":
      return destroy ? "destroy approved" : "approved";
    case "completed":
      if (!planned) {
        return destroy ? "nothing to destroy" : "no changes";
      }
      return destroy ? "destroyed" : "applied";
    case "failed":
      if (!planned) {
        return destroy ? "destroy plan failed" : "plan failed";
      }
      return destroy ? "destroy failed" : "apply failed";
    case "canceled":
      return destroy ? "destroy discarded" : "discarded";
    default:
      if (!planned) {
        return destroy ? "planning destroy" : "planning";
      }
      return destroy ? "destroying" : "applying";
  }
}

function planRunHeadline(run: Pick<TemplateRun, "status" | "plan_summary">): string {
  switch (run.status) {
    case "completed":
      return run.plan_summary ? "plan finished" : "no changes";
    case "failed":
      return "plan failed";
    case "canceled":
      return "canceled";
    default:
      return "planning";
  }
}

function autoApprovedRunHeadline(run: Pick<TemplateRun, "status">): string {
  switch (run.status) {
    case "completed":
      return "applied";
    case "failed":
      return "apply failed";
    case "canceled":
      return "canceled";
    default:
      return "applying";
  }
}

const STEP_LABELS: Record<TemplateRunStep, string> = {
  waiting_for_executor: "waiting for an executor",
  preparing_workspace: "preparing workspace",
  fetching_source: "fetching source",
  restoring_plan: "restoring saved plan",
  initializing: "initializing",
  selecting_workspace: "selecting workspace",
  planning: "planning",
  saving_plan: "saving plan",
  applying: "applying"
};

// Steps the headline already names: "planning · planning" says nothing.
const HEADLINE_STEPS = new Set<TemplateRunStep>(["planning", "applying"]);

// A running run adds the step it is on, and a failed one the step it failed
// on, so a slow clone reads as a clone and a failed one says so.
function runLabel(run: RunFields, headline: string): string {
  if (run.step === "" || HEADLINE_STEPS.has(run.step)) {
    return headline;
  }
  // A backend newer than this client can send a step outside the union; read
  // it as if there were none rather than rendering "undefined" or throwing.
  const step = STEP_LABELS[run.step];
  if (!step) {
    return headline;
  }
  if (run.status === "running") {
    return `${headline} · ${step}`;
  }
  if (run.status === "failed") {
    return `${headline} while ${step}`;
  }
  return headline;
}

// A finished run's icon says what it left behind: nothing changed, a plan to
// read, or changes made.
const COMPLETED_ICONS: Record<string, LucideIcon> = {
  "no changes": Equal,
  "nothing to destroy": Equal,
  "plan finished": FileText
};

/**
 * openplan UI's status for one run. Only a plan waiting for approval and a
 * failure are coloured; a run still working stays grey, like a template that
 * is destroying.
 */
export function runIndicator(run: RunFields): StatusIndicator {
  const headline = runHeadline(run);
  const label = runLabel(run, headline);
  switch (run.status) {
    case "waiting_approval":
      return { label, icon: Hourglass, tone: "attention", strong: true };
    case "failed":
      return { label, icon: TriangleAlert, tone: "failed", strong: true };
    case "canceled":
      return { label, icon: Ban, tone: "idle", strong: false };
    case "completed":
      return { label, icon: COMPLETED_ICONS[headline] ?? Check, tone: "settled", strong: false };
    default:
      return { label, icon: LoaderCircle, tone: "settled", strong: false };
  }
}

// runProgressTag changes whenever a run moves: a new status, or a new step
// within one. Queries that must refresh as a run progresses, such as its
// logs, key on it.
export function runProgressTag(run: Pick<TemplateRun, "status" | "step">): string {
  return `${run.status}:${run.step}`;
}

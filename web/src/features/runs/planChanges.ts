import type { PlanChangeAction, PlanResourceChange, TemplateRun } from "../../api/types";
import { isTerminalRunStatus } from "../../api/polling";

export type RunView = "changes" | "logs";

// Reading order, as the API sorts the list: what removes something first.
export const ACTION_ORDER: PlanChangeAction[] = ["replace", "destroy", "update", "create", "forget", "import", "move"];

interface ActionLook {
  // One character each, so the column lines up: tofu's -/+ for a replace is
  // three, so a replace reads ± instead.
  glyph: string;
  present: string;
  past: string;
  // Theme colour of the glyph and word: PlanDiff's for what adds, changes or
  // removes, muted for what only changes the record.
  tone: "text-success" | "text-warning" | "text-destructive" | "text-muted-foreground";
  // A removal is what needs a person, so it is set at weight 500.
  removes: boolean;
}

const ACTIONS: Record<string, ActionLook> = {
  create: { glyph: "+", present: "create", past: "created", tone: "text-success", removes: false },
  update: { glyph: "~", present: "update", past: "updated", tone: "text-warning", removes: false },
  replace: { glyph: "±", present: "replace", past: "replaced", tone: "text-destructive", removes: true },
  destroy: { glyph: "−", present: "destroy", past: "destroyed", tone: "text-destructive", removes: true },
  forget: { glyph: "·", present: "forget", past: "forgotten", tone: "text-muted-foreground", removes: false },
  import: { glyph: "·", present: "import", past: "imported", tone: "text-muted-foreground", removes: false },
  move: { glyph: "·", present: "move", past: "moved", tone: "text-muted-foreground", removes: false }
};

/** How an action reads. One the API passed through raw keeps its words. */
export function actionLook(action: PlanChangeAction): ActionLook {
  return ACTIONS[action] ?? { glyph: "·", present: action, past: action, tone: "text-muted-foreground", removes: false };
}

/**
 * Whether the run's list says what was done rather than what would be: an
 * apply or destroy that finished. A plan run, a discarded plan or a failed
 * apply still reads as the plan's intent.
 */
export function isApplied(run: TemplateRun): boolean {
  return run.status === "completed" && run.operation !== "plan";
}

/**
 * The view a run opens on: its changes when there is a list to read, and its
 * logs otherwise. While planning, after a failure, for an auto-approved apply
 * and when the list is unavailable, the log is the only record of what
 * changes; for a plan with nothing to change, the log says so as plainly as
 * the Changes view would.
 */
export function defaultRunView(run: TemplateRun): RunView {
  if (run.status === "failed") {
    return "logs";
  }
  const state = run.plan_changes?.state;
  return state === "complete" || state === "truncated" ? "changes" : "logs";
}

/**
 * What the Changes view says in place of a list, or null when there is a list
 * to show.
 */
export function changesNote(run: TemplateRun): string | null {
  if (run.plan_changes?.state === "unavailable") {
    return "openplan can't list this plan's changes; the plan log has them all.";
  }
  if (run.plan_changes) {
    return null;
  }
  if (run.auto_approve) {
    return "An auto-approved apply saves no plan to list; its log says what it changed.";
  }
  if (run.status === "failed") {
    return "The plan failed, so this run changes nothing.";
  }
  if (!isTerminalRunStatus(run.status)) {
    return "Changes appear here when the plan finishes.";
  }
  return "This plan changes nothing.";
}

// tofu's reasons, said the way a reviewer would. A reason this doesn't know
// is a display hint the format says to treat as none.
const REASONS: Record<string, string> = {
  replace_because_tainted: "tainted",
  replace_because_cannot_update: "can't be updated in place",
  replace_by_request: "replacement requested",
  replace_by_triggers: "replace_triggered_by changed",
  delete_because_no_resource_config: "no longer in the configuration",
  delete_because_no_module: "its module was removed",
  delete_because_wrong_repetition: "count or for_each changed form",
  delete_because_count_index: "its count no longer includes it",
  delete_because_each_key: "its for_each no longer includes it",
  delete_because_no_move_target: "moved to an address that doesn't exist"
};

function pathText(path: (string | number)[]): string {
  return path.map((step) => (typeof step === "number" ? `[${step}]` : step)).join(".").replace(/\.\[/g, "[");
}

/**
 * Why a resource changes, from the plan: what forces a replace, why something
 * is destroyed, which attributes an update changes. A create says nothing:
 * no attribute means the same for every resource type. A move or an import
 * adds where from.
 */
export function changeReason(change: PlanResourceChange): string {
  const parts: string[] = [];
  if (change.action === "replace" && change.replace_paths && change.replace_paths.length > 0) {
    const paths = change.replace_paths.map(pathText);
    parts.push(`${paths.join(", ")} ${paths.length === 1 ? "forces" : "force"} replacement`);
  } else if (change.action === "update") {
    const changed = changedAttributes(change);
    if (changed.length === 1 && isShortScalar(changed[0].before) && isShortScalar(changed[0].after)) {
      parts.push(`${changed[0].key} ${valueText(changed[0].before)} → ${valueText(changed[0].after)}`);
    } else if (changed.length > 0) {
      const names = changed.map((attribute) => attribute.key);
      parts.push(names.length > 3 ? `${names.slice(0, 3).join(", ")} and ${names.length - 3} more` : names.join(", "));
    }
  } else if (change.action !== "create" && REASONS[change.action_reason]) {
    parts.push(REASONS[change.action_reason]);
  }
  if (change.action === "forget") {
    parts.push("left running, dropped from the state");
  }
  if (change.previous_address && change.previous_address !== change.address) {
    parts.push(`moved from ${change.previous_address}`);
  }
  if (change.importing_id) {
    parts.push(`imported (${change.importing_id})`);
  }
  return parts.join(" · ");
}

interface Attribute {
  key: string;
  before: unknown;
  after: unknown;
}

function asObject(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function same(a: unknown, b: unknown): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

/** The top-level attributes whose value the change alters, by name. */
export function changedAttributes(change: PlanResourceChange): Attribute[] {
  const before = asObject(change.before);
  const after = asObject(change.after);
  const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])].sort();
  return keys.filter((key) => !same(before[key], after[key])).map((key) => ({ key, before: before[key], after: after[key] }));
}

function isShortScalar(value: unknown): boolean {
  return (typeof value === "string" && value.length <= 24) || typeof value === "number" || typeof value === "boolean";
}

// Masks the executor put in place of values read as words, not strings.
const MASKS = new Set(["(sensitive)", "(known after apply)", "(too large to show)"]);

/** One value as tofu writes it: strings quoted, masks and absent values bare. */
export function valueText(value: unknown): string {
  if (value === undefined || value === null) {
    return "null";
  }
  if (typeof value === "string") {
    return MASKS.has(value) ? value : JSON.stringify(value);
  }
  return JSON.stringify(value);
}

export interface DiffLine {
  sign: "+" | "-" | "~" | " ";
  key: string;
  text: string;
  // An attribute that forces the replace.
  forcesReplacement: boolean;
}

/**
 * The lines an opened row shows, close to tofu's own plan output: for a
 * create every attribute, for a destroy every attribute going, and otherwise
 * only what changes, with a count of the rest. Values are the API's, masks
 * included.
 */
export function diffLines(change: PlanResourceChange): { lines: DiffLine[]; unchanged: number } {
  const before = asObject(change.before);
  const after = asObject(change.after);
  const forcing = new Set((change.replace_paths ?? []).map((path) => String(path[0])));
  const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])].sort();
  const lines: DiffLine[] = [];
  let unchanged = 0;
  for (const key of keys) {
    const inBefore = key in before && before[key] !== null;
    const inAfter = key in after && after[key] !== null;
    if (!inBefore && inAfter) {
      lines.push({ sign: "+", key, text: valueText(after[key]), forcesReplacement: forcing.has(key) });
    } else if (inBefore && !inAfter) {
      lines.push({ sign: "-", key, text: `${valueText(before[key])} -> null`, forcesReplacement: forcing.has(key) });
    } else if (!same(before[key], after[key])) {
      lines.push({ sign: "~", key, text: `${valueText(before[key])} -> ${valueText(after[key])}`, forcesReplacement: forcing.has(key) });
    } else if (inBefore) {
      unchanged++;
    }
  }
  return { lines, unchanged };
}

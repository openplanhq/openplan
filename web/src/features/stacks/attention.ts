import type { AttentionItem } from "../../api/types";

// What needs a person, counted the ways the stacks screens show it.

export interface StackAttention {
  waiting: number;
  destroyFailed: number;
  planFailed: number;
  applyFailed: number;
}

/** Which operation a run_failed item's run was: a plan or an apply. */
export function failedOperation(item: AttentionItem): "plan" | "apply" {
  return item.run?.operation === "apply" ? "apply" : "plan";
}

/** Counts each stack's items, by kind, failed runs by operation. A stack
    with none has no entry. */
export function attentionByStack(items: AttentionItem[]): Map<string, StackAttention> {
  const byStack = new Map<string, StackAttention>();
  for (const item of items) {
    const counts = byStack.get(item.stack.id) ?? { waiting: 0, destroyFailed: 0, planFailed: 0, applyFailed: 0 };
    if (item.kind === "waiting_approval") {
      counts.waiting += 1;
    } else if (item.kind === "destroy_failed") {
      counts.destroyFailed += 1;
    } else if (failedOperation(item) === "apply") {
      counts.applyFailed += 1;
    } else {
      counts.planFailed += 1;
    }
    byStack.set(item.stack.id, counts);
  }
  return byStack;
}

/** Each stack template's item, if it has one. */
export function attentionByStackTemplate(items: AttentionItem[]): Map<string, AttentionItem> {
  return new Map(items.map((item) => [item.stack_template.id, item]));
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/**
 * The summary line's two halves: how many stacks need attention, and what
 * the items are. Null when nothing does, since an all-clear line is noise.
 */
export function attentionSummary(items: AttentionItem[]): { lead: string; breakdown: string } | null {
  if (items.length === 0) {
    return null;
  }
  const stacks = attentionByStack(items);
  const total: StackAttention = { waiting: 0, destroyFailed: 0, planFailed: 0, applyFailed: 0 };
  for (const counts of stacks.values()) {
    total.waiting += counts.waiting;
    total.destroyFailed += counts.destroyFailed;
    total.planFailed += counts.planFailed;
    total.applyFailed += counts.applyFailed;
  }
  const parts: string[] = [];
  if (total.waiting > 0) parts.push(`${count(total.waiting, "plan", "plans")} waiting for approval`);
  if (total.destroyFailed > 0) parts.push(`${count(total.destroyFailed, "destroy", "destroys")} failed`);
  if (total.planFailed > 0) parts.push(`${count(total.planFailed, "plan", "plans")} failed`);
  if (total.applyFailed > 0) parts.push(`${count(total.applyFailed, "apply", "applies")} failed`);
  return {
    lead: `${count(stacks.size, "stack needs", "stacks need")} attention`,
    breakdown: parts.join(", ")
  };
}

/** A list row's chip text for its waiting plans. */
export function waitingChipLabel(waiting: number): string {
  return `${count(waiting, "plan", "plans")} to approve`;
}

/** A list row's chip text for its failed destroys. */
export function failedChipLabel(failed: number): string {
  return failed === 1 ? "destroy failed" : `${failed} destroys failed`;
}

/** A list row's chip text for its failed plans or applies, the word alone
    for one, as a failed destroy's is. */
export function runFailedChipLabel(operation: "plan" | "apply", failed: number): string {
  if (failed === 1) {
    return `${operation} failed`;
  }
  return `${failed} ${operation === "apply" ? "applies" : "plans"} failed`;
}

/** The word after a list row's template count; the row sets the number apart
    and says "No templates" itself when there are none. */
export function templateCountWord(templates: number): string {
  return templates === 1 ? "template" : "templates";
}

/** Where an item's run is read, or null when it has none. */
export function attentionRunPath(item: AttentionItem): string | null {
  if (!item.run) {
    return null;
  }
  return `/stacks/${item.stack.id}/templates/${item.stack_template.id}/runs/${item.run.run_number}`;
}

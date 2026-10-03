import type { AttentionItem } from "../../api/types";

// What needs a person, counted the ways the stacks screens show it.

export interface StackAttention {
  waiting: number;
  failed: number;
}

/** Counts each stack's items, by kind. A stack with none has no entry. */
export function attentionByStack(items: AttentionItem[]): Map<string, StackAttention> {
  const byStack = new Map<string, StackAttention>();
  for (const item of items) {
    const counts = byStack.get(item.stack.id) ?? { waiting: 0, failed: 0 };
    if (item.kind === "waiting_approval") {
      counts.waiting += 1;
    } else {
      counts.failed += 1;
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
  let waiting = 0;
  let failed = 0;
  for (const counts of stacks.values()) {
    waiting += counts.waiting;
    failed += counts.failed;
  }
  const parts: string[] = [];
  if (waiting > 0) parts.push(`${count(waiting, "plan", "plans")} waiting for approval`);
  if (failed > 0) parts.push(`${count(failed, "destroy", "destroys")} failed`);
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

export function templateCountLabel(templates: number): string {
  return templates === 0 ? "no templates" : count(templates, "template", "templates");
}

/** Where an item's run is read, or null when it has none. */
export function attentionRunPath(item: AttentionItem): string | null {
  if (!item.run) {
    return null;
  }
  return `/stacks/${item.stack.id}/templates/${item.stack_template.id}/runs/${item.run.run_number}`;
}

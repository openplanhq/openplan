import type { TemplateRun, TemplateRunStatus, TemplateSyncStatus } from "./types";

// Only syncing can still change. Stalled is final for the screen too: the
// sync may never end, so following it would poll forever.
export function isSyncSettled(status: TemplateSyncStatus): boolean {
  return status !== "syncing";
}

export function isTerminalRunStatus(status: TemplateRunStatus): boolean {
  return status === "completed" || status === "failed" || status === "canceled";
}

// A plan waiting for approval counts as settled: it can wait for days, and
// moves only when someone approves or discards it on the run's own page.
export function isMovingRun(run: TemplateRun): boolean {
  return run.status !== "waiting_approval" && !isTerminalRunStatus(run.status);
}

export function nextPollDelayMs(failureCount: number): number {
  return Math.min(1500 + failureCount * 500, 5000);
}

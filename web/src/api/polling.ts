import type { TemplateRunStatus, TemplateSyncStatus } from "./types";

// Only syncing can still change. Stalled is final for the screen too: the
// sync may never end, so following it would poll forever.
export function isSyncSettled(status: TemplateSyncStatus): boolean {
  return status !== "syncing";
}

export function isTerminalRunStatus(status: TemplateRunStatus): boolean {
  return status === "completed" || status === "failed" || status === "canceled";
}

export function nextPollDelayMs(failureCount: number): number {
  return Math.min(1500 + failureCount * 500, 5000);
}

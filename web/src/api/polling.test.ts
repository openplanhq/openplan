import { describe, expect, it } from "vitest";
import { isSyncSettled, isTerminalRunStatus, nextPollDelayMs } from "./polling";

describe("polling helpers", () => {
  it("settles a sync on every status but syncing, stalled included", () => {
    expect(isSyncSettled("syncing")).toBe(false);
    for (const status of ["synced", "up_to_date", "invalid", "failed", "stalled"] as const) {
      expect(isSyncSettled(status)).toBe(true);
    }
  });

  it("identifies terminal run states", () => {
    expect(isTerminalRunStatus("completed")).toBe(true);
    expect(isTerminalRunStatus("failed")).toBe(true);
    expect(isTerminalRunStatus("canceled")).toBe(true);
    expect(isTerminalRunStatus("waiting_approval")).toBe(false);
  });

  it("backs off after repeated failures without exceeding five seconds", () => {
    expect(nextPollDelayMs(0)).toBe(1500);
    expect(nextPollDelayMs(1)).toBe(2000);
    expect(nextPollDelayMs(10)).toBe(5000);
  });
});

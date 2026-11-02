import { describe, expect, it } from "vitest";
import { formatDuration, formatTimeOfDay } from "./formatTimestamp";

describe("formatDuration", () => {
  it("prints a step's time the way CI does", () => {
    expect(formatDuration("2026-07-20T10:00:00Z", "2026-07-20T10:00:00.400Z")).toBe("0s");
    expect(formatDuration("2026-07-20T10:00:00Z", "2026-07-20T10:00:12Z")).toBe("12s");
    expect(formatDuration("2026-07-20T10:00:00Z", "2026-07-20T10:04:05Z")).toBe("4m 5s");
    expect(formatDuration("2026-07-20T10:00:00Z", "2026-07-20T11:02:30Z")).toBe("1h 2m");
  });

  // A wrong number is worse than none.
  it("says nothing for an unreadable or backwards span", () => {
    expect(formatDuration("not a time", "2026-07-20T10:00:12Z")).toBe("");
    expect(formatDuration("2026-07-20T10:00:12Z", "2026-07-20T10:00:00Z")).toBe("");
  });
});

describe("formatTimeOfDay", () => {
  it("gives the time to the second, and an unreadable value as it came", () => {
    expect(formatTimeOfDay("2026-07-20T10:00:09Z")).toMatch(/^\d\d:\d\d:09$/);
    expect(formatTimeOfDay("soon")).toBe("soon");
  });
});

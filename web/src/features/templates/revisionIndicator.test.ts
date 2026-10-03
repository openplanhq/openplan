import { LoaderCircle, TriangleAlert } from "lucide-react";
import { describe, expect, it } from "vitest";
import { revisionIndicator } from "./revisionIndicator";

describe("revisionIndicator", () => {
  it("says nothing about an active revision", () => {
    expect(revisionIndicator("active")).toBeNull();
  });

  it.each([
    ["pending_validation", "waiting for validation", LoaderCircle, "settled", false],
    ["validating", "validating", LoaderCircle, "settled", false],
    ["invalid", "failed validation", TriangleAlert, "failed", true]
  ] as const)("draws %s as an icon and a word", (status, label, icon, tone, strong) => {
    expect(revisionIndicator(status)).toEqual({ label, icon, tone, strong });
  });
});

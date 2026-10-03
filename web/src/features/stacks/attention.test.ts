import { describe, expect, it } from "vitest";
import type { AttentionItem } from "../../api/types";
import {
  attentionByStack,
  attentionRunPath,
  attentionSummary,
  failedChipLabel,
  templateCountLabel,
  waitingChipLabel
} from "./attention";

function item(overrides: Partial<AttentionItem> & { stackID?: string; templateID?: string } = {}): AttentionItem {
  const { stackID = "stack_1", templateID = "tpl_1", ...rest } = overrides;
  return {
    kind: "waiting_approval",
    at: "2026-10-03T09:00:00Z",
    stack: { id: stackID, name: stackID, slug: stackID },
    stack_template: { id: templateID, display_name: "eks-cluster" },
    run: null,
    ...rest
  };
}

describe("attentionSummary", () => {
  it("is null when nothing needs attention", () => {
    expect(attentionSummary([])).toBeNull();
  });

  it("counts stacks in the lead and items by kind in the breakdown", () => {
    const summary = attentionSummary([
      item({ stackID: "prod", templateID: "a" }),
      item({ stackID: "prod", templateID: "b" }),
      item({ stackID: "analytics", templateID: "c" }),
      item({ stackID: "edge", templateID: "d", kind: "destroy_failed" })
    ]);
    expect(summary).toEqual({
      lead: "3 stacks need attention",
      breakdown: "3 plans waiting for approval, 1 destroy failed"
    });
  });

  it("speaks in the singular for one", () => {
    expect(attentionSummary([item()])).toEqual({ lead: "1 stack needs attention", breakdown: "1 plan waiting for approval" });
  });

  it("leaves out a kind with no items", () => {
    expect(attentionSummary([item({ kind: "destroy_failed" }), item({ templateID: "b", kind: "destroy_failed" })])?.breakdown).toBe(
      "2 destroys failed"
    );
  });
});

describe("attentionByStack", () => {
  it("counts each stack's items by kind", () => {
    const counts = attentionByStack([item(), item({ templateID: "b", kind: "destroy_failed" }), item({ stackID: "other" })]);
    expect(counts.get("stack_1")).toEqual({ waiting: 1, failed: 1 });
    expect(counts.get("other")).toEqual({ waiting: 1, failed: 0 });
    expect(counts.has("quiet")).toBe(false);
  });
});

describe("labels", () => {
  it.each([
    [1, "1 plan to approve"],
    [2, "2 plans to approve"]
  ])("waiting chip for %i", (n, label) => expect(waitingChipLabel(n)).toBe(label));

  it.each([
    [1, "destroy failed"],
    [2, "2 destroys failed"]
  ])("failed chip for %i", (n, label) => expect(failedChipLabel(n)).toBe(label));

  it.each([
    [0, "no templates"],
    [1, "1 template"],
    [4, "4 templates"]
  ])("template count %i", (n, label) => expect(templateCountLabel(n)).toBe(label));
});

describe("attentionRunPath", () => {
  it("is the run's page under its template", () => {
    const run = { run_number: 7 } as AttentionItem["run"];
    expect(attentionRunPath(item({ run }))).toBe("/stacks/stack_1/templates/tpl_1/runs/7");
  });

  it("is null without a run", () => {
    expect(attentionRunPath(item())).toBeNull();
  });
});

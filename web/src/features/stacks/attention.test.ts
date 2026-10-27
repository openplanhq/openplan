import { describe, expect, it } from "vitest";
import type { AttentionItem } from "../../api/types";
import {
  attentionByStack,
  attentionRunPath,
  attentionSummary,
  failedChipLabel,
  failedOperation,
  runFailedChipLabel,
  templateCountWord,
  waitingChipLabel
} from "./attention";

function item(overrides: Partial<AttentionItem> & { stackID?: string; templateID?: string } = {}): AttentionItem {
  const { stackID = "stack_1", templateID = "tpl_1", ...rest } = overrides;
  return {
    kind: "waiting_approval",
    at: "2026-10-03T09:00:00Z",
    stack: { id: stackID, name: stackID, slug: stackID },
    stack_template: { id: templateID, workspace_name: "ws_1", display_name: "eks-cluster" },
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

  it("names failed plans and applies apart from failed destroys", () => {
    const plan = { operation: "plan", run_number: 2 } as AttentionItem["run"];
    const apply = { operation: "apply", run_number: 3, plan_summary: { add: 1, change: 0, destroy: 0 } } as AttentionItem["run"];
    const summary = attentionSummary([
      item(),
      item({ templateID: "b", kind: "destroy_failed" }),
      item({ templateID: "c", kind: "run_failed", run: plan }),
      item({ stackID: "edge", templateID: "d", kind: "run_failed", run: plan }),
      item({ stackID: "edge", templateID: "e", kind: "run_failed", run: apply })
    ]);
    expect(summary).toEqual({
      lead: "2 stacks need attention",
      breakdown: "1 plan waiting for approval, 1 destroy failed, 2 plans failed, 1 apply failed"
    });
  });

  it("leaves out a kind with no items", () => {
    expect(attentionSummary([item({ kind: "destroy_failed" }), item({ templateID: "b", kind: "destroy_failed" })])?.breakdown).toBe(
      "2 destroys failed"
    );
  });
});

describe("attentionByStack", () => {
  it("counts each stack's items by kind", () => {
    const plan = { operation: "plan" } as AttentionItem["run"];
    const apply = { operation: "apply", plan_summary: { add: 1, change: 0, destroy: 0 } } as AttentionItem["run"];
    const counts = attentionByStack([
      item(),
      item({ templateID: "b", kind: "destroy_failed" }),
      item({ templateID: "c", kind: "run_failed", run: plan }),
      item({ templateID: "d", kind: "run_failed", run: apply }),
      item({ stackID: "other" })
    ]);
    expect(counts.get("stack_1")).toEqual({ waiting: 1, destroyFailed: 1, planFailed: 1, applyFailed: 1 });
    expect(counts.get("other")).toEqual({ waiting: 1, destroyFailed: 0, planFailed: 0, applyFailed: 0 });
    expect(counts.has("quiet")).toBe(false);
  });
});

describe("failedOperation", () => {
  // An apply run plans first: one that failed before it had a plan changed
  // nothing, so it reads as a failed plan, as the run's own label does.
  it.each([
    ["a plan", { operation: "plan", plan_summary: null, auto_approve: false }, "plan"],
    ["an apply that failed while planning", { operation: "apply", plan_summary: null, auto_approve: false }, "plan"],
    ["an apply that failed applying its plan", { operation: "apply", plan_summary: { add: 1, change: 0, destroy: 0 }, auto_approve: false }, "apply"],
    ["an auto-approved apply", { operation: "apply", plan_summary: null, auto_approve: true }, "apply"]
  ] as const)("names the operation of %s", (_, run, operation) => {
    expect(failedOperation(item({ kind: "run_failed", run: run as unknown as AttentionItem["run"] }))).toBe(operation);
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
    ["plan", 1, "plan failed"],
    ["plan", 2, "2 plans failed"],
    ["apply", 1, "apply failed"],
    ["apply", 3, "3 applies failed"]
  ] as const)("failed %s chip for %i", (operation, n, label) => expect(runFailedChipLabel(operation, n)).toBe(label));

  it.each([
    [1, "template"],
    [4, "templates"]
  ])("template count word for %i", (n, word) => expect(templateCountWord(n)).toBe(word));
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

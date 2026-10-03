import { Check, Diff, Hourglass, LoaderCircle, Minus, TriangleAlert } from "lucide-react";
import { describe, expect, it } from "vitest";
import type { AttentionItem, StackTemplate } from "../../api/types";
import { stackTemplateActivity, stackTemplateIndicator } from "./StackTemplateStatusLabel";

function template(overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id: "tpl_1",
    stack_id: "stack_1",
    component_key: "eks",
    source_template_id: "source_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "",
    source_ref: "main",
    workspace_name: "ws_1",
    display_name: "eks-cluster",
    config: {},
    last_applied_run_id: "",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "never",
    created_by: "user_1",
    lifecycle: "active",
    ...overrides
  };
}

const waiting = { kind: "waiting_approval", at: "2026-10-03T09:30:00Z" } as AttentionItem;

describe("stackTemplateIndicator", () => {
  // Every state is an icon and a word; only those that need a person are
  // coloured, and only those carry weight.
  it.each([
    ["applied", template({ live_state: "matches" }), undefined, Check, "settled", false],
    ["changed", template({ live_state: "differs" }), undefined, Diff, "attention", false],
    ["not applied", template(), undefined, Minus, "idle", false],
    ["destroy failed", template({ lifecycle: "failed" }), undefined, TriangleAlert, "failed", true],
    ["destroying", template({ lifecycle: "destroying" }), undefined, LoaderCircle, "settled", false],
    ["waiting for approval", template({ live_state: "differs" }), waiting, Hourglass, "attention", true]
  ] as const)("shows %s", (label, stackTemplate, attention, icon, tone, strong) => {
    expect(stackTemplateIndicator(stackTemplate, attention)).toEqual({ label, icon, tone, strong });
  });
});

describe("stackTemplateActivity", () => {
  it("says a template was never applied", () => {
    expect(stackTemplateActivity(template(), undefined)).toBe("Never applied");
  });

  it("says nothing of a destroy in progress", () => {
    expect(stackTemplateActivity(template({ lifecycle: "destroying", last_applied_at: "2026-09-22T10:00:00Z" }), undefined)).toBe("");
  });

  it("dates a waiting plan from when it finished", () => {
    expect(stackTemplateActivity(template(), waiting)).toMatch(/^Planned 3 Oct, \d\d:\d\d$/);
  });
});

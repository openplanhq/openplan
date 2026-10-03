import { describe, expect, it } from "vitest";
import type { StackTemplate } from "../../api/types";
import { defaultStackTemplate, stackTemplatePath, templateTabOf } from "./templateSelection";

function template(id: string, overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id,
    stack_id: "stack_1",
    component_key: id,
    source_template_id: "source_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "",
    source_ref: "main",
    workspace_name: id,
    display_name: id,
    config: {},
    last_applied_run_id: "",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "matches",
    created_by: "user_1",
    lifecycle: "active",
    ...overrides
  };
}

describe("defaultStackTemplate", () => {
  it("opens on the first template with a plan waiting for approval", () => {
    const failed = template("cdn", { lifecycle: "failed" });
    const waiting = template("eks", { pending_plan_run_id: "run_14" });
    expect(defaultStackTemplate([template("network"), failed, waiting])?.id).toBe("eks");
  });

  it("falls back to the first whose destroy failed", () => {
    expect(defaultStackTemplate([template("network"), template("cdn", { lifecycle: "failed" })])?.id).toBe("cdn");
  });

  it("falls back to the first template", () => {
    expect(defaultStackTemplate([template("network"), template("eks")])?.id).toBe("network");
  });

  it("has nothing to open on a stack with no templates", () => {
    expect(defaultStackTemplate([])).toBeNull();
  });
});

describe("templateTabOf", () => {
  it.each([
    ["/stacks/s1", "runs"],
    ["/stacks/s1/templates/new", "runs"],
    ["/stacks/s1/templates/t1", "runs"],
    ["/stacks/s1/templates/t1/runs", "runs"],
    ["/stacks/s1/templates/t1/runs/14", "runs"],
    ["/stacks/s1/templates/t1/variables", "variables"],
    ["/stacks/s1/templates/t1/credentials", "credentials"],
    ["/stacks/s1/templates/t1/settings", "settings"],
    ["/stacks/s1/templates/t1/upgrade", "settings"]
  ] as const)("%s belongs to %s", (pathname, tab) => {
    expect(templateTabOf(pathname)).toBe(tab);
  });
});

describe("stackTemplatePath", () => {
  it("names a template's tab, Runs by default", () => {
    expect(stackTemplatePath("s1", "t1")).toBe("/stacks/s1/templates/t1/runs");
    expect(stackTemplatePath("s1", "t1", "variables")).toBe("/stacks/s1/templates/t1/variables");
  });
});

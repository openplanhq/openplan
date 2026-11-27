import { describe, expect, it } from "vitest";
import { defaultStackTemplate, stackTemplatePath, templateTabOf } from "./templateSelection";
import { stackTemplate } from "../../api/testSupport";

describe("defaultStackTemplate", () => {
  it("opens on the first template with a plan waiting for approval", () => {
    const failed = stackTemplate({ id: "cdn", lifecycle: "failed" });
    const waiting = stackTemplate({ id: "eks", pending_plan_run_id: "run_14" });
    expect(defaultStackTemplate([stackTemplate({ id: "network" }), failed, waiting])?.id).toBe("eks");
  });

  it("falls back to the first whose destroy failed", () => {
    expect(defaultStackTemplate([stackTemplate({ id: "network" }), stackTemplate({ id: "cdn", lifecycle: "failed" })])?.id).toBe("cdn");
  });

  it("falls back to the first template", () => {
    expect(defaultStackTemplate([stackTemplate({ id: "network" }), stackTemplate({ id: "eks" })])?.id).toBe("network");
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
    ["/stacks/s1/templates/t1/environment", "environment"],
    ["/stacks/s1/templates/t1/settings", "settings"],
    ["/stacks/s1/templates/t1/change-revision", "settings"]
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

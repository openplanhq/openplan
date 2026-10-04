import { Ban, Check, Equal, FileText, Hourglass, LoaderCircle, TriangleAlert } from "lucide-react";
import { describe, expect, it } from "vitest";
import type { TemplateRun } from "../../api/types";
import { runIndicator, runProgressTag } from "./runIndicator";

const counts = { add: 1, change: 0, destroy: 0 };

function label(
  operation: TemplateRun["operation"],
  status: TemplateRun["status"],
  planned: boolean,
  autoApprove = false,
  step: TemplateRun["step"] = ""
): string {
  return runIndicator({ operation, status, step, plan_summary: planned ? counts : null, auto_approve: autoApprove }).label;
}

describe("runIndicator words", () => {
  it.each([
    ["apply", "running", false, "planning"],
    ["destroy", "queued", false, "planning destroy"],
    ["apply", "waiting_approval", true, "waiting for approval"],
    ["destroy", "waiting_approval", true, "destroy waiting for approval"],
    ["apply", "approved", true, "approved"],
    ["destroy", "approved", true, "destroy approved"],
    ["apply", "running", true, "applying"],
    ["destroy", "running", true, "destroying"],
    ["apply", "completed", false, "no changes"],
    ["destroy", "completed", false, "nothing to destroy"],
    ["apply", "completed", true, "applied"],
    ["destroy", "completed", true, "destroyed"],
    ["apply", "failed", false, "plan failed"],
    ["destroy", "failed", false, "destroy plan failed"],
    ["apply", "failed", true, "apply failed"],
    ["destroy", "failed", true, "destroy failed"],
    ["apply", "canceled", true, "discarded"],
    ["destroy", "canceled", true, "destroy discarded"]
  ] as const)("%s %s (planned: %s) reads %s", (operation, status, planned, expected) => {
    expect(label(operation, status, planned)).toBe(expected);
  });

  // A plan run applies nothing, so no label of its says it did, even once
  // its plan has counts.
  it.each([
    ["queued", false, "planning"],
    ["running", true, "planning"],
    ["completed", true, "plan finished"],
    ["completed", false, "no changes"],
    ["failed", true, "plan failed"],
    ["canceled", false, "canceled"]
  ] as const)("plan run %s (planned: %s) reads %s", (status, planned, expected) => {
    expect(label("plan", status, planned)).toBe(expected);
  });

  // An auto-approved apply never plans on its own, so it is applying from the
  // start, before it has any counts.
  it.each([
    ["queued", false, "applying"],
    ["running", false, "applying"],
    ["completed", true, "applied"],
    ["failed", false, "apply failed"],
    ["canceled", false, "canceled"]
  ] as const)("auto-approved apply %s (counts: %s) reads %s", (status, planned, expected) => {
    expect(label("apply", status, planned, true)).toBe(expected);
  });

  // A running run says what it is doing; a failed one says what it was doing.
  // Planning and applying add nothing the headline does not already say.
  it.each([
    ["plan", "running", false, false, "fetching_source", "planning · fetching source"],
    ["apply", "running", true, false, "waiting_for_executor", "applying · waiting for an executor"],
    ["destroy", "running", true, false, "restoring_plan", "destroying · restoring saved plan"],
    ["apply", "running", false, true, "initializing", "applying · initializing"],
    ["apply", "running", false, false, "planning", "planning"],
    ["apply", "running", true, false, "applying", "applying"],
    ["apply", "failed", false, false, "fetching_source", "plan failed while fetching source"],
    ["destroy", "failed", true, false, "waiting_for_executor", "destroy failed while waiting for an executor"],
    ["apply", "failed", true, false, "applying", "apply failed"],
    ["apply", "waiting_approval", true, false, "saving_plan", "waiting for approval"],
    ["apply", "completed", true, false, "applying", "applied"]
  ] as const)("%s %s (planned: %s, auto: %s) on %s reads %s", (operation, status, planned, autoApprove, step, expected) => {
    expect(label(operation, status, planned, autoApprove, step)).toBe(expected);
  });

  // A backend newer than this client can send a step outside the union (API
  // responses are cast, not validated). It reads exactly like no step.
  it("reads an unknown step as its headline, running or failed", () => {
    const unknownStep = "cloning" as TemplateRun["step"];
    expect(label("apply", "running", false, false, unknownStep)).toBe("planning");
    expect(label("apply", "failed", false, false, unknownStep)).toBe("plan failed");
  });
});

describe("runIndicator look", () => {
  // Only what waits on a person or broke is coloured, and only that carries
  // weight; a run still working is grey, like the destroying template state.
  it.each([
    ["apply", "waiting_approval", true, false, Hourglass, "attention", true],
    ["apply", "completed", true, false, Check, "settled", false],
    ["destroy", "completed", true, false, Check, "settled", false],
    ["plan", "completed", true, false, FileText, "settled", false],
    ["apply", "completed", false, false, Equal, "settled", false],
    ["destroy", "completed", false, false, Equal, "settled", false],
    ["apply", "completed", false, true, Check, "settled", false],
    ["apply", "failed", true, false, TriangleAlert, "failed", true],
    ["apply", "canceled", true, false, Ban, "idle", false],
    ["apply", "running", true, false, LoaderCircle, "settled", false],
    ["apply", "queued", false, false, LoaderCircle, "settled", false],
    ["apply", "approved", true, false, LoaderCircle, "settled", false]
  ] as const)("%s %s (planned: %s, auto: %s) is drawn as it should be", (operation, status, planned, autoApprove, icon, tone, strong) => {
    const indicator = runIndicator({ operation, status, step: "", plan_summary: planned ? counts : null, auto_approve: autoApprove });
    expect({ icon: indicator.icon, tone: indicator.tone, strong: indicator.strong }).toEqual({ icon, tone, strong });
  });
});

// Logs are refetched when this tag changes. Status alone stays running for a
// whole run, so the step has to be part of it.
describe("runProgressTag", () => {
  it("changes when the step changes within one status", () => {
    expect(runProgressTag({ status: "running", step: "initializing" })).not.toBe(runProgressTag({ status: "running", step: "planning" }));
  });

  it("changes when the status changes on the same step", () => {
    expect(runProgressTag({ status: "running", step: "applying" })).not.toBe(runProgressTag({ status: "completed", step: "applying" }));
  });
});

import { describe, expect, it } from "vitest";
import type { TemplateRun } from "../../api/types";
import {
  destroyLockReason,
  revisionLockReason,
  runActionsNote,
  runInFlightReason,
  startRunLockReason,
  variablesLockReason
} from "./lockReasons";
import type { LockState } from "./lockReasons";

function run(status: TemplateRun["status"]): TemplateRun {
  return {
    id: "run_7",
    tenant_id: "tenant_123",
    stack_template_id: "st_1",
    template_revision_id: "rev_1",
    source_template_id: "src_1",
    operation: "apply",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    workspace_name: "ws",
    config_json: {},
    backend_type: "s3",
    backend_config_hash: "hash",
    status,
    step: "",
    trigger_actor: "user_123",
    trigger_actor_display_name: "user_123",
    created_at: "2026-10-03T00:00:00Z",
    error_summary: "",
    run_number: 7,
    auto_approve: false,
    plan_summary: null
  };
}

const free: LockState = { canOperate: true, lifecycle: "active", runs: "success", activeRun: null };

describe("runInFlightReason", () => {
  it("asks for a decision on a waiting plan and patience otherwise, as a sentence", () => {
    expect(runInFlightReason(run("waiting_approval"), "changing the config")).toBe("Apply or discard run #7 before changing the config.");
    expect(runInFlightReason(run("running"), "destroying")).toBe("Wait for run #7 before destroying.");
  });
});

describe("lock reasons", () => {
  it("leave every control free when nothing stands in the way", () => {
    for (const reason of [startRunLockReason, variablesLockReason, revisionLockReason, destroyLockReason]) {
      expect(reason(free)).toBe("");
    }
    expect(runActionsNote(free)).toBe("Plan shows what would change. Apply saves a plan that waits for approval.");
  });

  // Columns: what locks, then the reason for Plan and Apply, Save variables,
  // Change revision and Destroy. "" means that control stays free.
  it.each([
    [
      "missing operator access",
      { canOperate: false, lifecycle: "destroying", activeRun: run("running") },
      "Starting a run requires operator access.",
      "Editing requires operator access.",
      "Changing the revision requires operator access.",
      "Destroying requires operator access."
    ],
    [
      "a destroy in progress",
      { lifecycle: "destroying", activeRun: run("running") },
      "Destroy in progress.",
      "Destroy in progress.",
      "Destroy in progress.",
      "Destroy in progress."
    ],
    [
      "a failed destroy",
      { lifecycle: "failed" },
      "A template whose destroy failed cannot start runs.",
      "",
      "",
      "A template whose destroy failed cannot start runs."
    ],
    ["an orphaned template", { lifecycle: "orphaned" }, "Only an active template can start runs.", "", "", "Only an active template can be destroyed."],
    ["runs still loading", { runs: "pending" }, "Loading runs…", "", "", "Loading runs…"],
    [
      "runs that failed to load",
      { runs: "error" },
      "This template's runs could not be loaded.",
      "",
      "",
      "This template's runs could not be loaded."
    ],
    [
      "a plan waiting for approval",
      { activeRun: run("waiting_approval") },
      "Apply or discard run #7 before starting another run.",
      "Apply or discard run #7 before changing the config.",
      "Apply or discard run #7 before changing the revision.",
      "Apply or discard run #7 before destroying."
    ],
    [
      "a run still working",
      { activeRun: run("running") },
      "Wait for run #7 before starting another run.",
      "Wait for run #7 before changing the config.",
      "Wait for run #7 before changing the revision.",
      "Wait for run #7 before destroying."
    ]
  ] as const)("say why for %s", (_, overrides, startRun, variables, revision, destroy) => {
    const state: LockState = { ...free, ...overrides };
    expect([startRunLockReason(state), variablesLockReason(state), revisionLockReason(state), destroyLockReason(state)]).toEqual([
      startRun,
      variables,
      revision,
      destroy
    ]);
  });

  it("puts the lock in the note beside Plan and Apply, in place of what they do", () => {
    expect(runActionsNote({ ...free, runs: "pending" })).toBe("Loading runs…");
  });
});

import { describe, expect, it } from "vitest";
import type { PlanResourceChange, TemplateRun } from "../../api/types";
import { actionLook, changeReason, changesNote, defaultRunView, diffLines, isApplied } from "./planChanges";

function change(overrides: Partial<PlanResourceChange> = {}): PlanResourceChange {
  return {
    address: "aws_db_instance.main",
    previous_address: "",
    module_address: "",
    mode: "managed",
    type: "aws_db_instance",
    name: "main",
    provider_name: "registry.opentofu.org/hashicorp/aws",
    deposed: "",
    action: "update",
    create_before_destroy: false,
    action_reason: "",
    importing_id: "",
    replace_paths: null,
    before: null,
    after: null,
    ...overrides
  };
}

function run(overrides: Partial<TemplateRun> = {}): TemplateRun {
  return {
    id: "run_1",
    tenant_id: "tenant_123",
    stack_template_id: "stpl_1",
    template_revision_id: "rev_1",
    operation: "apply",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1",
    status: "waiting_approval",
    step: "",
    trigger_actor: "user_1",
    trigger_actor_display_name: "Priya Raman",
    created_at: "2026-10-19T10:42:00Z",
    error_summary: "",
    run_number: 22,
    auto_approve: false,
    plan_summary: { add: 4, change: 1, destroy: 2 },
    plan_changes: { state: "complete", total: 6 },
    ...overrides
  };
}

describe("actionLook", () => {
  it("gives every action one glyph, so the column lines up", () => {
    for (const action of ["create", "update", "replace", "destroy", "forget", "import", "move", "something-new"]) {
      expect(actionLook(action).glyph).toHaveLength(1);
    }
    expect(actionLook("replace").glyph).toBe("±");
  });

  it("keeps the words of an action it doesn't name", () => {
    expect(actionLook("create-forget").present).toBe("create-forget");
    expect(actionLook("create-forget").tone).toBe("text-muted-foreground");
  });
});

describe("changeReason", () => {
  it("names what forces a replace", () => {
    expect(changeReason(change({ action: "replace", replace_paths: [["storage_encrypted"]] }))).toBe("storage_encrypted forces replacement");
    expect(changeReason(change({ action: "replace", replace_paths: [["a"], ["tags", "env"], ["rule", 0]] }))).toBe(
      "a, tags.env, rule[0] force replacement"
    );
  });

  it("says why a resource is destroyed, and treats a reason it doesn't know as none", () => {
    expect(changeReason(change({ action: "destroy", action_reason: "delete_because_no_resource_config" }))).toBe("no longer in the configuration");
    expect(changeReason(change({ action: "destroy", action_reason: "delete_because_something_new" }))).toBe("");
  });

  it("shows a single short update's values, and names several", () => {
    expect(changeReason(change({ before: { max_connections: "200", id: "x" }, after: { max_connections: "400", id: "x" } }))).toBe(
      'max_connections "200" → "400"'
    );
    expect(changeReason(change({ before: { a: 1, b: 2, c: 3, d: 4 }, after: { a: 5, b: 6, c: 7, d: 8 } }))).toBe("a, b, c and 1 more");
  });

  it("says nothing for a create, and adds a move and an import", () => {
    expect(changeReason(change({ action: "create", after: { name: "x" } }))).toBe("");
    expect(changeReason(change({ action: "move", previous_address: "aws_instance.old", address: "aws_instance.new" }))).toBe(
      "moved from aws_instance.old"
    );
    expect(changeReason(change({ action: "import", importing_id: "i-123" }))).toBe("imported (i-123)");
  });
});

describe("diffLines", () => {
  it("shows only what changes, marking what forces the replace, and counts the rest", () => {
    const { lines, unchanged } = diffLines(
      change({
        action: "replace",
        replace_paths: [["storage_encrypted"]],
        before: { storage_encrypted: false, password: "hunter2", arn: "arn:1", name: "main" },
        after: { storage_encrypted: true, password: "hunter2", arn: "(known after apply)", name: "main" }
      })
    );
    expect(lines).toEqual([
      { sign: "~", key: "arn", text: '"arn:1" -> (known after apply)', forcesReplacement: false },
      { sign: "~", key: "storage_encrypted", text: "false -> true", forcesReplacement: true }
    ]);
    expect(unchanged).toBe(2);
  });

  it("lists everything a create adds and a destroy removes", () => {
    expect(diffLines(change({ action: "create", after: { name: "x" } })).lines).toEqual([{ sign: "+", key: "name", text: '"x"', forcesReplacement: false }]);
    expect(diffLines(change({ action: "destroy", before: { bucket: "logs" } })).lines).toEqual([
      { sign: "-", key: "bucket", text: '"logs" -> null', forcesReplacement: false }
    ]);
  });
});

describe("the view a run opens on", () => {
  it("opens on Changes when there is a list, and on Logs otherwise", () => {
    expect(defaultRunView(run())).toBe("changes");
    expect(defaultRunView(run({ plan_changes: { state: "truncated", total: 4000 } }))).toBe("changes");
    expect(defaultRunView(run({ plan_changes: { state: "unavailable", total: null } }))).toBe("logs");
    expect(defaultRunView(run({ status: "running", plan_changes: null, plan_summary: null }))).toBe("logs");
    expect(defaultRunView(run({ status: "failed" }))).toBe("logs");
    expect(defaultRunView(run({ status: "completed", plan_changes: null, plan_summary: null }))).toBe("logs");
  });

  it("says why a run has no list", () => {
    expect(changesNote(run())).toBeNull();
    expect(changesNote(run({ plan_changes: { state: "unavailable", total: null } }))).toMatch(/plan log has them all/);
    expect(changesNote(run({ status: "running", plan_changes: null }))).toBe("Changes appear here when the plan finishes.");
    expect(changesNote(run({ status: "failed", plan_changes: null }))).toBe("The plan failed, so this run changes nothing.");
    expect(changesNote(run({ status: "completed", plan_changes: null, plan_summary: null }))).toBe("This plan changes nothing.");
    expect(changesNote(run({ status: "completed", auto_approve: true, plan_changes: null }))).toMatch(/auto-approved apply/);
  });

  it("reads in the past tense once an apply finished", () => {
    expect(isApplied(run({ status: "completed" }))).toBe(true);
    expect(isApplied(run({ status: "completed", operation: "plan" }))).toBe(false);
    expect(isApplied(run({ status: "canceled" }))).toBe(false);
  });
});

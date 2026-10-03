// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRun } from "../../api/types";
import { AuthContext } from "../../auth/AuthContext";
import TemplateRunHistory from "./TemplateRunHistory";

function run(overrides: Partial<TemplateRun> = {}): TemplateRun {
  return {
    id: "run_1",
    tenant_id: "tenant_123",
    stack_template_id: "stpl_1",
    template_revision_id: "rev_1",
    source_template_id: "tpl_1",
    operation: "plan",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    workspace_name: "acme-prod-primary",
    config_json: {},
    backend_type: "s3",
    backend_config_hash: "hash",
    status: "queued",
    step: "",
    trigger_actor: "user_123",
    trigger_actor_display_name: "user_123",
    created_at: "2026-07-20T00:00:00Z",
    error_summary: "",
    run_number: 1,
    auto_approve: false,
    plan_summary: null,
    ...overrides
  };
}

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function seedRuns(queryClient: QueryClient, runs: TemplateRun[]) {
  queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "stpl_1"), runs);
}

function renderHistory(queryClient: QueryClient) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
          status: "authenticated",
          login: () => {},
          logout: () => {}
        }}
      >
        <MemoryRouter initialEntries={["/stacks/stack_1/templates/stpl_1/runs"]}>
          <TemplateRunHistory stackId="stack_1" stackTemplateId="stpl_1" />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

describe("TemplateRunHistory", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("links every run in the history to its run detail screen", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, [
      run({ id: "run_apply_1", run_number: 2, operation: "apply", status: "waiting_approval", trigger_actor: "someone_else", created_at: "2026-07-20T01:00:00Z" }),
      run({ id: "run_plan_1", run_number: 1, operation: "plan", status: "completed", trigger_actor: "someone_else", created_at: "2026-07-20T00:00:00Z" })
    ]);

    renderHistory(queryClient);

    // Links carry the run's number within its template, not its id.
    expect(screen.getByTestId("template-run-history-run_apply_1").getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs/2");
    expect(screen.getByTestId("template-run-history-run_plan_1").getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs/1");
  });

  it("lays each run out in run, status, changes, started by and time columns", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, [
      run({
        id: "run_plan_1",
        run_number: 12,
        operation: "apply",
        status: "completed",
        trigger_actor: "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs",
        trigger_actor_display_name: "Ada Lovelace",
        plan_summary: { add: 3, change: 1, destroy: 0 }
      })
    ]);

    renderHistory(queryClient);

    expect(screen.getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Run", "Status", "Changes", "Started by", "Time"]);
    const cells = within(screen.getByTestId("template-run-row-run_plan_1")).getAllByRole("cell");
    expect(cells).toHaveLength(5);
    expect(cells[0].textContent).toBe("#12");
    expect(cells[1].textContent).toBe("applied");
    expect(within(cells[2]).getByRole("img", { name: "3 to add, 1 to change, 0 to destroy" })).toBeTruthy();
    expect(cells[3].textContent).toBe("Ada Lovelace");
    expect(cells[4].querySelector("time")?.getAttribute("datetime")).toBe("2026-07-20T00:00:00Z");
  });

  // Approving and discarding happen on the run, after reading its plan.
  it("offers no actions on any row, even a plan waiting for approval", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, [run({ id: "run_apply_1", run_number: 2, operation: "apply", status: "waiting_approval" })]);

    renderHistory(queryClient);

    expect(screen.getAllByRole("columnheader")).toHaveLength(5);
    expect(screen.getByTestId("template-run-row-run_apply_1").querySelector("button")).toBeNull();
  });

  // A status names its step too ("applying · preparing workspace"), which
  // can be wider than its column; it wraps there rather than running into
  // the changes beside it.
  it("lets a long status wrap inside its column", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, [run({ id: "run_1", operation: "apply", status: "running", step: "preparing_workspace", plan_summary: { add: 1, change: 0, destroy: 0 } })]);

    renderHistory(queryClient);

    const status = screen.getByTestId("template-run-status-run_1");
    expect(status.textContent).toBe("applying · preparing workspace");
    expect(status.className).toContain("whitespace-normal");
    expect(status.className).not.toContain("whitespace-nowrap");
  });

  it("shows an empty state that says how to start", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, []);

    renderHistory(queryClient);

    expect(screen.getByTestId("template-run-history-empty").textContent).toContain("No runs yet");
    expect(screen.getByTestId("template-run-history-empty").textContent).toContain("Plan to see what this template would create.");
  });

});

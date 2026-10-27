// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { pagesOf } from "../../api/listTestSupport";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRun } from "../../api/types";
import { AuthContext } from "../../auth/AuthContext";
import { useRefreshStackOnRunChange } from "./useRefreshStackOnRunChange";

function run(overrides: Partial<TemplateRun> = {}): TemplateRun {
  return {
    id: "run_1",
    tenant_id: "tenant_123",
    stack_template_id: "stpl_1",
    template_revision_id: "rev_1",
    operation: "plan",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    status: "running",
    step: "planning",
    trigger_actor: "user_123",
    trigger_actor_display_name: "user_123",
    created_at: "2026-10-27T09:00:00Z",
    error_summary: "",
    command_error: "",
    run_number: 1,
    auto_approve: false,
    plan_summary: null,
    plan_changes: null,
    ...overrides
  };
}

function setup(latest: TemplateRun) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "stpl_1"), pagesOf([latest]));
  const invalidated: unknown[][] = [];
  vi.spyOn(queryClient, "invalidateQueries").mockImplementation(async (filters) => {
    invalidated.push([...(filters?.queryKey ?? [])]);
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          me: { sub: "user_123", tenant_id: "tenant_123", display_name: "Test User", global_capabilities: { can_create_stack: false, can_publish_template: false, can_manage_platform_access: false } },
          status: "authenticated",
          login: () => {},
          logout: () => {}
        }}
      >
        {children}
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  renderHook(() => useRefreshStackOnRunChange("stack_1", "stpl_1"), { wrapper });
  return { queryClient, invalidated };
}

describe("useRefreshStackOnRunChange", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("reads nothing again while the latest run is still going", () => {
    const { invalidated } = setup(run());

    expect(invalidated).toEqual([]);
  });

  // The attention list polls every 30 s; a failed plan, or one now waiting,
  // must reach the template's status and Needs attention with the stack.
  it.each(["failed", "waiting_approval", "completed"] as const)("reads the stack and the attention list again once the latest run is %s", async (status) => {
    const { queryClient, invalidated } = setup(run());

    act(() => {
      queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "stpl_1"), pagesOf([run({ status })]));
    });

    // The query hands the new list over after the update, not during it.
    await waitFor(() => expect(invalidated).toContainEqual([...queryKeys.stack("tenant_123", "stack_1")]));
    expect(invalidated).toContainEqual([...queryKeys.attention("tenant_123")]);
  });
});

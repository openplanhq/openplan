// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { queryKeys } from "../../api/queryKeys";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { AttentionItem, TemplateRun } from "../../api/types";
import { formatDateTime } from "../../shared/formatTimestamp";
import StackAttentionScreen from "./StackAttentionScreen";

const TENANT = "tenant_123";

function waiting(overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    kind: "waiting_approval",
    at: "2026-10-03T09:30:00Z",
    stack: { id: "stack_prod", name: "prod", slug: "prod" },
    stack_template: { id: "tpl_eks", workspace_name: "ws_eks", display_name: "eks-cluster" },
    run: {
      id: "run_7",
      run_number: 7,
      trigger_actor: "user_priya",
      trigger_actor_display_name: "Priya Shah",
      plan_summary: { add: 2, change: 1, destroy: 0 },
      plan_changes: null,
    } as TemplateRun,
    ...overrides
  };
}

function failed(overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    kind: "destroy_failed",
    at: "2026-09-29T08:00:00Z",
    stack: { id: "stack_edge", name: "Edge CDN", slug: "edge-cdn" },
    stack_template: { id: "tpl_cdn", workspace_name: "ws_cdn", display_name: "cloudfront" },
    run: { id: "run_3", run_number: 3 } as TemplateRun,
    ...overrides
  };
}

function runFailed(operation: "plan" | "apply", overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    kind: "run_failed",
    at: "2026-10-03T11:00:00Z",
    stack: { id: "stack_dev", name: "dev", slug: "dev" },
    stack_template: { id: `tpl_${operation}`, workspace_name: `ws_${operation}`, display_name: operation === "plan" ? "supabase" : "vpc" },
    run: { id: "run_2", run_number: 2, operation, status: "failed", plan_summary: operation === "apply" ? { add: 1, change: 0, destroy: 0 } : null } as TemplateRun,
    ...overrides
  };
}

function testQueryClient(items?: AttentionItem[]): QueryClient {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (items) {
    queryClient.setQueryData(queryKeys.attention(TENANT), items);
  }
  return queryClient;
}

const auth: AuthContextValue = {
  me: { sub: "user_1", tenant_id: TENANT, display_name: "Test User", global_capabilities: { can_create_stack: false, can_manage_templates: false, can_manage_platform_access: false } },
  status: "authenticated",
  login: () => {},
  logout: () => {}
};

function renderScreen(queryClient: QueryClient) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={["/stacks/attention"]}>
          <StackAttentionScreen />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

describe("StackAttentionScreen", () => {
  beforeEach(() => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows a loading state while the list is pending", () => {
    renderScreen(testQueryClient());

    expect(screen.getByTestId("attention-loading")).toBeTruthy();
  });

  it("titles the page under a trail back to the stacks, with no count", () => {
    renderScreen(testQueryClient([waiting(), failed()]));

    expect(screen.getByRole("heading", { level: 1, name: "Needs attention" })).toBeTruthy();
    expect(screen.queryByTestId("page-count")).toBeNull();
    const trail = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(trail.getByRole("link", { name: "Stacks" }).getAttribute("href")).toBe("/stacks");
  });

  it("lists waiting plans with who planned them, what they change and a link to review", () => {
    renderScreen(testQueryClient([waiting(), failed()]));

    const group = within(screen.getByTestId("attention-group-waiting"));
    expect(group.getByRole("heading", { level: 2 }).textContent).toBe("Waiting for approval");
    const row = within(screen.getByTestId("attention-row-tpl_eks"));
    expect(row.getByRole("link", { name: "prod / eks-cluster" }).getAttribute("href")).toBe("/stacks/stack_prod/templates/tpl_eks");
    expect(row.getByText("Planned by Priya Shah")).toBeTruthy();
    expect(row.getByRole("img", { name: "2 to add, 1 to change, 0 to destroy" }).textContent).toBe("+2~1−0");
    expect(row.getByText(formatDateTime("2026-10-03T09:30:00Z"))).toBeTruthy();
    expect(row.getByRole("link", { name: "Review plan" }).getAttribute("href")).toBe("/stacks/stack_prod/templates/tpl_eks/runs/7");
  });

  it("lists failed destroys with what that means and a link to the run", () => {
    renderScreen(testQueryClient([waiting(), failed()]));

    const group = within(screen.getByTestId("attention-group-failed"));
    expect(group.getByRole("heading", { level: 2 }).textContent).toBe("Destroy failed");
    const row = within(screen.getByTestId("attention-row-tpl_cdn"));
    expect(row.getByText("The destroy run stopped before it finished. Some resources may still exist.")).toBeTruthy();
    expect(row.getByRole("link", { name: "View run" }).getAttribute("href")).toBe("/stacks/stack_edge/templates/tpl_cdn/runs/3");
  });

  // A failed plan changed nothing; a failed apply may have changed some of
  // what it set out to, so the two lines say so apart.
  it("lists failed plans and applies after failed destroys, each saying what it means", () => {
    renderScreen(testQueryClient([waiting(), failed(), runFailed("plan"), runFailed("apply")]));

    const groups = screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent);
    expect(groups).toEqual(["Waiting for approval", "Destroy failed", "Plan or apply failed"]);
    const plan = within(screen.getByTestId("attention-row-tpl_plan"));
    expect(plan.getByRole("link", { name: "dev / supabase" })).toBeTruthy();
    expect(plan.getByText("The plan failed. Nothing was changed.")).toBeTruthy();
    expect(plan.getByText(formatDateTime("2026-10-03T11:00:00Z"))).toBeTruthy();
    expect(plan.getByRole("link", { name: "View run" }).getAttribute("href")).toBe("/stacks/stack_dev/templates/tpl_plan/runs/2");
    const apply = within(screen.getByTestId("attention-row-tpl_apply"));
    expect(apply.getByText("The apply failed. Some changes may have been made.")).toBeTruthy();
  });

  // An apply whose own plan failed changed nothing, so it says so.
  it("says nothing changed when an apply failed before it had a plan", () => {
    const item = runFailed("apply");
    renderScreen(testQueryClient([{ ...item, run: { ...item.run!, plan_summary: null } }]));

    expect(within(screen.getByTestId("attention-row-tpl_apply")).getByText("The plan failed. Nothing was changed.")).toBeTruthy();
  });

  // The only item a failed plan: the page lists it rather than standing empty.
  it("shows a failed plan when it is the only thing needing attention", () => {
    renderScreen(testQueryClient([runFailed("plan")]));

    expect(screen.getByTestId("attention-group-run-failed")).toBeTruthy();
    expect(screen.queryByTestId("attention-empty")).toBeNull();
  });

  it("leaves out a kind with nothing in it", () => {
    renderScreen(testQueryClient([failed()]));

    expect(screen.queryByTestId("attention-group-waiting")).toBeNull();
    expect(screen.getByTestId("attention-group-failed")).toBeTruthy();
  });

  it("offers no run link for a failure with no run on record", () => {
    renderScreen(testQueryClient([failed({ run: null })]));

    expect(within(screen.getByTestId("attention-row-tpl_cdn")).queryByRole("link", { name: "View run" })).toBeNull();
  });

  // As the stack's own pages name it, so the two never disagree.
  it("names a template with no display name by its workspace", () => {
    renderScreen(testQueryClient([failed({ stack_template: { id: "tpl_cdn", workspace_name: "ws_cdn", display_name: "" } })]));

    expect(within(screen.getByTestId("attention-row-tpl_cdn")).getByRole("link", { name: "Edge CDN / ws_cdn" })).toBeTruthy();
  });

  it("says nothing needs attention, with a way back, when the list is empty", () => {
    renderScreen(testQueryClient([]));

    const empty = within(screen.getByTestId("attention-empty"));
    expect(empty.getByRole("heading", { level: 2, name: "Nothing needs attention" })).toBeTruthy();
    expect(empty.getByRole("link", { name: "Back to stacks" }).getAttribute("href")).toBe("/stacks");
  });

  it("offers a retry when the list fails to load", async () => {
    let calls = 0;
    vi.spyOn(globalThis, "fetch").mockImplementation(() => {
      calls += 1;
      return calls === 1
        ? Promise.reject(new TypeError("network down"))
        : Promise.resolve(new Response("[]", { status: 200, headers: { "content-type": "application/json" } }));
    });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("attention-error")).toBeTruthy());
    fireEvent.click(screen.getByTestId("attention-retry"));
    await waitFor(() => expect(screen.getByTestId("attention-empty")).toBeTruthy());
  });
});

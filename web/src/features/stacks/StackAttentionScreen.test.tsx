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
    stack_template: { id: "tpl_eks", display_name: "eks-cluster" },
    run: {
      id: "run_7",
      run_number: 7,
      trigger_actor: "user_priya",
      trigger_actor_display_name: "Priya Shah",
      plan_summary: { add: 2, change: 1, destroy: 0 }
    } as TemplateRun,
    ...overrides
  };
}

function failed(overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    kind: "destroy_failed",
    at: "2026-09-29T08:00:00Z",
    stack: { id: "stack_edge", name: "Edge CDN", slug: "edge-cdn" },
    stack_template: { id: "tpl_cdn", display_name: "cloudfront" },
    run: { id: "run_3", run_number: 3 } as TemplateRun,
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
  me: { sub: "user_1", tenantID: TENANT, displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
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

  it("titles the page under a trail back to the stacks, with the number of items", () => {
    renderScreen(testQueryClient([waiting(), failed()]));

    expect(screen.getByRole("heading", { level: 1, name: "Needs attention" })).toBeTruthy();
    expect(screen.getByTestId("page-count").textContent).toBe("2");
    const trail = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(trail.getByRole("link", { name: "Stacks" }).getAttribute("href")).toBe("/stacks");
  });

  it("lists waiting plans with who planned them, what they change and a link to review", () => {
    renderScreen(testQueryClient([waiting(), failed()]));

    const group = within(screen.getByTestId("attention-group-waiting"));
    expect(group.getByRole("heading", { level: 2 }).textContent).toBe("Waiting for approval1");
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
    expect(group.getByRole("heading", { level: 2 }).textContent).toBe("Destroy failed1");
    const row = within(screen.getByTestId("attention-row-tpl_cdn"));
    expect(row.getByText("The destroy run stopped before it finished. Some resources may still exist.")).toBeTruthy();
    expect(row.getByRole("link", { name: "View run" }).getAttribute("href")).toBe("/stacks/stack_edge/templates/tpl_cdn/runs/3");
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

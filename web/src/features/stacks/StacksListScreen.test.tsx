// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import StacksListScreen from "./StacksListScreen";
import { formatDateTime, formatTimestamp } from "../../shared/formatTimestamp";
import { queryKeys } from "../../api/queryKeys";
import type { AttentionItem, StackListItem, StackTemplate, StackView, TemplateRun } from "../../api/types";

const TENANT = "tenant_123";

function stack(overrides: Partial<StackListItem> = {}): StackListItem {
  return {
    id: "stack_1",
    tenant_id: TENANT,
    name: "Payments",
    slug: "payments",
    tags: {},
    default_credential_ids: [],
    created_by: "user_123",
    created_at: "2026-07-19T00:00:00Z",
    effectiveCapabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false },
    template_count: 0,
    ...overrides
  };
}

function stackTemplate(overrides: Partial<StackTemplate> = {}): StackTemplate {
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
    created_by: "user_123",
    lifecycle: "active",
    ...overrides
  };
}

function waitingItem(overrides: Partial<AttentionItem> = {}): AttentionItem {
  return {
    kind: "waiting_approval",
    at: "2026-10-03T09:30:00Z",
    stack: { id: "stack_1", name: "Payments", slug: "payments" },
    stack_template: { id: "tpl_1", display_name: "eks-cluster" },
    run: { id: "run_1", run_number: 7, plan_summary: { add: 2, change: 1, destroy: 0 } } as TemplateRun,
    ...overrides
  };
}

// staleTime: Infinity keeps seeded cache data from triggering a background
// refetch on mount — see the identical rationale in useStackCapabilities.test.tsx.
function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function seed(queryClient: QueryClient, data: { stacks?: StackListItem[]; attention?: AttentionItem[]; views?: StackView[] }) {
  if (data.stacks) queryClient.setQueryData(queryKeys.stacks(TENANT), data.stacks);
  queryClient.setQueryData(queryKeys.attention(TENANT), data.attention ?? []);
  for (const view of data.views ?? []) {
    queryClient.setQueryData(queryKeys.stack(TENANT, view.stack.id), view);
  }
}

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: TENANT, displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: true, canPublishTemplate: true } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides
  };
}

function renderScreen(queryClient: QueryClient, options: { auth?: AuthContextValue; path?: string } = {}) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={options.auth ?? authValue()}>
        <MemoryRouter initialEntries={[options.path ?? "/stacks"]}>
          <StacksListScreen />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

/** Answers each API path with its own response; anything else never settles. */
function mockAPI(routes: Record<string, () => Promise<Response>>) {
  return vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
    const url = new URL(String(input instanceof Request ? input.url : input), "http://localhost");
    const route = routes[url.pathname.replace(`/v1/tenants/${TENANT}`, "")];
    return route ? route() : new Promise(() => {});
  });
}

describe("StacksListScreen", () => {
  beforeEach(() => {
    // Queries a test does not seed stay pending rather than reaching a network.
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("shows a loading state while the stacks query is pending", () => {
    renderScreen(testQueryClient());

    expect(screen.getByTestId("stacks-list-loading")).toBeTruthy();
  });

  it("titles the page with the number of stacks", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing" })] });

    renderScreen(queryClient);

    expect(screen.getByRole("heading", { level: 1, name: "Stacks" })).toBeTruthy();
    expect(screen.getByTestId("page-count").textContent).toBe("2");
  });

  it("lists each stack with its slug and template count, selecting it in place", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack({ template_count: 4 }), stack({ id: "stack_2", name: "Billing", slug: "billing" })]
    });

    renderScreen(queryClient);

    const list = within(screen.getByRole("list", { name: "Stacks" }));
    const rows = list.getAllByRole("link");
    expect(rows.map((row) => row.getAttribute("href"))).toEqual(["/stacks?stack=stack_1", "/stacks?stack=stack_2"]);
    expect(rows[0].textContent).toContain("payments·4 templates");
    expect(rows[1].textContent).toContain("billing·no templates");
  });

  it("selects the first stack when the URL names none", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing" })] });

    renderScreen(queryClient);

    expect(screen.getByTestId("stack-row-stack_1").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("stack-row-stack_2").getAttribute("aria-current")).toBeNull();
    const preview = within(screen.getByTestId("stack-preview"));
    expect(preview.getByRole("heading", { level: 2, name: "Payments" })).toBeTruthy();
    expect(preview.getByTestId("stack-preview-open").getAttribute("href")).toBe("/stacks/stack_1");
  });

  it("selects the stack the URL names", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing" })] });

    renderScreen(queryClient, { path: "/stacks?stack=stack_2" });

    expect(screen.getByTestId("stack-row-stack_2").getAttribute("aria-current")).toBe("true");
    expect(within(screen.getByTestId("stack-preview")).getByRole("heading", { level: 2, name: "Billing" })).toBeTruthy();
  });

  it("falls back to the first stack when the URL names one that is not listed", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient, { path: "/stacks?stack=stack_gone" });

    expect(screen.getByTestId("stack-row-stack_1").getAttribute("aria-current")).toBe("true");
  });

  it("selects another stack when its row is chosen", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing" })] });

    renderScreen(queryClient);
    fireEvent.click(screen.getByTestId("stack-row-stack_2"));

    expect(screen.getByTestId("stack-row-stack_2").getAttribute("aria-current")).toBe("true");
    expect(within(screen.getByTestId("stack-preview")).getByRole("heading", { level: 2, name: "Billing" })).toBeTruthy();
  });

  it("filters the list by name or slug as the person types", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing-eu" })] });

    renderScreen(queryClient);
    fireEvent.change(screen.getByRole("searchbox", { name: "Filter stacks" }), { target: { value: "EU" } });

    expect(screen.queryByTestId("stack-row-stack_1")).toBeNull();
    expect(screen.getByTestId("stack-row-stack_2")).toBeTruthy();

    fireEvent.change(screen.getByRole("searchbox", { name: "Filter stacks" }), { target: { value: "nothing" } });
    expect(screen.getByTestId("stacks-filter-empty").textContent).toBe("No stacks match this filter.");
  });

  it("says what needs attention in one line that links to the attention page", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack(), stack({ id: "stack_2", name: "Edge", slug: "edge" })],
      attention: [
        waitingItem(),
        waitingItem({ kind: "destroy_failed", stack: { id: "stack_2", name: "Edge", slug: "edge" }, stack_template: { id: "tpl_9", display_name: "cdn" } })
      ]
    });

    renderScreen(queryClient);

    const summary = screen.getByTestId("attention-summary");
    expect(summary.getAttribute("href")).toBe("/stacks/attention");
    expect(summary.textContent).toBe("2 stacks need attention·1 plan waiting for approval, 1 destroy failed");
  });

  it("shows no summary line when nothing needs attention", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient);

    expect(screen.queryByTestId("attention-summary")).toBeNull();
  });

  it("marks the rows of stacks that need attention, and only those", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack(), stack({ id: "stack_2", name: "Edge", slug: "edge" }), stack({ id: "stack_3", name: "Quiet", slug: "quiet" })],
      attention: [
        waitingItem(),
        waitingItem({ kind: "destroy_failed", stack: { id: "stack_2", name: "Edge", slug: "edge" }, stack_template: { id: "tpl_9", display_name: "cdn" } })
      ]
    });

    renderScreen(queryClient);

    expect(screen.getByTestId("stack-row-stack_1").textContent).toContain("1 plan to approve");
    expect(screen.getByTestId("stack-row-stack_2").textContent).toContain("destroy failed");
    const quiet = screen.getByTestId("stack-row-stack_3").textContent ?? "";
    expect(quiet).not.toContain("approve");
    expect(quiet).not.toContain("failed");
  });

  it("shows the selected stack's templates with their state, time and action", () => {
    const queryClient = testQueryClient();
    const listed = stack({ template_count: 3 });
    seed(queryClient, {
      stacks: [listed],
      attention: [waitingItem()],
      views: [
        {
          stack: listed,
          templates: [
            stackTemplate({ live_state: "differs", pending_plan_run_id: "run_1" }),
            stackTemplate({ id: "tpl_2", display_name: "network", live_state: "matches", last_applied_at: "2026-09-22T10:00:00Z" }),
            stackTemplate({ id: "tpl_3", display_name: "rds-postgres" })
          ]
        }
      ]
    });

    renderScreen(queryClient);

    const rows = within(screen.getByTestId("stack-preview-templates")).getAllByRole("row");
    const cells = rows.map((row) => within(row).getAllByRole("cell").map((cell) => cell.textContent));
    expect(cells).toEqual([
      ["eks-cluster", "waiting for approval", `Planned ${formatDateTime("2026-10-03T09:30:00Z")}`, "Review plan"],
      ["network", "applied", `Applied ${formatTimestamp("2026-09-22T10:00:00Z")}`, ""],
      ["rds-postgres", "not applied", "Never applied", ""]
    ]);
    expect(within(rows[0]).getByRole("link", { name: "eks-cluster" }).getAttribute("href")).toBe("/stacks/stack_1/templates/tpl_1");
    expect(within(rows[0]).getByRole("link", { name: "Review plan" }).getAttribute("href")).toBe("/stacks/stack_1/templates/tpl_1/runs/7");
  });

  // openplan UI's ResourceRow is at least 52px tall.
  it("draws each template row at least 52px tall", () => {
    const queryClient = testQueryClient();
    const listed = stack();
    seed(queryClient, { stacks: [listed], views: [{ stack: listed, templates: [stackTemplate()] }] });

    renderScreen(queryClient);

    const [row] = within(screen.getByTestId("stack-preview-templates")).getAllByRole("row");
    expect(row.classList).toContain("h-13");
  });

  it("fills the chips with the soft signal colours", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack(), stack({ id: "stack_2", name: "Edge", slug: "edge" })],
      attention: [
        waitingItem(),
        waitingItem({ kind: "destroy_failed", stack: { id: "stack_2", name: "Edge", slug: "edge" }, stack_template: { id: "tpl_9", display_name: "cdn" } })
      ]
    });

    renderScreen(queryClient);

    const waitingChip = within(screen.getByTestId("stack-row-stack_1")).getByText("1 plan to approve");
    expect(waitingChip.classList).toContain("bg-warning-soft");
    expect(waitingChip.classList).toContain("rounded-sm");
    expect(within(screen.getByTestId("stack-row-stack_2")).getByText("destroy failed").classList).toContain("bg-destructive-soft");
  });

  it("says so when the selected stack has no templates", () => {
    const queryClient = testQueryClient();
    const listed = stack();
    seed(queryClient, { stacks: [listed], views: [{ stack: listed, templates: [] }] });

    renderScreen(queryClient);

    expect(screen.getByTestId("stack-preview-empty").textContent).toContain("No templates in this stack yet");
  });

  it("links the preview to Environment and Access only for someone who manages access", () => {
    const queryClient = testQueryClient();
    const viewer = stack();
    const manager = stack({ id: "stack_2", name: "Billing", slug: "billing", effectiveCapabilities: { canView: true, canOperate: true, canApprove: true, canManageAccess: true } });
    seed(queryClient, { stacks: [viewer, manager], views: [{ stack: viewer, templates: [] }, { stack: manager, templates: [] }] });

    renderScreen(queryClient);
    const sections = () => within(screen.getByRole("navigation", { name: "Stack sections" })).getAllByRole("link").map((link) => link.textContent);
    expect(sections()).toEqual(["Templates"]);

    fireEvent.click(screen.getByTestId("stack-row-stack_2"));
    expect(sections()).toEqual(["Templates", "Environment", "Access"]);
  });

  it("says so, under a heading, when there are no stacks", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [] });

    renderScreen(queryClient);

    const empty = screen.getByTestId("stacks-list-empty");
    expect(empty.getAttribute("data-slot")).toBe("empty");
    expect(within(empty).getByRole("heading", { level: 2, name: "No stacks yet" })).toBeTruthy();
    expect(within(empty).getByText("No stacks visible to you yet.")).toBeTruthy();
    expect(screen.queryByTestId("stacks-list")).toBeNull();
  });

  // Every control takes a 44px target on a coarse pointer.
  it("gives the page's controls a 44px target on coarse pointers", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient);

    expect(screen.getByTestId("create-stack-link").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByTestId("stack-preview-open").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByTestId("stack-row-stack_1").classList).toContain("min-h-14");
  });

  it("gives the retry button a 44px target on coarse pointers", async () => {
    mockAPI({ "/stacks": () => Promise.reject(new TypeError("network down")) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-retry").classList).toContain("pointer-coarse:h-11"));
  });

  it("renders the shared boundary screen for a handled API error status", async () => {
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ error: "unavailable", message: "service unavailable" }, 503)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-service-unavailable")).toBeTruthy());
  });

  it("renders the generic error state when the API returns 401", async () => {
    // The 401 drives client.ts's fetchWithAuth to navigate via
    // globalThis.location.assign; stub it so jsdom does not attempt (and
    // warn about) a real navigation.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ error: "unauthorized", message: "unauthorized" }, 401)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-error")).toBeTruthy());
  });

  it("renders AccessDenied when the API returns 403", async () => {
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ error: "forbidden", message: "forbidden" }, 403)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });

  it("renders NotFound when the API returns 404", async () => {
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ error: "not_found", message: "not found" }, 404)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-not-found")).toBeTruthy());
  });

  it("renders a retryable generic error state for unhandled failures", async () => {
    let calls = 0;
    mockAPI({
      "/stacks": () => {
        calls += 1;
        return calls === 1 ? Promise.reject(new TypeError("network down")) : Promise.resolve(jsonResponse([stack()]));
      },
      "/attention": () => Promise.resolve(jsonResponse([]))
    });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-error")).toBeTruthy());
    fireEvent.click(screen.getByTestId("stacks-list-retry"));
    await waitFor(() => expect(screen.getByTestId("stacks-list")).toBeTruthy());
    expect(calls).toBe(2);
  });

  it("still lists the stacks when the attention list fails", async () => {
    mockAPI({
      "/stacks": () => Promise.resolve(jsonResponse([stack()])),
      "/attention": () => Promise.reject(new TypeError("network down"))
    });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list")).toBeTruthy());
    expect(screen.queryByTestId("attention-summary")).toBeNull();
  });

  it("shows the create-stack action for a role with canCreateStack", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient);

    expect(screen.getByTestId("create-stack-link").getAttribute("href")).toBe("/stacks/new");
  });

  it("hides the create-stack action for a role without canCreateStack", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient, {
      auth: authValue({ me: { ...authValue().me!, globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } } })
    });

    expect(screen.getByTestId("stacks-list")).toBeTruthy();
    expect(screen.queryByTestId("create-stack-link")).toBeNull();
  });
});

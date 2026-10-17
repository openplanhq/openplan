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
import { asListBody, pagesOf } from "../../api/listTestSupport";

const TENANT = "tenant_123";

function stack(overrides: Partial<StackListItem> = {}): StackListItem {
  return {
    id: "stack_1",
    tenant_id: TENANT,
    name: "Payments",
    slug: "payments",
    slug_is_derived: true,
    tags: {},
    default_credential_ids: [],
    created_by: "user_123",
    created_at: "2026-07-19T00:00:00Z",
    effective_capabilities: { can_view: true, can_operate: false, can_approve: false, can_manage_access: false },
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
    stack_template: { id: "tpl_1", workspace_name: "ws_1", display_name: "eks-cluster" },
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
  if (data.stacks) queryClient.setQueryData(queryKeys.stackList(TENANT, ""), pagesOf(data.stacks));
  queryClient.setQueryData(queryKeys.attention(TENANT), data.attention ?? []);
  for (const view of data.views ?? []) {
    queryClient.setQueryData(queryKeys.stack(TENANT, view.stack.id), view);
  }
}

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenant_id: TENANT, display_name: "Test User", global_capabilities: { can_create_stack: true, can_publish_template: true } },
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
  return new Response(JSON.stringify(asListBody(body)), { status, headers: { "content-type": "application/json" } });
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

  // The header does not wait for the list, so nothing jumps when it lands.
  it("shows a loading state under the page's header while the stacks query is pending", () => {
    renderScreen(testQueryClient());

    expect(screen.getByTestId("stacks-list-loading").textContent).toContain("Loading stacks…");
    expect(screen.getByRole("heading", { level: 1, name: "Stacks" })).toBeTruthy();
    expect(screen.getByTestId("create-stack-link")).toBeTruthy();
    expect(screen.queryByTestId("page-count")).toBeNull();
  });

  it("says the stacks failed to load under the page's header, with an outline Retry", async () => {
    mockAPI({ "/stacks": () => Promise.reject(new TypeError("network down")) });

    renderScreen(testQueryClient());

    const error = await screen.findByTestId("stacks-list-error");
    expect(within(error).getByRole("alert").textContent).toBe("Something went wrong while loading stacks.");
    expect(screen.getByRole("heading", { level: 1, name: "Stacks" })).toBeTruthy();
    // An outline button, not the page's second primary one.
    const retry = screen.getByTestId("stacks-list-retry");
    expect(retry.classList).toContain("bg-background");
    expect(retry.classList).not.toContain("bg-primary");
  });

  it("titles the page with the number of stacks", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing" })] });

    renderScreen(queryClient);

    expect(screen.getByRole("heading", { level: 1, name: "Stacks" })).toBeTruthy();
    expect(screen.getByTestId("page-count").textContent).toBe("2");
  });

  it("lists each stack with its template count, selecting it in place", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack({ template_count: 4 }), stack({ id: "stack_2", name: "Billing", slug: "billing" })]
    });

    renderScreen(queryClient);

    const list = within(screen.getByRole("list", { name: "Stacks" }));
    const rows = list.getAllByRole("link");
    expect(rows.map((row) => row.getAttribute("href"))).toEqual(["/stacks?stack=stack_1", "/stacks?stack=stack_2"]);
    const [first, second] = list.getAllByTestId("stack-row-templates");
    expect(first.textContent).toBe("4 templates");
    // The number stands apart from the word, in the foreground colour.
    expect(within(first).getByText("4").className).toContain("text-foreground");
    expect(second.textContent).toBe("No templates");
  });

  it("shows a slug only when someone chose it", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack(), stack({ id: "stack_2", name: "Shared networking", slug: "net-core", slug_is_derived: false })]
    });

    renderScreen(queryClient);

    // A slug the name gives would only repeat it.
    expect(screen.getByTestId("stack-row-stack_1").textContent).not.toContain("payments");
    const chosen = within(screen.getByTestId("stack-row-stack_2"));
    expect(chosen.getByText("net-core").getAttribute("title")).toBe("net-core");
    // With a slug after it the name stops short of the line, so the slug is cut first.
    expect(chosen.getByText("Shared networking").className).toContain("max-w-beside-slug");
    expect(within(screen.getByTestId("stack-row-stack_1")).getByText("Payments").className).toContain("max-w-full");
  });

  it("draws an empty count in subtle grey, but muted on the selected row", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing" })] });

    renderScreen(queryClient);

    const [selected, other] = screen.getAllByTestId("stack-row-templates");
    // Subtle grey on the selected row's fill would fall under 4.5:1.
    expect(selected.className).toContain("text-muted-foreground");
    expect(other.className).toContain("text-subtle-foreground");
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

  it("shows the preview's slug only when someone chose it, and its tags either way", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [
        stack(),
        stack({ id: "stack_2", name: "Billing", slug: "billing", tags: { env: "prod" } }),
        stack({ id: "stack_3", name: "Shared networking", slug: "net-core", slug_is_derived: false })
      ]
    });

    const first = renderScreen(queryClient);
    // Neither a chosen slug nor tags: the line under the name goes.
    const header = screen.getByRole("heading", { level: 2, name: "Payments" }).parentElement!;
    expect(header.children).toHaveLength(1);
    first.unmount();

    renderScreen(queryClient, { path: "/stacks?stack=stack_2" });
    const billing = within(screen.getByTestId("stack-preview"));
    expect(billing.getByText("env: prod")).toBeTruthy();
    expect(billing.queryByText("billing")).toBeNull();
    cleanup();

    renderScreen(queryClient, { path: "/stacks?stack=stack_3" });
    expect(within(screen.getByTestId("stack-preview")).getByText("net-core")).toBeTruthy();
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

  // The filter goes to the server as q, once typing pauses, so it finds a
  // stack on a page not yet loaded.
  it("filters the list on the server by what the person types", async () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Billing", slug: "billing-eu" })] });
    const fetchMock = mockAPI({
      "/stacks": async () => jsonResponse([stack({ id: "stack_2", name: "Billing", slug: "billing-eu" })])
    });

    renderScreen(queryClient);
    fireEvent.change(screen.getByRole("searchbox", { name: "Filter stacks" }), { target: { value: " EU " } });

    await waitFor(() => expect(screen.queryByTestId("stack-row-stack_1")).toBeNull());
    expect(screen.getByTestId("stack-row-stack_2")).toBeTruthy();
    const asked = fetchMock.mock.calls.map(([input]) => new URL(String(input), "http://localhost"));
    expect(asked.find((url) => url.pathname.endsWith("/stacks"))?.searchParams.get("q")).toBe("EU");
  });

  it("says when nothing matches the filter, and previews nothing", async () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });
    queryClient.setQueryData(queryKeys.stackList(TENANT, "nothing"), pagesOf([]));

    renderScreen(queryClient);
    fireEvent.change(screen.getByRole("searchbox", { name: "Filter stacks" }), { target: { value: "nothing" } });

    expect((await screen.findByTestId("stacks-filter-empty")).textContent).toBe("No stacks match this filter.");
    expect(screen.queryByTestId("stack-preview")).toBeNull();
    // Not the first-install empty state: the tenant has stacks.
    expect(screen.queryByTestId("stacks-list-empty")).toBeNull();
  });

  it("previews a stack the filter shows, never one it hides", async () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack(), stack({ id: "stack_2", name: "Edge CDN", slug: "edge" })] });
    queryClient.setQueryData(queryKeys.stackList(TENANT, "edge"), pagesOf([stack({ id: "stack_2", name: "Edge CDN", slug: "edge" })]));

    renderScreen(queryClient, { path: "/stacks?stack=stack_1" });
    fireEvent.change(screen.getByRole("searchbox", { name: "Filter stacks" }), { target: { value: "edge" } });

    await waitFor(() => expect(screen.getByTestId("stack-row-stack_2").getAttribute("aria-current")).toBe("true"));
    expect(within(screen.getByTestId("stack-preview")).getByRole("heading", { level: 2, name: "Edge CDN" })).toBeTruthy();

    fireEvent.change(screen.getByRole("searchbox", { name: "Filter stacks" }), { target: { value: "" } });
    await waitFor(() => expect(screen.getByTestId("stack-row-stack_1").getAttribute("aria-current")).toBe("true"));
  });

  // The list comes a page at a time; Load more appends the next, and goes
  // once the last is in.
  it("loads the next page of stacks under the list", async () => {
    const queryClient = testQueryClient();
    const fetchMock = mockAPI({
      "/stacks": async () => jsonResponse({ items: [stack({ id: "stack_2", name: "Billing", slug: "billing" })], next_cursor: null })
    });
    queryClient.setQueryData(queryKeys.stackList(TENANT, ""), {
      pages: [{ items: [stack()], next_cursor: "page-2" }],
      pageParams: [undefined]
    });

    renderScreen(queryClient);
    fireEvent.click(screen.getByTestId("stacks-load-more"));

    expect(await screen.findByTestId("stack-row-stack_2")).toBeTruthy();
    expect(screen.getByTestId("stack-row-stack_1")).toBeTruthy();
    expect(screen.queryByTestId("stacks-load-more")).toBeNull();
    const asked = fetchMock.mock.calls.map(([input]) => new URL(String(input), "http://localhost")).filter((url) => url.pathname.endsWith("/stacks"));
    expect(asked.map((url) => url.searchParams.get("cursor"))).toEqual(["page-2"]);
  });

  // There is no total to ask for, so the header counts the stacks only once
  // every page is in, rather than counting the ones loaded so far.
  it("counts the stacks in the header only once every page is loaded", async () => {
    const queryClient = testQueryClient();
    mockAPI({ "/stacks": async () => jsonResponse({ items: [stack({ id: "stack_2", name: "Billing", slug: "billing" })], next_cursor: null }) });
    queryClient.setQueryData(queryKeys.stackList(TENANT, ""), {
      pages: [{ items: [stack()], next_cursor: "page-2" }],
      pageParams: [undefined]
    });

    renderScreen(queryClient);
    expect(screen.queryByTestId("page-count")).toBeNull();

    fireEvent.click(screen.getByTestId("stacks-load-more"));
    await waitFor(() => expect(screen.getByTestId("page-count").textContent).toBe("2"));
  });

  it("keeps the stacks shown when the next page fails, and offers to try again", async () => {
    const queryClient = testQueryClient();
    mockAPI({ "/stacks": async () => jsonResponse({ type: "about:blank", title: "Internal Server Error", status: 500 }, 500) });
    queryClient.setQueryData(queryKeys.stackList(TENANT, ""), {
      pages: [{ items: [stack()], next_cursor: "page-2" }],
      pageParams: [undefined]
    });

    renderScreen(queryClient);
    fireEvent.click(screen.getByTestId("stacks-load-more"));

    expect(await screen.findByText("More stacks couldn't be loaded.")).toBeTruthy();
    expect(screen.getByTestId("stack-row-stack_1")).toBeTruthy();
    expect(screen.getByTestId("stacks-load-more").textContent).toBe("Try again");
  });

  it("says what needs attention in one line that links to the attention page", () => {
    const queryClient = testQueryClient();
    seed(queryClient, {
      stacks: [stack(), stack({ id: "stack_2", name: "Edge", slug: "edge" })],
      attention: [
        waitingItem(),
        waitingItem({ kind: "destroy_failed", stack: { id: "stack_2", name: "Edge", slug: "edge" }, stack_template: { id: "tpl_9", workspace_name: "ws_9", display_name: "cdn" } })
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
        waitingItem({ kind: "destroy_failed", stack: { id: "stack_2", name: "Edge", slug: "edge" }, stack_template: { id: "tpl_9", workspace_name: "ws_9", display_name: "cdn" } })
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
        waitingItem({ kind: "destroy_failed", stack: { id: "stack_2", name: "Edge", slug: "edge" }, stack_template: { id: "tpl_9", workspace_name: "ws_9", display_name: "cdn" } })
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

  it("puts Environment and Access in the preview's header only for someone who manages access", () => {
    const queryClient = testQueryClient();
    const viewer = stack();
    const manager = stack({ id: "stack_2", name: "Billing", slug: "billing", effective_capabilities: { can_view: true, can_operate: true, can_approve: true, can_manage_access: true } });
    seed(queryClient, { stacks: [viewer, manager], views: [{ stack: viewer, templates: [] }, { stack: manager, templates: [] }] });

    renderScreen(queryClient);
    const actions = () =>
      within(screen.getByTestId("stack-preview-actions"))
        .getAllByRole("link")
        .map((link) => [link.textContent, link.getAttribute("href")]);
    // No section nav: its Templates went where Open stack already goes.
    expect(screen.queryByRole("navigation", { name: "Stack sections" })).toBeNull();
    expect(actions()).toEqual([["Open stack", "/stacks/stack_1"]]);

    fireEvent.click(screen.getByTestId("stack-row-stack_2"));
    expect(actions()).toEqual([
      ["Environment", "/stacks/stack_2/environment"],
      ["Access", "/stacks/stack_2/access"],
      ["Open stack", "/stacks/stack_2"]
    ]);
  });

  it("says so, under a heading, when there are no stacks", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [] });

    renderScreen(queryClient);

    const empty = screen.getByTestId("stacks-list-empty");
    expect(empty.getAttribute("data-slot")).toBe("empty");
    expect(within(empty).getByRole("heading", { level: 2, name: "No stacks yet" })).toBeTruthy();
    expect(screen.queryByTestId("stacks-list")).toBeNull();
  });

  // A new install opens here: the empty state says what a stack is for and
  // offers the one action that makes one.
  it("offers Create stack in the empty state to someone who may create one", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [] });

    renderScreen(queryClient);

    const empty = screen.getByTestId("stacks-list-empty");
    expect(within(empty).getByText("Create a stack for each environment you deploy to, such as prod or staging.")).toBeTruthy();
    const create = within(empty).getByRole("link", { name: "Create stack" });
    expect(create.getAttribute("href")).toBe("/stacks/new");
    // An outline button: the header's Create stack is the page's one primary.
    expect(create.classList).toContain("bg-background");
  });

  it("tells someone who may not create a stack how one reaches them, with no action", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [] });

    renderScreen(queryClient, {
      auth: authValue({ me: { sub: "user_1", tenant_id: TENANT, display_name: "Test User", global_capabilities: { can_create_stack: false, can_publish_template: false } } })
    });

    const empty = screen.getByTestId("stacks-list-empty");
    expect(within(empty).getByText("Stacks appear here once someone gives you access to one.")).toBeTruthy();
    expect(within(empty).queryByRole("link")).toBeNull();
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
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ type: "about:blank", title: "Service Unavailable", detail: "service unavailable" }, 503)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-service-unavailable")).toBeTruthy());
  });

  it("renders the generic error state when the API returns 401", async () => {
    // The 401 drives client.ts's fetchWithAuth to navigate via
    // globalThis.location.assign; stub it so jsdom does not attempt (and
    // warn about) a real navigation.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ type: "about:blank", title: "Unauthorized", detail: "unauthorized" }, 401)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-error")).toBeTruthy());
  });

  it("renders AccessDenied when the API returns 403", async () => {
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ type: "about:blank", title: "Forbidden", detail: "forbidden" }, 403)) });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });

  it("renders NotFound when the API returns 404", async () => {
    mockAPI({ "/stacks": () => Promise.resolve(jsonResponse({ type: "about:blank", title: "Not Found", detail: "not found" }, 404)) });

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

  it("shows the create-stack action for a role with can_create_stack", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient);

    expect(screen.getByTestId("create-stack-link").getAttribute("href")).toBe("/stacks/new");
  });

  it("hides the create-stack action for a role without can_create_stack", () => {
    const queryClient = testQueryClient();
    seed(queryClient, { stacks: [stack()] });

    renderScreen(queryClient, {
      auth: authValue({ me: { ...authValue().me!, global_capabilities: { can_create_stack: false, can_publish_template: false } } })
    });

    expect(screen.getByTestId("stacks-list")).toBeTruthy();
    expect(screen.queryByTestId("create-stack-link")).toBeNull();
  });
});

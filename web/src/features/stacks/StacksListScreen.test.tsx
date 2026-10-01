// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import StacksListScreen from "./StacksListScreen";
import { queryKeys } from "../../api/queryKeys";
import type { Stack } from "../../api/types";

function stack(overrides: Partial<Stack> = {}): Stack {
  return {
    id: "stack_1",
    tenant_id: "tenant_123",
    name: "Payments",
    slug: "payments",
    tags: {},
    default_credential_ids: [],
    created_by: "user_123",
    created_at: "2026-07-19T00:00:00Z",
    effectiveCapabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false },
    ...overrides
  };
}

// staleTime: Infinity keeps seeded cache data from triggering a background
// refetch on mount — see the identical rationale in useStackCapabilities.test.tsx.
function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: true, canPublishTemplate: true } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides,
  };
}

function renderScreen(queryClient: QueryClient, auth?: AuthContextValue) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth ?? authValue()}>
        <MemoryRouter initialEntries={["/stacks"]}>
          <StacksListScreen />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

describe("StacksListScreen", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
  });

  it("shows a loading state while the stacks query is pending", () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderScreen(testQueryClient());

    expect(screen.getByTestId("stacks-list-loading")).toBeTruthy();
  });

  it("renders the stacks returned by the API as links to their detail routes", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [
      stack(),
      stack({ id: "stack_2", name: "Billing", slug: "billing" })
    ]);

    renderScreen(queryClient);

    const list = screen.getByTestId("stacks-list");
    expect(list.textContent).toContain("Payments");
    expect(list.textContent).toContain("Billing");
    const hrefs = screen.getAllByRole("link").map((link) => link.getAttribute("href"));
    expect(hrefs).toContain("/stacks/stack_1");
    expect(hrefs).toContain("/stacks/stack_2");
  });

  it("renders an empty state when the API returns no visible stacks", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), []);

    renderScreen(queryClient);

    expect(screen.getByTestId("stacks-list-empty")).toBeTruthy();
  });

  it("lists the stacks in a table, one row each, name then slug", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [
      stack(),
      stack({ id: "stack_2", name: "Billing", slug: "billing" })
    ]);

    renderScreen(queryClient);

    const table = screen.getByRole("table");
    expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Name", "Slug"]);
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell").map((cell) => cell.textContent))).toEqual([
      ["Payments", "payments"],
      ["Billing", "billing"]
    ]);
    expect(within(rows[0]).getByRole("link", { name: "Payments" }).getAttribute("href")).toBe("/stacks/stack_1");
  });

  // Fixed layout keeps each column's width whatever a cell holds. A name or
  // slug longer than its column ends in an ellipsis, and hovering shows it
  // whole.
  it("cuts a long name or slug to its column and shows it whole on hover", () => {
    const name = "payments-core-production-eu-west-1-and-then-some";
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack({ name, slug: name })]);

    renderScreen(queryClient);

    const link = screen.getByRole("link", { name });
    expect(link.getAttribute("title")).toBe(name);
    expect(link.classList).toContain("truncate");
    const slug = within(screen.getByRole("table")).getAllByRole("cell")[1];
    expect(slug.getAttribute("title")).toBe(name);
    expect(slug.classList).toContain("truncate");
  });

  it("declares the table's column widths, so a long name moves no column", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    renderScreen(queryClient);

    const table = screen.getByRole("table");
    expect(table.classList).toContain("table-fixed");
    expect(table.querySelectorAll("colgroup > col")).toHaveLength(2);
  });

  it("says so, under a heading, when there are no stacks", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), []);

    renderScreen(queryClient);

    const empty = screen.getByTestId("stacks-list-empty");
    expect(empty.getAttribute("data-slot")).toBe("empty");
    expect(within(empty).getByRole("heading", { level: 2, name: "No stacks yet" })).toBeTruthy();
    expect(within(empty).getByText("No stacks visible to you yet.")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  // Preflight leaves a heading with the body's type.
  it("sets the empty state's heading type itself", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), []);

    renderScreen(queryClient);

    const heading = screen.getByRole("heading", { level: 2 }).classList;
    for (const name of ["font-heading", "text-sm", "font-medium", "tracking-tight"]) {
      expect(heading).toContain(name);
    }
  });

  // Every control takes a 44px target on a coarse pointer. The row's link
  // fills its cell, so padding it out makes the whole cell 44px.
  it("gives every control a 44px target on coarse pointers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    renderScreen(queryClient);

    expect(screen.getByTestId("create-stack-link").classList).toContain("pointer-coarse:h-11");
    const link = screen.getByRole("link", { name: "Payments" }).classList;
    expect(link).toContain("block");
    expect(link).toContain("pointer-coarse:py-3");
  });

  it("gives the retry button a 44px target on coarse pointers", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("network down"));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-retry").classList).toContain("pointer-coarse:h-11"));
  });

  it("renders the shared boundary screen for a handled API error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "unavailable", message: "service unavailable" }, 503)
    );

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-service-unavailable")).toBeTruthy());
  });

  it("renders the generic error state when the API returns 401", async () => {
    // The 401 drives client.ts's fetchWithAuth to navigate via
    // globalThis.location.assign; stub it so jsdom does not attempt (and
    // warn about) a real navigation.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "unauthorized", message: "unauthorized" }, 401)
    );

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-error")).toBeTruthy());
  });

  it("renders AccessDenied when the API returns 403", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "forbidden", message: "forbidden" }, 403)
    );

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });

  it("renders NotFound when the API returns 404", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "not_found", message: "not found" }, 404)
    );

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-not-found")).toBeTruthy());
  });

  it("renders a retryable generic error state for unhandled failures", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockRejectedValueOnce(new TypeError("network down"))
      .mockResolvedValueOnce(jsonResponse([stack()]));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-error")).toBeTruthy());
    fireEvent.click(screen.getByTestId("stacks-list-retry"));
    await waitFor(() => expect(screen.getByTestId("stacks-list")).toBeTruthy());
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("shows the create-stack action for a role with canCreateStack", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    renderScreen(queryClient);

    expect(screen.getByTestId("create-stack-link").getAttribute("href")).toBe("/stacks/new");
  });

  it("hides the create-stack action for a role without canCreateStack", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    renderScreen(queryClient, authValue({ me: { ...authValue().me!, globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } } }));

    expect(screen.getByTestId("stacks-list")).toBeTruthy();
    expect(screen.queryByTestId("create-stack-link")).toBeNull();
  });
});

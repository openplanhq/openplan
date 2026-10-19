// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryRouter, MemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "./AuthContext";
import type { AuthContextValue } from "./AuthContext";
import RequireCapability from "./RequireCapability";
import { queryKeys } from "../api/queryKeys";
import type { StackView } from "../api/types";

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: {
      sub: "user_1",
      tenant_id: "tenant_123",
      display_name: "Test User",
      global_capabilities: { can_create_stack: false, can_publish_template: false, can_manage_platform_access: false }
    },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides
  };
}

function testQueryClient(): QueryClient {
  // staleTime: Infinity keeps seeded cache data from triggering a background
  // refetch on mount (the default staleTime: 0 would refetch stale data even
  // though it's already cached, defeating the "no additional fetch" assertions).
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function stackView(capabilities: Partial<StackView["stack"]["effective_capabilities"]>): StackView {
  return {
    stack: {
      id: "stack_1",
      tenant_id: "tenant_123",
      name: "Stack",
      slug: "stack",
      slug_is_derived: true,
      tags: {},
      default_credential_ids: [],
      created_by: "user_123",
      created_at: "2026-07-19T00:00:00Z",
      effective_capabilities: { can_view: false, can_operate: false, can_approve: false, can_manage_access: false, ...capabilities }
    },
    templates: []
  };
}

function seededClient(stackId: string, capabilities: Partial<StackView["stack"]["effective_capabilities"]>): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.stack("tenant_123", stackId), stackView(capabilities));
  return queryClient;
}

function renderGate(
  ui: ReactElement,
  { auth = authValue(), queryClient = testQueryClient() }: { auth?: AuthContextValue; queryClient?: QueryClient } = {}
): string {
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter>{ui}</MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

describe("RequireCapability — gate mode (component-level)", () => {
  it("renders children when the global capability is allowed", () => {
    const markup = renderGate(
      <RequireCapability capability="can_create_stack">
        <span data-testid="protected">shown</span>
      </RequireCapability>,
      { auth: authValue({ me: { ...authValue().me!, global_capabilities: { can_create_stack: true, can_publish_template: true, can_manage_platform_access: false } } }) }
    );

    expect(markup).toContain('data-testid="protected"');
  });

  it("fails closed (renders nothing) when denied and no fallback is given", () => {
    const markup = renderGate(
      <RequireCapability capability="can_create_stack">
        <span data-testid="protected">shown</span>
      </RequireCapability>
    );

    expect(markup).toBe("");
  });

  it("renders the fallback when denied and a fallback is given", () => {
    const markup = renderGate(
      <RequireCapability capability="can_create_stack" fallback={<span data-testid="reason">Requires operator role</span>}>
        <span data-testid="protected">shown</span>
      </RequireCapability>
    );

    expect(markup).toContain('data-testid="reason"');
    expect(markup).not.toContain('data-testid="protected"');
  });

  it("renders nothing while the global capability is loading", () => {
    const markup = renderGate(
      <RequireCapability capability="can_create_stack" fallback={<span data-testid="reason">shown</span>}>
        <span data-testid="protected">shown</span>
      </RequireCapability>,
      { auth: authValue({ status: "loading", me: null }) }
    );

    expect(markup).toBe("");
  });

  it("reads a stack capability from the query cache using an explicit stackId, with no fetch", () => {
    const markup = renderGate(
      <RequireCapability capability="can_operate" stackId="stack_1">
        <span data-testid="protected">shown</span>
      </RequireCapability>,
      { queryClient: seededClient("stack_1", { can_operate: true }) }
    );

    expect(markup).toContain('data-testid="protected"');
  });

  it("falls back to the current route's stackId param when none is given explicitly", () => {
    const testRouter = createMemoryRouter(
      [
        {
          path: "/stacks/:stackId",
          element: (
            <RequireCapability capability="can_operate">
              <span data-testid="protected">shown</span>
            </RequireCapability>
          )
        }
      ],
      { initialEntries: ["/stacks/stack_1"] }
    );
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={seededClient("stack_1", { can_operate: true })}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="protected"');
  });

  it("renders nothing while a stack capability is loading (no cached data yet)", () => {
    const markup = renderGate(
      <RequireCapability capability="can_operate" stackId="stack_1">
        <span data-testid="protected">shown</span>
      </RequireCapability>
    );

    expect(markup).toBe("");
  });
});

describe("RequireCapability — route mode", () => {
  function renderRoute(
    capability: "can_view" | "can_manage_access" | "can_create_stack",
    { auth = authValue(), queryClient = testQueryClient() }: { auth?: AuthContextValue; queryClient?: QueryClient } = {}
  ): string {
    const testRouter = createMemoryRouter(
      [
        {
          path: "/stacks/:stackId",
          element: <RequireCapability capability={capability} mode="route" />,
          children: [{ index: true, element: <span data-testid="protected-route">shown</span> }]
        }
      ],
      { initialEntries: ["/stacks/stack_1"] }
    );
    return renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={auth}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );
  }

  it("renders the routed content when allowed", () => {
    const markup = renderRoute("can_view", { queryClient: seededClient("stack_1", { can_view: true }) });

    expect(markup).toContain('data-testid="protected-route"');
  });

  it("renders NotFound, identical to a real 404, when canView is denied", () => {
    const markup = renderRoute("can_view", { queryClient: seededClient("stack_1", { can_view: false }) });

    expect(markup).toContain('data-testid="route-not-found"');
    expect(markup).not.toContain('data-testid="route-access-denied"');
  });

  it("renders AccessDenied, not NotFound, when a non-can_view capability is denied", () => {
    const markup = renderRoute("can_manage_access", { queryClient: seededClient("stack_1", { can_view: true, can_manage_access: false }) });

    expect(markup).toContain('data-testid="route-access-denied"');
    expect(markup).not.toContain('data-testid="route-not-found"');
  });

  it("renders nothing while loading (no cached stack data yet)", () => {
    const markup = renderRoute("can_view");

    expect(markup).toBe("");
  });
});

// What the stack's page shows when its stack never arrives. Before, each of
// these was a blank page: nothing said why, and nothing offered a way on.
describe("RequireCapability — a stack that does not load", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  function respond(status: number, body: unknown): Response {
    return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
  }

  function renderStackRoute(queryClient: QueryClient, auth: AuthContextValue = authValue()) {
    const testRouter = createMemoryRouter(
      [
        {
          path: "/stacks/:stackId",
          element: <RequireCapability capability="can_view" mode="route" />,
          children: [{ index: true, element: <span data-testid="protected-route">shown</span> }]
        }
      ],
      { initialEntries: ["/stacks/stack_1"] }
    );
    return render(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={auth}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );
  }

  it("signs the person out on a 401, with nothing drawn in the meantime", async () => {
    // A 401 sends client.ts's fetchWithAuth to the sign-in page through
    // location.assign; stub it so jsdom does not try to navigate.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => respond(401, { type: "about:blank", title: "Unauthorized", detail: "unauthorized" }));
    const logout = vi.fn();

    renderStackRoute(testQueryClient(), authValue({ logout }));

    await waitFor(() => expect(logout).toHaveBeenCalled());
    expect(screen.queryByTestId("stack-route-error")).toBeNull();
    expect(screen.queryByTestId("protected-route")).toBeNull();
  });

  // The app retries a failed query three times; a 404 answers the same on
  // each, so the page says so after the first rather than after the last.
  it("says the page is not found on the first 404, without waiting out the retries", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => respond(404, { type: "about:blank", title: "Not Found", detail: "not found" }));
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: 3, retryDelay: 60_000 } } });

    renderStackRoute(queryClient);

    expect(await screen.findByTestId("route-not-found")).toBeTruthy();
  });

  it("answers a stack the person may not see as not found, never as not permitted", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => respond(403, { type: "about:blank", title: "Forbidden", detail: "forbidden" }));

    renderStackRoute(testQueryClient());

    expect(await screen.findByTestId("route-not-found")).toBeTruthy();
    expect(screen.queryByTestId("route-access-denied")).toBeNull();
  });

  it("says the authorization service is unavailable on a 503", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => respond(503, { type: "about:blank", title: "Service Unavailable", detail: "unavailable" }));

    renderStackRoute(testQueryClient());

    expect(await screen.findByTestId("route-service-unavailable")).toBeTruthy();
    expect(screen.queryByTestId("protected-route")).toBeNull();
  });

  it("says the stack failed to load, and loads it on Retry", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(respond(500, { type: "about:blank", title: "Internal Server Error", detail: "boom" }))
      .mockResolvedValueOnce(respond(200, stackView({ can_view: true })));

    renderStackRoute(testQueryClient());

    const error = await screen.findByTestId("stack-route-error");
    expect(error.textContent).toContain("Something went wrong while loading this stack.");
    expect(screen.getByRole("alert")).toBeTruthy();

    fireEvent.click(screen.getByTestId("stack-route-retry"));

    expect(await screen.findByTestId("protected-route")).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});

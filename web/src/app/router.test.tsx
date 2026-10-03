import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../auth/AuthContext";
import type { AuthContextValue } from "../auth/AuthContext";

vi.mock("../auth/SessionProvider");

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides,
  };
}

// The stack every stack-route test below seeds: canView-gated routes resolve
// once the stack query cache has effectiveCapabilities. retry: false +
// staleTime: Infinity, so seeded data never triggers a real fetch().
function stackQueryClient(
  queryKeys: typeof import("../api/queryKeys").queryKeys,
  options: { capabilities?: { canView: boolean; canOperate: boolean; canApprove: boolean; canManageAccess: boolean }; templates?: unknown[] } = {}
): QueryClient {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
    stack: {
      id: "stack_1",
      tenant_id: "tenant_123",
      name: "Stack",
      slug: "stack",
      tags: {},
      default_credential_ids: [],
      created_by: "user_123",
      created_at: "2026-07-19T00:00:00Z",
      effectiveCapabilities: options.capabilities ?? { canView: true, canOperate: true, canApprove: true, canManageAccess: true }
    },
    templates: options.templates ?? []
  });
  queryClient.setQueryData(queryKeys.attention("tenant_123"), []);
  return queryClient;
}

// The panel draws its outlet only for a template the stack has.
const installedTemplate = {
  id: "st_1",
  stack_id: "stack_1",
  component_key: "vpc",
  source_template_id: "tmpl_src_1",
  desired_template_revision_id: "rev_1",
  last_applied_template_revision_id: "",
  source_ref: "main",
  workspace_name: "ws",
  display_name: "",
  config: {},
  last_applied_run_id: "",
  pending_plan_run_id: "",
  plan_state: "none",
  live_state: "never",
  created_by: "user_123",
  lifecycle: "active"
};

describe("routeConfig", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  // "/" is not a screen. The design spec's route map has no row for it, but
  // a bare sign-in lands there, so it must redirect rather than render. The
  // redirect is a loader, so it resolves during router initialization —
  // before any element renders — which is what this asserts.
  it("redirects the index route to /stacks without rendering a screen", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/"] });
    await vi.waitFor(() => expect(testRouter.state.initialized).toBe(true));

    expect(testRouter.state.location.pathname).toBe("/stacks");
  });

  it("renders the create stack screen at /stacks/new", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    {
      const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/new"] });
      const markup = renderToStaticMarkup(
        <QueryClientProvider client={new QueryClient()}>
          <AuthContext.Provider value={authValue({ me: { ...authValue().me!, globalCapabilities: { isPlatformAdmin: false, canCreateStack: true, canPublishTemplate: true } } })}>
            <RouterProvider router={testRouter} />
          </AuthContext.Provider>
        </QueryClientProvider>
      );
      expect(markup).toContain("Create stack");
      expect(markup).not.toContain('data-testid="route-placeholder"');
    }
  });

  it("renders the stack's page at /stacks/:stackId", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { queryKeys } = await import("../api/queryKeys");
    const queryClient = stackQueryClient(queryKeys);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="stack-page"');
    expect(markup).toContain('data-testid="stack-empty"');
  });

  it("sends the old template list at /stacks/:stackId/templates to the stack's page", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/templates"] });
    await vi.waitFor(() => expect(testRouter.state.initialized).toBe(true));

    expect(testRouter.state.location.pathname).toBe("/stacks/stack_1");
  });

  it("renders Environment under a Stacks / stack / Environment breadcrumb", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { queryKeys } = await import("../api/queryKeys");
    const queryClient = stackQueryClient(queryKeys);
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/environment"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="stack-section-layout"');
    expect(markup).toContain('href="/stacks/stack_1"');
    expect(markup).toMatch(/<h1[^>]*aria-current="page"[^>]*>Environment<\/h1>/);
  });

  it("renders the stacks list screen at /stacks", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    // retry: false + staleTime: Infinity: this test seeds the cache directly and
    // must never let TanStack Query's default refetch-on-mount fire a real,
    // unmocked fetch() for stale data — see useStackCapabilities.test.tsx.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [
      {
        id: "stack_1",
        tenant_id: "tenant_123",
        name: "Payments",
        slug: "payments",
        tags: {},
        default_credential_ids: [],
        created_by: "user_123",
        created_at: "2026-07-19T00:00:00Z",
        effectiveCapabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false }
      }
    ]);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="stacks-list"');
    expect(markup).toContain("Payments");
    expect(markup).not.toContain('data-testid="route-placeholder"');
    // A redesigned page: its panels sit on the grey canvas.
    expect(markup).toContain('data-canvas="true"');
  });

  // "attention" is a static segment, so it is the attention page, never a
  // stack whose id is "attention".
  it("renders the attention screen at /stacks/attention", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.attention("tenant_123"), []);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/attention"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="attention-empty"');
    expect(markup).toContain('data-canvas="true"');
    expect(markup).not.toContain('data-testid="stack-detail-shell"');
  });

  it("renders the template registry screen at /templates", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    // retry: false + staleTime: Infinity: this test seeds the cache directly and
    // must never let TanStack Query's default refetch-on-mount fire a real,
    // unmocked fetch() for stale data — see useStackCapabilities.test.tsx.
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      {
        id: "rev_1",
        tenant_id: "tenant_123",
        source_template_id: "tpl_1",
        repo_owner: "hashicorp",
        repo_name: "terraform-aws-vpc",
        source_ref: "main",
        resolved_commit_sha: "abcdef1234567890",
        root_path: ".",
        name: "VPC",
        description: "",
        tags: [],
        status: "active"
      }
    ]);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/templates"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="templates-list"');
    expect(markup).toContain("VPC");
    expect(markup).not.toContain('data-testid="route-placeholder"');
    // Not redesigned yet, so still the white page.
    expect(markup).not.toContain("data-canvas");
  });

  it("renders the template registration screen at /templates/new", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/templates/new"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain("Register template");
    expect(markup).toContain("Root path");
    expect(markup).not.toContain('data-testid="route-not-found"');
  });

  it("renders a 404, not a permission leak, when canView is denied for a stack route", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    // retry: false + staleTime: Infinity: this test seeds the cache directly and
    // must never let TanStack Query's default refetch-on-mount fire a real,
    // unmocked fetch() for stale data — see the identical rationale in
    // useStackCapabilities.test.tsx (Task 2) and RequireCapability.test.tsx (Task 3).
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
      stack: {
        id: "stack_1",
        tenant_id: "tenant_123",
        name: "Stack",
        slug: "stack",
        tags: {},
        default_credential_ids: [],
        created_by: "user_123",
        created_at: "2026-07-19T00:00:00Z",
        effectiveCapabilities: { canView: false, canOperate: false, canApprove: false, canManageAccess: false }
      },
      templates: []
    });

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="route-not-found"');
  });

  it("renders AccessDenied for /stacks/:stackId/access when canManageAccess is denied but canView is allowed", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    // retry: false + staleTime: Infinity: this test seeds the cache directly and
    // must never let TanStack Query's default refetch-on-mount fire a real,
    // unmocked fetch() for stale data — see the identical rationale in
    // useStackCapabilities.test.tsx (Task 2) and RequireCapability.test.tsx (Task 3).
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
      stack: {
        id: "stack_1",
        tenant_id: "tenant_123",
        name: "Stack",
        slug: "stack",
        tags: {},
        default_credential_ids: [],
        created_by: "user_123",
        created_at: "2026-07-19T00:00:00Z",
        effectiveCapabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false }
      },
      templates: []
    });

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/access"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="route-access-denied"');
  });

  it("renders the CreateStackScreen when canCreateStack is allowed", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/new"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain("Create stack");
    expect(markup).not.toContain('data-testid="route-placeholder"');
  });

  // The sign-in screen is a sibling of "/", not a child, because everything
  // under "/" renders inside SessionProvider -- which resolves only with a
  // session, and this is the screen you are shown because you have none.
  // Nested, it could never mount. SessionProvider is mocked here, so the proof
  // is that the screen renders with no AuthContext around it at all.
  it("renders the sign-in screen at /signin outside the session boundary", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/signin"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={testRouter} />
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="signin-submit"');
    expect(markup).toContain("Sign in");
  });

  it("renders the 404 screen for unknown paths", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");


    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/nonexistent"] });
    const markup = renderToStaticMarkup(
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={testRouter} />
      </AuthContext.Provider>
    );

    expect(markup).toContain('data-testid="route-not-found"');
  });

  it("renders the add template screen at /stacks/:stackId/templates/new", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { createMemoryRouter, RouterProvider } = await import("react-router-dom");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
      stack: {
        id: "stack_1",
        tenant_id: "tenant_123",
        name: "Payments",
        slug: "payments",
        tags: {},
        default_credential_ids: [],
        created_by: "user_123",
        created_at: "2026-07-19T00:00:00Z",
        effectiveCapabilities: { canView: true, canOperate: true, canApprove: true, canManageAccess: true }
      },
      templates: []
    });
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), []);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/templates/new"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="add-stack-template-none"');
  });

  it("renders the upgrade screen at /stacks/:stackId/templates/:stackTemplateId/upgrade", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { queryKeys } = await import("../api/queryKeys");
    const queryClient = stackQueryClient(queryKeys, { templates: [installedTemplate] });
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), []);

    const testRouter = createMemoryRouter(routeConfig, {
      initialEntries: ["/stacks/stack_1/templates/st_1/upgrade"]
    });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    // The panel draws the template's header, then the screen in its outlet.
    expect(markup).toContain('data-testid="template-panel"');
    expect(markup).toMatch(/data-testid="upgrade-[a-z-]+"/);
  });

  it("renders AccessDenied for /stacks/:stackId/templates/new when canOperate is denied but canView is allowed", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { createMemoryRouter, RouterProvider } = await import("react-router-dom");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
      stack: {
        id: "stack_1",
        tenant_id: "tenant_123",
        name: "Payments",
        slug: "payments",
        tags: {},
        default_credential_ids: [],
        created_by: "user_123",
        created_at: "2026-07-19T00:00:00Z",
        effectiveCapabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false }
      },
      templates: []
    });

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/templates/new"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="route-access-denied"');
  });

  it("renders AccessDenied for /stacks/:stackId/templates/:stackTemplateId/upgrade when canOperate is denied but canView is allowed", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { queryKeys } = await import("../api/queryKeys");
    const queryClient = stackQueryClient(queryKeys, {
      capabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false },
      templates: [installedTemplate]
    });

    const testRouter = createMemoryRouter(routeConfig, {
      initialEntries: ["/stacks/stack_1/templates/st_1/upgrade"]
    });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="route-access-denied"');
  });
});

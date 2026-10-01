import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryRouter, Outlet, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { StackTemplate } from "../../api/types";
import type { StackCapabilities } from "../../auth/types";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";

vi.mock("../../auth/SessionProvider");

function authValue(): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
  };
}

// Renders the app's real routeConfig at a stack-scoped path with the stack
// query cache pre-seeded, mirroring router.test.tsx: retry: false +
// staleTime: Infinity so the seeded data never triggers a real fetch().
async function renderStackRoute(path: string, capabilities: StackCapabilities, templates: StackTemplate[] = []) {
  const { routeConfig } = await import("../../app/router");
  const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
  const { queryKeys } = await import("../../api/queryKeys");

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
      effectiveCapabilities: capabilities
    },
    templates
  });

  const testRouter = createMemoryRouter(routeConfig, { initialEntries: [path] });
  return renderToStaticMarkup(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={testRouter} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

// The breadcrumb's links and title, read from server-rendered markup without
// depending on classes or attribute order.
function breadcrumbOf(markup: string) {
  const nav = markup.match(/<nav [^>]*aria-label="Breadcrumb"[^>]*>[\s\S]*?<\/nav>/)?.[0] ?? "";
  return {
    links: [...nav.matchAll(/<a [^>]*href="([^"]*)"[^>]*>([^<]*)<\/a>/g)].map(([, href, text]) => `${text} ${href}`),
    title: nav.match(/<h1 [^>]*aria-current="page"[^>]*>([^<]*)<\/h1>/)?.[1],
    detail: nav.includes('data-slot="breadcrumb-detail"')
  };
}

const allAllowed: StackCapabilities = { canView: true, canOperate: true, canApprove: true, canManageAccess: true };

const vpc: StackTemplate = {
  id: "st_1",
  stack_id: "stack_1",
  component_key: "vpc",
  source_template_id: "tmpl_src_1",
  desired_template_revision_id: "rev_1",
  last_applied_template_revision_id: "",
  source_ref: "main",
  workspace_name: "ws-payments",
  display_name: "Network",
  config: {},
  last_applied_run_id: "",
  pending_plan_run_id: "",
  plan_state: "none",
  live_state: "never",
  created_by: "user_123",
  lifecycle: "active"
};

describe("StackDetailShell", () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it("renders the stack name and all four tabs when every capability is granted", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1", allAllowed);

    expect(markup).toContain('data-testid="stack-detail-shell"');
    expect(markup).toContain("Payments");
    expect(markup).toContain('href="/stacks/stack_1"');
    expect(markup).toContain('href="/stacks/stack_1/templates"');
    expect(markup).toContain('href="/stacks/stack_1/access"');
    expect(markup).toContain('href="/stacks/stack_1/environment"');
  });

  // Runs live on each template's own page, so the stack has no Runs tab and
  // no runs routes of its own.
  it("offers no Runs tab", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1", allAllowed);

    expect(markup).not.toContain('href="/stacks/stack_1/runs"');
    expect(markup).not.toContain(">Runs<");
  });

  it("no longer resolves the standalone runs routes", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    for (const path of ["/stacks/stack_1/runs", "/stacks/stack_1/runs/run_1", "/stacks/stack_1/template"]) {
      const markup = await renderStackRoute(path, allAllowed);
      expect(markup).toContain('data-testid="route-not-found"');
      expect(markup).not.toContain('data-testid="stack-detail-shell"');
    }
  });

  it("omits the Access tab when canManageAccess is denied", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1", { ...allAllowed, canManageAccess: false });

    expect(markup).toContain('data-testid="stack-detail-shell"');
    expect(markup).toContain('href="/stacks/stack_1/templates"');
    expect(markup).not.toContain('href="/stacks/stack_1/access"');
    expect(markup).not.toContain('href="/stacks/stack_1/environment"');
  });

  it("renders each nested route's content inside the shell", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    // The access tab is a real screen now with its own loading state; the
    // grants query is unseeded here, so the shell renders the StackAccessScreen
    // component as the nested content.
    const accessMarkup = await renderStackRoute("/stacks/stack_1/access", allAllowed);
    expect(accessMarkup).toContain('data-testid="stack-detail-shell"');
    expect(accessMarkup).toContain("Current Grants");

    const environmentMarkup = await renderStackRoute("/stacks/stack_1/environment", allAllowed);
    expect(environmentMarkup).toContain('data-testid="stack-detail-shell"');
    expect(environmentMarkup).toContain('data-testid="environment-loading"');

    // The seeded stack view has no installed templates, so the list renders
    // its empty state as the nested content.
    const templateMarkup = await renderStackRoute("/stacks/stack_1/templates", allAllowed);
    expect(templateMarkup).toContain('data-testid="stack-detail-shell"');
    expect(templateMarkup).toContain('data-testid="stack-template-empty"');

    // A template's page nests inside the stack shell, with its own tabs.
    const detailMarkup = await renderStackRoute("/stacks/stack_1/templates/st_1/settings", allAllowed, [vpc]);
    expect(detailMarkup).toContain('data-testid="stack-detail-shell"');
    expect(detailMarkup).toContain('data-testid="stack-template-detail"');
    expect(detailMarkup).toContain('data-testid="template-settings-tab"');

    // Run detail's runs query is unseeded here, so the shell renders its
    // loading state as the nested content.
    const runDetailMarkup = await renderStackRoute("/stacks/stack_1/templates/st_1/runs/1", allAllowed, [vpc]);
    expect(runDetailMarkup).toContain('data-testid="stack-template-detail"');
    expect(runDetailMarkup).toContain('data-testid="run-detail-loading"');
  });

  it("titles the page with a Stacks / stack breadcrumb", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1/templates", allAllowed);

    expect(breadcrumbOf(markup)).toEqual({ links: ["Stacks /stacks"], title: "Payments", detail: false });
  });

  // A template's page has one row of tabs, the template's; the breadcrumb is
  // the way back to the stack's. Its state sits at the end of that row, not in
  // the breadcrumb.
  it("swaps the stack tabs for the template's on a template's page", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1/templates/st_1/settings", allAllowed, [{ ...vpc, live_state: "differs" }]);

    expect(markup).toContain('aria-label="Template sections"');
    expect(markup).not.toContain('aria-label="Stack sections"');
    expect(breadcrumbOf(markup).detail).toBe(false);
    // The template's state follows its tab list, at the end of the same row.
    expect(markup).toMatch(/role="tablist"[^>]*aria-label="Template sections".*data-testid="stack-template-state"[^>]*>.*changed/);
    expect(markup).toContain("Plan, then apply");
  });

  it("keeps the template's state off a run's page, where it would read as the run's", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1/templates/st_1/runs/1", allAllowed, [vpc]);

    expect(markup).not.toContain('data-testid="stack-template-state"');
  });

  it("names the template on its own page", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1/templates/st_1/settings", allAllowed, [vpc]);

    expect(breadcrumbOf(markup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates"],
      title: "Network"
    });
  });

  it("extends the breadcrumb through the template on a page below it", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const runMarkup = await renderStackRoute("/stacks/stack_1/templates/st_1/runs/4", allAllowed, [vpc]);
    expect(breadcrumbOf(runMarkup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates", "Network /stacks/stack_1/templates/st_1"],
      title: "Run #4"
    });

    const upgradeMarkup = await renderStackRoute("/stacks/stack_1/templates/st_1/upgrade", allAllowed, [vpc]);
    expect(breadcrumbOf(upgradeMarkup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates", "Network /stacks/stack_1/templates/st_1"],
      title: "Change revision"
    });

    const addMarkup = await renderStackRoute("/stacks/stack_1/templates/new", allAllowed);
    expect(breadcrumbOf(addMarkup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates"],
      title: "Add template"
    });
  });

  it("marks the tab matching the current route as current", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1/templates", allAllowed);

    expect(markup).toMatch(/aria-current="page"[^>]*>Templates|href="\/stacks\/stack_1\/templates"[^>]*aria-current="page"/);
  });

  // The tab's selected state comes from stackSection(), and its link's
  // aria-current from NavLink. They must name the same tab on every route,
  // or a screen reader hears one tab selected and another current.
  it.each([
    ["/stacks/stack_1", "Overview"],
    ["/stacks/stack_1/templates", "Templates"],
    ["/stacks/stack_1/templates/new", "Templates"],
    ["/stacks/stack_1/environment", "Environment"],
    ["/stacks/stack_1/access", "Access"]
  ])("selects exactly one tab at %s: %s, the one its link marks current", async (path, tab) => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute(path, allAllowed);

    const tabs = [...markup.matchAll(/<a [^>]*role="tab"[^>]*>([^<]*)<\/a>/g)].map(([tag, label]) => ({
      label,
      selected: tag.includes('aria-selected="true"'),
      current: tag.includes('aria-current="page"')
    }));
    expect(tabs.map(({ label }) => label)).toEqual(["Overview", "Templates", "Environment", "Access"]);
    expect(tabs.filter(({ selected }) => selected).map(({ label }) => label)).toEqual([tab]);
    expect(tabs.filter(({ current }) => current).map(({ label }) => label)).toEqual([tab]);
  });

  it("names the stack's tab list", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1", allAllowed);

    expect(markup).toMatch(/<div [^>]*role="tablist"[^>]*aria-label="Stack sections"/);
  });

  it("still renders NotFound (no shell chrome) when canView is denied", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1", {
      canView: false,
      canOperate: false,
      canApprove: false,
      canManageAccess: false
    });

    expect(markup).toContain('data-testid="route-not-found"');
    expect(markup).not.toContain('data-testid="stack-detail-shell"');
  });
});

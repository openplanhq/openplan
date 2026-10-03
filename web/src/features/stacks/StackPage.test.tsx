// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, redirect, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { StackCapabilities } from "../../auth/types";
import { queryKeys } from "../../api/queryKeys";
import type { AttentionItem, StackTemplate, StackView } from "../../api/types";
import StackIndexPanel from "./StackIndexPanel";
import StackPage from "./StackPage";
import TemplatePanel from "./TemplatePanel";

// The stack's page through the same route shape as router.tsx, with each
// template tab stubbed, so these tests are about the frame: the header, the
// list, the selection and the phone layout. The tabs have their own tests.

const TENANT = "tenant_123";
const allAllowed: StackCapabilities = { canView: true, canOperate: true, canApprove: true, canManageAccess: true };

function template(id: string, overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id,
    stack_id: "stack_1",
    component_key: id,
    source_template_id: "source_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "rev_1",
    source_ref: "v1.0.0",
    workspace_name: id,
    display_name: id,
    config: {},
    last_applied_run_id: "",
    last_applied_at: "2026-09-22T10:00:00Z",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "matches",
    created_by: "user_1",
    lifecycle: "active",
    ...overrides
  };
}

const network = template("network");
const eks = template("eks-cluster", { source_ref: "v1.4.0", pending_plan_run_id: "run_14", pending_plan_at: "2026-10-03T09:30:00Z" });
const cdn = template("cloudfront", { lifecycle: "failed" });

function view(templates: StackTemplate[], capabilities: StackCapabilities = allAllowed): StackView {
  return {
    stack: {
      id: "stack_1",
      tenant_id: TENANT,
      name: "Payments",
      slug: "payments",
      tags: { env: "production" },
      default_credential_ids: [],
      created_by: "user_1",
      created_at: "2026-07-19T00:00:00Z",
      effectiveCapabilities: capabilities
    },
    templates
  };
}

const failedItem: AttentionItem = {
  kind: "destroy_failed",
  at: "2026-09-29T08:00:00Z",
  stack: { id: "stack_1", name: "Payments", slug: "payments" },
  stack_template: { id: "cloudfront", workspace_name: "cloudfront", display_name: "cloudfront" },
  run: null
};

function seed(templates: StackTemplate[], capabilities: StackCapabilities = allAllowed): QueryClient {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(queryKeys.stack(TENANT, "stack_1"), view(templates, capabilities));
  queryClient.setQueryData(queryKeys.attention(TENANT), [failedItem]);
  for (const stackTemplate of templates) {
    queryClient.setQueryData(queryKeys.templateRuns(TENANT, stackTemplate.id), []);
  }
  return queryClient;
}

function authValue(): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: TENANT, displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

function renderPage(queryClient: QueryClient, path: string) {
  const router = createMemoryRouter(
    [
      {
        path: "/stacks/:stackId",
        element: <StackPage />,
        children: [
          { index: true, element: <StackIndexPanel /> },
          { path: "templates", loader: ({ params }) => redirect(`/stacks/${params.stackId}`) },
          { path: "templates/new", element: <p data-testid="add-template-content">add</p> },
          {
            path: "templates/:stackTemplateId",
            element: <TemplatePanel />,
            children: [
              { path: "runs", element: <p data-testid="runs-content">runs</p> },
              { path: "runs/:runNumber", element: <p data-testid="run-content">run</p> },
              { path: "variables", element: <p data-testid="variables-content">variables</p> },
              { path: "settings", element: <p data-testid="settings-content">settings</p> },
              { path: "upgrade", element: <p data-testid="upgrade-content">upgrade</p> }
            ]
          }
        ]
      }
    ],
    { initialEntries: [path] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

beforeEach(() => {
  // Everything is seeded; anything else stays pending rather than reaching out.
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("StackPage header", () => {
  it("titles the page with the stack, a trail back to Stacks, its slug and its tags", () => {
    renderPage(seed([network]), "/stacks/stack_1");

    expect(screen.getByRole("heading", { level: 1, name: "Payments" })).toBeTruthy();
    expect(within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByRole("link", { name: "Stacks" }).getAttribute("href")).toBe("/stacks");
    expect(screen.getByText("payments")).toBeTruthy();
    expect(screen.getByText("env: production")).toBeTruthy();
  });

  it("offers Environment and Access only to people who may manage access", () => {
    renderPage(seed([network]), "/stacks/stack_1");
    expect(screen.getByRole("link", { name: "Environment" }).getAttribute("href")).toBe("/stacks/stack_1/environment");
    expect(screen.getByRole("link", { name: "Access" }).getAttribute("href")).toBe("/stacks/stack_1/access");
    cleanup();

    renderPage(seed([network], { ...allAllowed, canManageAccess: false }), "/stacks/stack_1");
    expect(screen.queryByRole("link", { name: "Environment" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Access" })).toBeNull();
  });
});

describe("StackPage selection", () => {
  it("opens on the first template with a plan waiting, without changing the URL", () => {
    const router = renderPage(seed([network, cdn, eks]), "/stacks/stack_1");

    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-link-eks-cluster").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("stack-template-link-network").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("template-runs-tab")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/stacks/stack_1");
  });

  it("falls back to a failed destroy, then to the first template", () => {
    renderPage(seed([network, cdn]), "/stacks/stack_1");
    expect(screen.getByRole("heading", { level: 2, name: "cloudfront" })).toBeTruthy();
    cleanup();

    renderPage(seed([network, template("rds")]), "/stacks/stack_1");
    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
  });

  it("selects the template in the URL", () => {
    renderPage(seed([network, eks]), "/stacks/stack_1/templates/network/variables");

    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-link-network").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("variables-content")).toBeTruthy();
  });

  // Picking another template keeps the tab; from a run it lands on Runs, and
  // from Change revision on Settings.
  it.each([
    ["/stacks/stack_1/templates/network/variables", "/stacks/stack_1/templates/eks-cluster/variables"],
    ["/stacks/stack_1/templates/network/runs/3", "/stacks/stack_1/templates/eks-cluster/runs"],
    ["/stacks/stack_1/templates/network/upgrade", "/stacks/stack_1/templates/eks-cluster/settings"],
    ["/stacks/stack_1/templates/new", "/stacks/stack_1/templates/eks-cluster/runs"]
  ])("from %s, another template's row opens %s", (from, to) => {
    renderPage(seed([network, eks]), from);
    expect(screen.getByTestId("stack-template-link-eks-cluster").getAttribute("href")).toBe(to);
  });

  it("says so when the URL names a template the stack does not have, and keeps the list", () => {
    renderPage(seed([network, eks]), "/stacks/stack_1/templates/gone/runs");

    expect(screen.getByTestId("stack-template-missing").textContent).toBe("That template is not installed on this stack.");
    expect(screen.getByTestId("stack-template-link-network")).toBeTruthy();
  });

  // The index picks its template once. A plan approved underneath it must not
  // swap the panel to another template while someone is reading it.
  it("keeps the stack's own path on the template it opened on while the stack changes", async () => {
    const queryClient = seed([network, eks]);
    renderPage(queryClient, "/stacks/stack_1");
    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();

    act(() => {
      queryClient.setQueryData(queryKeys.stack(TENANT, "stack_1"), view([network, { ...eks, pending_plan_run_id: "", pending_plan_at: undefined }]));
    });
    // The list has caught up: the plan is gone from eks-cluster's row.
    await waitFor(() => expect(within(screen.getByTestId("stack-template-link-eks-cluster")).queryByText("plan to approve")).toBeNull());
    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-link-eks-cluster").getAttribute("aria-current")).toBe("true");

    // Gone from the stack, it gives way to the default.
    act(() => {
      queryClient.setQueryData(queryKeys.stack(TENANT, "stack_1"), view([network]));
    });
    await waitFor(() => expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy());
  });

  it("sends the old template list to the stack's page", async () => {
    const router = renderPage(seed([network]), "/stacks/stack_1/templates");
    await waitFor(() => expect(router.state.location.pathname).toBe("/stacks/stack_1"));
  });
});

describe("StackPage list", () => {
  it("shows each template's ref and what last happened, and chips only for what needs a person", () => {
    renderPage(seed([network, eks, cdn]), "/stacks/stack_1");

    const eksRow = screen.getByTestId("stack-template-link-eks-cluster");
    expect(eksRow.textContent).toContain("v1.4.0");
    expect(eksRow.textContent).toMatch(/Planned 3 Oct, \d\d:\d\d/);
    expect(within(eksRow).getByText("plan to approve")).toBeTruthy();

    const cdnRow = screen.getByTestId("stack-template-link-cloudfront");
    expect(within(cdnRow).getByText("destroy failed")).toBeTruthy();
    expect(cdnRow.textContent).toMatch(/Failed 29 Sept?, \d\d:\d\d/);

    const networkRow = screen.getByTestId("stack-template-link-network");
    expect(networkRow.textContent).toMatch(/Applied 22 Sept? 2026/);
    expect(within(networkRow).queryByText(/plan to approve|destroy failed/)).toBeNull();
  });

  it("narrows the list as the person types, keeps the selection, and says when nothing matches", () => {
    renderPage(seed([network, eks]), "/stacks/stack_1");
    const filter = screen.getByRole("searchbox", { name: "Filter templates" });

    fireEvent.change(filter, { target: { value: "net" } });
    expect(screen.getByTestId("stack-template-link-network")).toBeTruthy();
    expect(screen.queryByTestId("stack-template-link-eks-cluster")).toBeNull();
    // The filter narrows the list, not the selection.
    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();

    fireEvent.change(filter, { target: { value: "zzz" } });
    expect(screen.getByTestId("stack-templates-filter-empty").textContent).toBe("No templates match this filter.");
  });

  it("ends the list with Add template for operators, selected while adding", () => {
    renderPage(seed([network]), "/stacks/stack_1");
    const add = screen.getByTestId("add-stack-template-link");
    expect(add.getAttribute("href")).toBe("/stacks/stack_1/templates/new");
    expect(add.textContent).toBe("Add template");
    cleanup();

    renderPage(seed([network]), "/stacks/stack_1/templates/new");
    expect(screen.getByTestId("add-stack-template-link").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("add-stack-template-link").textContent).toBe("New template");
    expect(screen.getByTestId("stack-template-link-network").getAttribute("aria-current")).toBeNull();
    cleanup();

    renderPage(seed([network], { ...allAllowed, canOperate: false }), "/stacks/stack_1");
    expect(screen.queryByTestId("add-stack-template-link")).toBeNull();
  });

  it("says a stack with no templates has none, and offers to add one", () => {
    renderPage(seed([]), "/stacks/stack_1");

    expect(screen.getByTestId("stack-templates-none").textContent).toBe("No templates in this stack yet.");
    expect(screen.getByTestId("stack-empty")).toBeTruthy();
    expect(screen.getAllByRole("link", { name: /Add template/ })).toHaveLength(2);
  });
});

// On a phone the page shows the list or the panel, never both. This is CSS,
// so these two tests read the classes that switch it.
describe("StackPage on a phone", () => {
  it("shows only the list on the stack's own path", () => {
    renderPage(seed([network]), "/stacks/stack_1");

    expect(screen.getByTestId("stack-list-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("stack-panel-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.queryByRole("link", { name: "Templates" })).toBeNull();
  });

  it("shows only the panel on a template's path, with a way back to the list", () => {
    renderPage(seed([network]), "/stacks/stack_1/templates/network/runs");

    expect(screen.getByTestId("stack-list-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("stack-panel-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByRole("link", { name: "Templates" }).getAttribute("href")).toBe("/stacks/stack_1");
  });
});

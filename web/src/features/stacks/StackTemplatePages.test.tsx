// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { Navigate, MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import RequireCapability from "../../auth/RequireCapability";
import { queryKeys } from "../../api/queryKeys";
import type { StackTemplate, StackView, TemplateRevision, TemplateRun, TemplateVariable } from "../../api/types";
import type { StackCapabilities } from "../../auth/types";
import RunDetailScreen from "../runs/RunDetailScreen";
import TemplatePanel from "./TemplatePanel";
import TemplateCredentialsTab from "./TemplateCredentialsTab";
import TemplateRunsTab from "./TemplateRunsTab";
import TemplateSettingsTab from "./TemplateSettingsTab";
import TemplateVariablesTab from "./TemplateVariablesTab";

// The panel for one template and its tabs, rendered through the same route
// shape as router.tsx so tab links and the index redirect are exercised the
// way the app uses them. The stack's page around it has its own tests.

const allAllowed: StackCapabilities = { canView: true, canOperate: true, canApprove: true, canManageAccess: true };

function stackView(capabilities: StackCapabilities, templates: StackTemplate[]): StackView {
  return {
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
  };
}

function stackTemplate(overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id: "st_1",
    stack_id: "stack_1",
    component_key: "vpc",
    source_template_id: "tmpl_src_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "rev_0",
    source_ref: "main",
    workspace_name: "ws-payments",
    display_name: "",
    config: { region: "us-east-1" },
    last_applied_run_id: "run_9",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "never",
    created_by: "user_123",
    lifecycle: "active",
    ...overrides
  };
}

function templateRevision(overrides: Partial<TemplateRevision> = {}): TemplateRevision {
  return {
    id: "rev_1",
    tenant_id: "tenant_123",
    source_template_id: "tmpl_src_1",
    repo_owner: "hashicorp",
    repo_name: "vpc",
    source_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    root_path: ".",
    name: "vpc",
    description: "",
    tags: [],
    status: "active",
    created_at: "2026-07-19T00:00:00Z",
    ...overrides
  };
}

function variable(overrides: Partial<TemplateVariable> = {}): TemplateVariable {
  return {
    template_revision_id: "rev_1",
    name: "region",
    type_expression: "string",
    description: "",
    required: true,
    has_default: false,
    sensitive: false,
    has_validation: false,
    ...overrides
  };
}

// staleTime: Infinity keeps seeded cache data from triggering a background
// refetch on mount — see the identical rationale in StacksListScreen.test.tsx.
function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function seedDefaultData(queryClient: QueryClient, capabilities: StackCapabilities = allAllowed) {
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(capabilities, [stackTemplate()]));
  queryClient.setQueryData(queryKeys.attention("tenant_123"), []);
  queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
    templateRevision(),
    templateRevision({ id: "rev_2", source_ref: "v2", resolved_commit_sha: "1234567abcdef" })
  ]);
  queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);
  queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_2"), [variable()]);
  // The inline run sections query runs for whichever template is selected;
  // seeding both keeps these tests off the network.
  queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), []);
  queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_2"), []);
}

/** True when `first` appears before `second` in document order. */
function precedes(first: HTMLElement, second: HTMLElement): boolean {
  return Boolean(first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING);
}

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides,
  };
}

function runFor(stackTemplateID: string, overrides: Partial<TemplateRun> = {}): TemplateRun {
  return {
    id: `run_for_${stackTemplateID}`,
    tenant_id: "tenant_123",
    stack_template_id: stackTemplateID,
    template_revision_id: "rev_1",
    source_template_id: "tmpl_src_1",
    operation: "plan",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    workspace_name: "ws-payments",
    config_json: {},
    backend_type: "s3",
    backend_config_hash: "hash",
    status: "completed",
    step: "",
    trigger_actor: "user_123",
    trigger_actor_display_name: "user_123",
    created_at: "2026-07-20T00:00:00Z",
    error_summary: "",
    run_number: 1,
    auto_approve: false,
    plan_summary: null,
    ...overrides
  };
}

function renderAt(queryClient: QueryClient, initialEntry: string, auth?: AuthContextValue) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth ?? authValue()}>
        <MemoryRouter initialEntries={[initialEntry]}>
          <Routes>
            <Route path="/stacks/:stackId/templates/:stackTemplateId" element={<TemplatePanel />}>
              <Route index element={<Navigate to="runs" replace />} />
              <Route path="runs" element={<TemplateRunsTab />} />
              <Route path="runs/:runNumber" element={<RunDetailScreen />} />
              <Route path="variables" element={<TemplateVariablesTab />} />
              <Route path="credentials" element={<RequireCapability capability="canManageAccess" mode="route" />}>
                <Route index element={<TemplateCredentialsTab />} />
              </Route>
              <Route path="settings" element={<TemplateSettingsTab />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function actionButton(name: RegExp): HTMLButtonElement {
  return screen.getByRole("button", { name }) as HTMLButtonElement;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

describe("TemplatePanel", () => {
  it("opens on the Runs tab", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1");

    await waitFor(() => expect(screen.getByTestId("template-runs-tab")).toBeTruthy());
    expect(screen.getByRole("link", { name: "Runs" }).getAttribute("aria-current")).toBe("page");
  });

  it("says so when the template in the URL is not installed on the stack", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_missing/runs");

    expect(screen.getByTestId("stack-template-missing")).toBeTruthy();
  });

  it("hides the Credentials tab without canManageAccess", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient, { ...allAllowed, canManageAccess: false });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/runs");

    await waitFor(() => expect(screen.getByTestId("template-runs-tab")).toBeTruthy());
    expect(screen.queryByRole("link", { name: "Credentials" })).toBeNull();
    expect(screen.getByRole("link", { name: "Settings" })).toBeTruthy();
  });

  it("keeps the Runs tab lit while reading one run", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    const run = runFor("st_1", { run_number: 3 });
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), [run]);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", run.id), run);
    queryClient.setQueryData(queryKeys.templateRunLogs("tenant_123", run.id, "completed"), []);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/runs/3");

    await waitFor(() => expect(screen.getByTestId("run-detail-status")).toBeTruthy());
    // The run's trail links to Runs too; the tab is the one in the tab row.
    const tabs = screen.getByRole("navigation", { name: "Template sections" });
    expect(within(tabs).getByRole("link", { name: "Runs" }).getAttribute("aria-current")).toBe("page");
  });

  it("names the template with its state, its ref and what last happened", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(
      queryKeys.stack("tenant_123", "stack_1"),
      stackView(allAllowed, [
        stackTemplate({ display_name: "eks-cluster", source_ref: "v1.4.0", pending_plan_run_id: "run_14", pending_plan_at: "2026-10-03T09:30:00Z" })
      ])
    );

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/runs");

    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-status-st_1").textContent).toBe("waiting for approval");
    expect(screen.getByText("v1.4.0")).toBeTruthy();
    expect(screen.getByText(/^Planned 3 Oct, \d\d:\d\d$/)).toBeTruthy();
  });

  it("marks Settings as the current tab while changing the revision", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    render(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <MemoryRouter initialEntries={["/stacks/stack_1/templates/st_1/upgrade"]}>
            <Routes>
              <Route path="/stacks/:stackId/templates/:stackTemplateId" element={<TemplatePanel />}>
                <Route path="upgrade" element={<p>change revision</p>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(screen.getByRole("link", { name: "Settings" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Runs" }).getAttribute("aria-current")).toBeNull();
  });

  // The stack's states change when a run settles, and only the runs query
  // polls. The panel refreshes the stack itself, so the header and the list
  // stay current on every tab, not just Runs.
  // A destroy that finishes takes its template out of the stack. Whoever
  // approved it on the run is still reading that run, so the panel keeps it,
  // says the template was destroyed, and does not call the link stale.
  it("keeps a run readable when its template is destroyed while it is open", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    const run = runFor("st_1", { operation: "destroy", status: "running", run_number: 4, plan_summary: { add: 0, change: 0, destroy: 2 } });
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), [run]);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", run.id), run);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/runs/4");
    await waitFor(() => expect(screen.getByTestId("run-detail-screen")).toBeTruthy());

    act(() => {
      queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(allAllowed, []));
    });

    await waitFor(() => expect(screen.getByText("destroyed")).toBeTruthy());
    expect(screen.queryByTestId("stack-template-missing")).toBeNull();
    expect(screen.getByTestId("run-detail-screen")).toBeTruthy();
  });

  it("refreshes the stack when the template's latest run settles, on any tab", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), [runFor("st_1", { status: "completed" })]);
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(
        async () =>
          new Response(JSON.stringify(stackView(allAllowed, [stackTemplate()])), { status: 200, headers: { "content-type": "application/json" } })
      );

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/stacks/stack_1"))).toBe(true));
  });
});

describe("TemplateRunsTab", () => {
  it("renders the run actions above the history for the template in the URL", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(
      queryKeys.stack("tenant_123", "stack_1"),
      stackView(allAllowed, [stackTemplate(), stackTemplate({ id: "st_2", desired_template_revision_id: "rev_2" })])
    );
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_2"), [runFor("st_2")]);

    renderAt(queryClient, "/stacks/stack_1/templates/st_2/runs");

    expect(precedes(screen.getByTestId("template-run-actions"), screen.getByTestId("template-run-history"))).toBe(true);
    expect(screen.getByTestId("template-run-history-run_for_st_2").getAttribute("href")).toBe("/stacks/stack_1/templates/st_2/runs/1");
    // Destroy is a tab away, not under the Plan button.
    expect(screen.queryByTestId("template-destroy-panel")).toBeNull();
  });
});

describe("TemplateVariablesTab", () => {
  it("shows a loading state while the template's variables are pending", () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(allAllowed, [stackTemplate()]));
    queryClient.setQueryData(queryKeys.attention("tenant_123"), []);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    expect(screen.getByTestId("template-variables-loading")).toBeTruthy();
  });

  it("renders variables prefilled from the installed template's config", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    expect((screen.getByLabelText(/region/) as HTMLInputElement).value).toBe("us-east-1");
  });

  it("renders the config of the template in the URL rather than the first installed one", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(
      queryKeys.stack("tenant_123", "stack_1"),
      stackView(allAllowed, [
        stackTemplate(),
        stackTemplate({ id: "st_2", desired_template_revision_id: "rev_2", config: { region: "ap-south-1" } })
      ])
    );

    renderAt(queryClient, "/stacks/stack_1/templates/st_2/variables");

    expect((screen.getByLabelText(/region/) as HTMLInputElement).value).toBe("ap-south-1");
  });

  it("shows no raw identifiers anywhere in the configuration", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    const config = screen.getByTestId("stack-template-config");
    expect(config.textContent).not.toContain("tmpl_src_1");
    expect(config.textContent).not.toContain("rev_1");
    expect(config.textContent).not.toContain("rev_0");
    expect(config.textContent).not.toContain("run_9");
    expect(screen.queryByTestId("stack-template-revision-select")).toBeNull();
    expect(screen.queryByRole("button", { name: /Install template/ })).toBeNull();
  });

  it("disables save while values match the installed config and enables it after an edit", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    expect(actionButton(/Save config/).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });
    expect(actionButton(/Save config/).disabled).toBe(false);
  });

  it("locks configuration when canOperate is denied", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient, { ...allAllowed, canOperate: false });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    await waitFor(() => expect(screen.getByTestId("variables-disabled-reason")).toBeTruthy());
    expect(actionButton(/Save config/).disabled).toBe(true);
    expect((screen.getByLabelText(/region/) as HTMLInputElement).disabled).toBe(true);
  });

  it("does not need the revisions query to render", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.removeQueries({ queryKey: queryKeys.templateRevisions("tenant_123") });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    await waitFor(() => expect(screen.getByTestId("stack-template-config")).toBeTruthy());
  });

  function mockVariablesFailure(status: number, error: string) {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error, message: error }), { status, headers: { "content-type": "application/json" } })
    );
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(allAllowed, [stackTemplate()]));
    queryClient.setQueryData(queryKeys.attention("tenant_123"), []);
    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");
  }

  it("renders the shared boundary screen for a handled API error status", async () => {
    mockVariablesFailure(503, "unavailable");
    await waitFor(() => expect(screen.getByTestId("route-service-unavailable")).toBeTruthy());
  });

  it("renders the generic error state when the API returns 401", async () => {
    // A 401 drives client.ts's fetchWithAuth to navigate via
    // globalThis.location.assign; stub it so jsdom does not attempt (and
    // warn about) a real navigation.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    mockVariablesFailure(401, "unauthorized");
    await waitFor(() => expect(screen.getByTestId("template-variables-error")).toBeTruthy());
  });

  it("renders AccessDenied when the API returns 403", async () => {
    mockVariablesFailure(403, "forbidden");
    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });

  it("renders NotFound when the API returns 404", async () => {
    mockVariablesFailure(404, "not_found");
    await waitFor(() => expect(screen.getByTestId("route-not-found")).toBeTruthy());
  });
});

describe("editing while a run is in flight", () => {
  // A plan waiting for approval is a promise about the config and revision it
  // was made from, so neither can change until it is applied or discarded.
  it("locks the config and the revision behind the waiting plan", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), [runFor("st_1", { status: "waiting_approval", run_number: 7 })]);

    const variables = renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");
    await waitFor(() => expect(screen.getByTestId("variables-disabled-reason").textContent).toBe("Apply or discard run #7 before changing the config."));
    expect(actionButton(/Save config/).disabled).toBe(true);
    variables.unmount();

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");
    expect(screen.getByTestId("upgrade-disabled-reason").textContent).toBe("Apply or discard run #7 before changing the revision.");
    expect((screen.getByTestId("change-stack-template-revision-link") as HTMLButtonElement).disabled).toBe(true);
  });
});

describe("TemplateCredentialsTab", () => {
  it("shows only this template's credentials and titles the panel by scope", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { id: "stack_credential", name: "STACK_ONLY", scope: "stack", created_at: "2026-07-19T00:00:00Z" }
    ]);
    queryClient.setQueryData(queryKeys.stackTemplateCredentials("tenant_123", "st_1"), [
      { id: "template_credential", name: "TEMPLATE_ONLY", scope: "stack_template", created_at: "2026-07-19T00:00:00Z" }
    ]);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/credentials");

    expect(screen.getByText("TEMPLATE_ONLY")).toBeTruthy();
    expect(screen.queryByText("STACK_ONLY")).toBeNull();
    expect(screen.getByText("Template credentials")).toBeTruthy();
    expect(screen.getByText("Overrides the stack environment for this template only.")).toBeTruthy();
  });

  it("denies the tab itself without canManageAccess", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient, { ...allAllowed, canManageAccess: false });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/credentials");

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });
});

describe("TemplateSettingsTab", () => {
  it("offers changing the revision, as a secondary action above destroy", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    const revisionAction = screen.getByTestId("stack-template-revision-action");
    expect(revisionAction.className).not.toContain("panel");
    expect(screen.getByTestId("change-stack-template-revision-link").getAttribute("href")).toBe("/stacks/stack_1/templates/st_1/upgrade");
    expect(precedes(revisionAction, screen.getByTestId("template-destroy-panel"))).toBe(true);
  });

  it("does not wait for the tenant revision list before offering a revision change", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.removeQueries({ queryKey: queryKeys.templateRevisions("tenant_123") });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    expect(screen.getByTestId("change-stack-template-revision-link")).toBeTruthy();
  });

  it("hides the revision change when canOperate is denied", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient, { ...allAllowed, canOperate: false });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    await waitFor(() => expect(screen.getByTestId("template-settings-tab")).toBeTruthy());
    expect(screen.queryByTestId("change-stack-template-revision-link")).toBeNull();
  });

  it("disables revision selection while the template is destroying", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(allAllowed, [stackTemplate({ lifecycle: "destroying" })]));

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    const changeRevisionControl = screen.getByTestId("change-stack-template-revision-link") as HTMLButtonElement;
    expect(changeRevisionControl.tagName).toBe("BUTTON");
    expect(changeRevisionControl.disabled).toBe(true);
    expect(screen.getByTestId("upgrade-disabled-reason").textContent).toBe("Destroy in progress.");
  });
});

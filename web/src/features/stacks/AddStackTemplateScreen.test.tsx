// @vitest-environment jsdom
import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import AddStackTemplateScreen from "./AddStackTemplateScreen";
import TemplatePanel from "./TemplatePanel";
import { queryKeys } from "../../api/queryKeys";
import type { StackTemplate, TemplateRevision, TemplateVariable } from "../../api/types";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";

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

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

// AddStackTemplateScreen renders via useQueryErrorBoundary, which calls
// useAuth() unconditionally (see queryErrorBoundary.tsx and its use in every
// sibling screen test — StackTemplateScreen.test.tsx, CreateStackScreen.test.tsx,
// TemplateRegistryScreen.test.tsx, etc.). Without an AuthContext.Provider the
// hook throws "useAuth must be used within an AuthProvider" before the screen
// can render at all, so this wrapper is required infrastructure, not a test
// case change.
function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides
  };
}

function LocationProbe() {
  const location = useLocation();
  return <span data-testid="location">{`${location.pathname}${location.search}`}</span>;
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function installedTemplate(id: string): StackTemplate {
  return {
    id,
    stack_id: "stack_1",
    component_key: "vpc",
    source_template_id: "tmpl_src_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "",
    source_ref: "main",
    workspace_name: "ws-vpc",
    display_name: "",
    config: { region: "eu-west-1" },
    last_applied_run_id: "",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "never",
    created_by: "user_123",
    lifecycle: "active"
  };
}

function stackView(templates: StackTemplate[]) {
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
      effectiveCapabilities: { canView: true, canOperate: true, canApprove: true, canManageAccess: true }
    },
    templates
  };
}

// The header names the stack being added to, so the stack is always seeded.
function seedStack(queryClient: QueryClient) {
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView([]));
}

function renderScreen(queryClient: QueryClient, routes: ReactNode = <Route path="/stacks/:stackId/templates/:stackTemplateId/*" element={<LocationProbe />} />) {
  seedStack(queryClient);
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <MemoryRouter initialEntries={["/stacks/stack_1/templates/new"]}>
          <Routes>
            <Route path="/stacks/:stackId/templates/new" element={<AddStackTemplateScreen />} />
            <Route path="/stacks/:stackId" element={<LocationProbe />} />
            {routes}
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

// Only while no popup is open: Base UI hides the rest of the page from
// assistive technology while one is.
const searchBox = () => screen.getByRole("combobox", { name: "Search templates" }) as HTMLInputElement;

async function chooseTemplate(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(searchBox());
  await user.type(searchBox(), name);
  await user.click(await screen.findByRole("option", { name: new RegExp(`^${name}`) }));
}

describe("AddStackTemplateScreen", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("searches the registry instead of listing it", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(
      queryKeys.templateRevisions("tenant_123"),
      ["acm", "alb", "eks", "iam", "kms", "rds", "sqs", "waf"].map((name, index) =>
        templateRevision({ id: `rev_${index}`, source_template_id: `tmpl_${index}`, name, root_path: `aws/${name}` })
      )
    );

    renderScreen(queryClient);

    // Nothing is listed until the search is used.
    expect(screen.queryByRole("option")).toBeNull();
    expect(screen.getByText("8 templates in 1 repository.")).toBeTruthy();

    await userEvent.setup().click(searchBox());

    // At most six at a time, by name, and the count says how many there are.
    const options = await screen.findAllByRole("option");
    expect(options.map((option) => option.textContent?.slice(0, 3))).toEqual(["acm", "alb", "eks", "iam", "kms", "rds"]);
    expect(screen.getByText("Showing 6 of 8 templates · type to narrow")).toBeTruthy();
  });

  it("narrows to what is typed, best match first, and counts the matches", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_1", source_template_id: "tmpl_1", name: "redis-operator" }),
      templateRevision({ id: "rev_2", source_template_id: "tmpl_2", name: "network" }),
      templateRevision({ id: "rev_3", source_template_id: "tmpl_3", name: "redis" })
    ]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await user.click(searchBox());
    await user.type(searchBox(), "redis");

    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(2));
    const [first, second] = screen.getAllByRole("option");
    expect(first.textContent?.startsWith("redis")).toBe(true);
    expect(second.textContent?.startsWith("redis-operator")).toBe(true);
    // The letters that matched are marked.
    expect(within(first).getByText("redis", { selector: "mark" })).toBeTruthy();
    expect(screen.getByText("2 matches")).toBeTruthy();
  });

  it("names each template's repository and ref under it, with how many revisions it has", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_2", resolved_commit_sha: "44b2e0199999" }),
      templateRevision({ id: "rev_1" })
    ]);

    renderScreen(queryClient);
    await userEvent.setup().click(searchBox());

    // Two revisions of one template are one choice.
    const [option] = await screen.findAllByRole("option");
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(option.textContent?.startsWith("vpc")).toBe(true);
    expect(option.textContent).toContain("hashicorp/vpc");
    expect(option.textContent).toContain("main");
    expect(option.textContent).toContain("2 revisions");
    // The commit is the Revision select's to show.
    expect(option.textContent).not.toContain("44b2e01");
    // An active latest revision says nothing: a StatusLabel marks itself with its tone.
    expect(option.querySelector("[data-tone]")).toBeNull();
  });

  it("says when nothing matches, and points at registering a template", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await user.click(searchBox());
    await user.type(searchBox(), "kafka");

    expect(await screen.findByText("No templates match this search.")).toBeTruthy();
    expect(screen.queryByRole("option")).toBeNull();
    expect(screen.getByTestId("add-template-search-register").getAttribute("href")).toBe("/templates/new");
  });

  it("closes the search on a pick, shows the template it picked, and configures it", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision(),
      templateRevision({ id: "rev_2", source_template_id: "tmpl_src_2", name: "rds", repo_owner: "my-org", repo_name: "rds" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);

    renderScreen(queryClient);
    expect(screen.queryByTestId("add-stack-template-variables")).toBeNull();

    await chooseTemplate(userEvent.setup(), "vpc");

    expect(screen.queryByRole("listbox")).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Search templates" })).toBeNull();
    const picked = screen.getByTestId("add-template-picked");
    expect(picked.textContent).toContain("vpc");
    expect(picked.textContent).toContain("hashicorp/vpc");
    expect(within(picked).getByRole("button", { name: "Change" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Configure vpc" })).toBeTruthy();
    expect((screen.getByLabelText(/region/) as HTMLInputElement).value).toBe("");
  });

  it("moves focus to the first variable once a template is chosen", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);

    renderScreen(queryClient);
    await chooseTemplate(userEvent.setup(), "vpc");

    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText(/region/)));
  });

  it("goes back to the chosen template when Change is left with Escape", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await chooseTemplate(user, "vpc");
    await user.click(screen.getByRole("button", { name: "Change" }));

    // Change opens the search, focused, over the form it keeps.
    await waitFor(() => expect(document.activeElement).toBe(searchBox()));
    expect(await screen.findAllByRole("option")).toHaveLength(1);

    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.getByTestId("add-template-picked")).toBeTruthy());
    expect(screen.getByRole("heading", { name: "Configure vpc" })).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Change" }));
  });

  it("marks itself unsaved once a variable value is typed, so SessionProvider's proactive re-auth defers", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);

    renderScreen(queryClient);
    await chooseTemplate(userEvent.setup(), "vpc");

    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  // Picking a template resets its revision and values, so picking it again
  // after Change, a second look rather than a new choice, must not.
  it("keeps the chosen revision and typed values when the same template is picked again", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_new", resolved_commit_sha: "f17f9834444" }),
      templateRevision({ id: "rev_old", resolved_commit_sha: "3c0e1126666" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_new"), [variable()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_old"), [variable()]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await chooseTemplate(user, "vpc");
    await user.click(screen.getByRole("combobox", { name: "Revision" }));
    await user.click(within(await screen.findByRole("listbox")).getByRole("option", { name: /3c0e112/ }));
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });

    await user.click(screen.getByRole("button", { name: "Change" }));
    await user.click(await screen.findByRole("option", { name: /^vpc/ }));

    expect(screen.getByRole("combobox", { name: "Revision" }).querySelector('[data-slot="select-value"]')?.textContent).toContain("3c0e112");
    expect((screen.getByLabelText(/region/) as HTMLInputElement).value).toBe("eu-west-1");
  });

  it("does not allow choosing a template with no active revision, and says why", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_pending", status: "pending_validation" })
    ]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await user.click(searchBox());

    const option = await screen.findByRole("option", { name: /^vpc/ });
    expect(option.getAttribute("aria-disabled")).toBe("true");
    // A disabled choice says why, as an icon and a word.
    expect(option.textContent).toContain("waiting for validation");
    expect(option.querySelector("[data-tone]")).not.toBeNull();

    await user.click(option);
    expect(screen.queryByTestId("add-template-picked")).toBeNull();
  });

  it("installs the newest active revision when a newer one failed validation", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_bad", status: "invalid", resolved_commit_sha: "44b2e0199999" }),
      templateRevision({ id: "rev_good", status: "active" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_good"), [variable()]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await user.click(searchBox());

    const option = await screen.findByRole("option", { name: /^vpc/ });
    // Selectable, because a validated revision is still available to install.
    expect(option.getAttribute("aria-disabled")).not.toBe("true");
    // The newest revision's failure is not hidden behind an older success.
    expect(option.textContent).toContain("failed validation");
    expect(option.textContent).toContain("1 revision");

    await user.click(option);

    // What gets installed is the active revision, not the newer broken one.
    const shown = screen.getByRole("combobox", { name: "Revision" }).querySelector('[data-slot="select-value"]')?.textContent ?? "";
    expect(shown).toContain("abcdef1");
    expect(shown).not.toContain("44b2e01");
    expect(screen.getByLabelText(/region/)).toBeTruthy();
  });

  it("adds the chosen revision and opens the new template's Runs", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: "st_new" }), {
        status: 200,
        headers: { "content-type": "application/json" }
      })
    );

    renderScreen(queryClient);

    await chooseTemplate(userEvent.setup(), "vpc");
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add template" }));

    await waitFor(() => expect(screen.getByTestId("location")).toBeTruthy());
    expect(screen.getByTestId("location").textContent).toBe("/stacks/stack_1/templates/st_new/runs");

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/v1/tenants/tenant_123/stacks/stack_1/templates");
    expect(init.method).toBe("POST");
    // No ref is sent: the revision already determines it, and a client-supplied
    // ref could disagree with the revision being installed.
    expect(JSON.parse(init.body as string)).toEqual({
      template_revision_id: "rev_1",
      config: { region: "eu-west-1" }
    });
  });

  it("surfaces a failed install without leaving the screen", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "conflict", message: "already installed" }), {
        status: 409,
        headers: { "content-type": "application/json" }
      })
    );

    renderScreen(queryClient);

    await chooseTemplate(userEvent.setup(), "vpc");
    fireEvent.click(screen.getByRole("button", { name: "Add template" }));

    await waitFor(() => expect(screen.getByTestId("add-stack-template-error")).toBeTruthy());
  });

  it("disables Add template and hides the empty-variables message while the chosen revision's variables are loading", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    // Variables for rev_1 are deliberately left unseeded, and fetch never
    // resolves, so useTemplateRevisionVariablesQuery stays pending.
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderScreen(queryClient);

    await chooseTemplate(userEvent.setup(), "vpc");

    expect(screen.getByTestId("add-stack-template-variables-loading")).toBeTruthy();
    expect(screen.queryByText("This template declares no variables.")).toBeNull();
    expect((screen.getByRole("button", { name: "Add template" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("does not render the previous revision's variable names after switching to a revision whose variables have not loaded", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision(),
      templateRevision({ id: "rev_2", source_template_id: "tmpl_src_2", name: "rds", repo_owner: "my-org", repo_name: "rds" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);
    // rev_2's variables are deliberately left unseeded, and fetch never
    // resolves. Without gating on isFetching, keepPreviousData would keep
    // rendering rev_1's "region" field under rev_2's name.
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderScreen(queryClient);
    const user = userEvent.setup();

    await chooseTemplate(user, "vpc");
    expect(screen.getByLabelText(/region/)).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Change" }));
    await user.click(await screen.findByRole("option", { name: /^rds/ }));

    expect(screen.queryByLabelText(/region/)).toBeNull();
    expect(screen.getByTestId("add-stack-template-variables-loading")).toBeTruthy();
  });

  it("shows an error instead of claiming the template has no variables when they fail to load", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    // A permanently failed variables fetch leaves data undefined, which renders
    // as the empty message — telling the user this template declares no
    // variables when in truth we could not find out. Install must not be
    // offered against a config we cannot build.
    //
    // The 401 also drives client.ts's fetchWithAuth to navigate via
    // globalThis.location.assign; stub it so jsdom does not attempt (and
    // warn about) a real navigation.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "unauthorized", message: "unauthorized" }), {
        status: 401,
        headers: { "content-type": "application/json" }
      })
    );

    renderScreen(queryClient);

    await chooseTemplate(userEvent.setup(), "vpc");

    await waitFor(() => expect(screen.getByTestId("add-stack-template-variables-error")).toBeTruthy());
    expect(screen.queryByText("This template declares no variables.")).toBeNull();
    expect((screen.getByRole("button", { name: "Add template" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("shows the one active revision in the picker, so what will be installed is on screen", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision(),
      // Not active, so not offered.
      templateRevision({ id: "rev_old", status: "invalid", resolved_commit_sha: "44b2e0199999" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await chooseTemplate(user, "vpc");

    await user.click(screen.getByRole("combobox", { name: "Revision" }));
    const options = within(await screen.findByRole("listbox")).getAllByRole("option").map((option) => option.textContent ?? "");
    expect(options).toHaveLength(1);
    expect(options[0]).toContain("abcdef1");
  });

  it("lists a template's active revisions newest-registered first, defaulting to the latest", async () => {
    const queryClient = testQueryClient();
    // The API returns created_at desc, id desc; the screen renders that order
    // as received and must not re-sort it.
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_new", resolved_commit_sha: "f17f9834444", created_at: "2026-08-19T10:00:00Z" }),
      templateRevision({ id: "rev_mid", resolved_commit_sha: "a91c2045555", created_at: "2026-08-02T09:30:00Z" }),
      templateRevision({ id: "rev_old", resolved_commit_sha: "3c0e1126666", created_at: "2026-06-28T14:15:00Z" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_new"), [variable()]);

    renderScreen(queryClient);
    const user = userEvent.setup();
    await chooseTemplate(user, "vpc");

    const select = screen.getByRole("combobox", { name: "Revision" });
    expect(select.querySelector('[data-slot="select-value"]')?.textContent).toContain("f17f983");

    await user.click(select);
    const options = within(await screen.findByRole("listbox")).getAllByRole("option").map((option) => option.textContent ?? "");
    expect(options).toHaveLength(3);
    expect(options[0]).toContain("f17f983");
    expect(options[0]).toContain("19 Aug 2026");
    expect(options[0]).toContain("latest");
    expect(options[1]).toContain("a91c204");
    expect(options[1]).not.toContain("latest");
    expect(options[2]).toContain("3c0e112");
  });

  it("installs the revision chosen in the picker rather than the default", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision({ id: "rev_new", resolved_commit_sha: "f17f9834444" }),
      templateRevision({ id: "rev_old", resolved_commit_sha: "3c0e1126666" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_new"), [variable()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_old"), [variable()]);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: "st_new" }), {
        status: 200,
        headers: { "content-type": "application/json" }
      })
    );

    renderScreen(queryClient);
    const user = userEvent.setup();
    await chooseTemplate(user, "vpc");
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });
    await user.click(screen.getByRole("combobox", { name: "Revision" }));
    await user.click(within(await screen.findByRole("listbox")).getByRole("option", { name: /3c0e112/ }));
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());

    // Switching revision clears typed values, the same as choosing a template:
    // the new revision's variables are what the config must be built from.
    expect((screen.getByLabelText(/region/) as HTMLInputElement).value).toBe("");

    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "us-east-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add template" }));

    await waitFor(() => expect(screen.getByTestId("location")).toBeTruthy());
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({
      template_revision_id: "rev_old",
      config: { region: "us-east-1" }
    });
  });

  it("points at template registration when the tenant has none", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), []);

    renderScreen(queryClient);

    expect(screen.getByTestId("add-stack-template-none")).toBeTruthy();
    expect(screen.getByTestId("register-template-link").getAttribute("href")).toBe("/templates/new");
  });

  it("replaces the template header with its own: what it is, which stack, and Cancel", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);

    renderScreen(queryClient);

    expect(screen.getByRole("heading", { level: 2, name: "Add template" })).toBeTruthy();
    expect(screen.getByText("to Payments")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 3, name: "Choose a template" })).toBeTruthy();
    await userEvent.setup().click(screen.getByRole("link", { name: "Cancel" }));
    expect(screen.getByTestId("location").textContent).toBe("/stacks/stack_1");
  });

  // The add's response is not labelled: it has no display name and no ref.
  // So the panel opens once the stack has reloaded with the new template,
  // labelled: it never shows the workspace name, or calls the template missing.
  it("opens the new template's panel once the stack has reloaded with it, labelled", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);
    queryClient.setQueryData(queryKeys.attention("tenant_123"), []);
    let answerStack: (response: Response) => void = () => {};
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation((input, init) => {
      if (init?.method === "POST") {
        return Promise.resolve(jsonResponse({ ...installedTemplate("st_new"), source_ref: "" }, 201));
      }
      if (String(input).endsWith("/stacks/stack_1")) {
        return new Promise<Response>((resolve) => {
          answerStack = resolve;
        });
      }
      return new Promise<Response>(() => {});
    });

    renderScreen(
      queryClient,
      <Route path="/stacks/:stackId/templates/:stackTemplateId" element={<TemplatePanel />}>
        <Route path="runs" element={<LocationProbe />} />
      </Route>
    );
    await chooseTemplate(userEvent.setup(), "vpc");
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add template" }));

    // While the stack reloads, the screen stays, with Add template busy.
    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/stacks/stack_1"))).toBe(true));
    expect(screen.queryByTestId("template-panel")).toBeNull();
    expect((screen.getByRole("button", { name: "Add template" }) as HTMLButtonElement).disabled).toBe(true);

    answerStack(jsonResponse(stackView([{ ...installedTemplate("st_new"), display_name: "vpc" }])));

    expect(await screen.findByRole("heading", { level: 2, name: "vpc" })).toBeTruthy();
    expect(screen.queryByText("ws-vpc")).toBeNull();
    expect(screen.queryByTestId("stack-template-missing")).toBeNull();
    expect(screen.getByTestId("location").textContent).toBe("/stacks/stack_1/templates/st_new/runs");
  });
});

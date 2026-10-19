// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { StackCapabilities } from "../../auth/types";
import { queryKeys } from "../../api/queryKeys";
import type { Stack, TemplateRun, TemplateRunLog } from "../../api/types";
import RunDetailScreen from "./RunDetailScreen";
import { asListBody, pagesOf } from "../../api/listTestSupport";
import { FakeEventSource } from "./eventSourceTestSupport";

function stack(capabilities: StackCapabilities): Stack {
  return {
    id: "stack_1",
    tenant_id: "tenant_123",
    name: "Payments",
    slug: "payments",
    slug_is_derived: true,
    tags: {},
    default_credential_ids: [],
    created_by: "user_123",
    created_at: "2026-07-19T00:00:00Z",
    effective_capabilities: capabilities
  };
}

function run(overrides: Partial<TemplateRun> = {}): TemplateRun {
  return {
    id: "run_1",
    tenant_id: "tenant_123",
    stack_template_id: "stpl_1",
    template_revision_id: "rev_1",
    operation: "plan",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    status: "completed",
    step: "",
    trigger_actor: "user_123",
    trigger_actor_display_name: "user_123",
    created_at: "2026-07-20T00:00:00Z",
    error_summary: "",
    run_number: 1,
    auto_approve: false,
    plan_summary: null,
    plan_changes: null,
    ...overrides
  };
}

// What wrote each log, by its name, as the API reports it.
// What wrote each log, by its name, as the API reports it.
const LOG_COMMANDS: Record<string, Pick<TemplateRunLog, "run_phase" | "seq" | "command" | "command_line">> = {
  "plan-1": { run_phase: "plan", seq: 1, command: "init", command_line: "tofu init" },
  "plan-2": { run_phase: "plan", seq: 2, command: "select_workspace", command_line: "tofu workspace select" },
  "plan-3": { run_phase: "plan", seq: 3, command: "plan", command_line: "tofu plan" },
  "apply-1": { run_phase: "apply", seq: 1, command: "init", command_line: "tofu init" },
  "apply-2": { run_phase: "apply", seq: 2, command: "select_workspace", command_line: "tofu workspace select" },
  "apply-3": { run_phase: "apply", seq: 3, command: "apply", command_line: "tofu apply" }
};

// A complete log unless a test says otherwise. The API sends the upload's
// fields only for a complete log.
function runLog(overrides: Partial<TemplateRunLog> = {}): TemplateRunLog {
  const name = overrides.name ?? "plan-3";
  const state = overrides.state ?? "complete";
  return {
    tenant_id: "tenant_123",
    run_id: "run_1",
    name,
    ...(LOG_COMMANDS[name] ?? { run_phase: "plan", seq: 1, command: name, command_line: name }),
    state,
    ...(state === "complete" ? { content_type: "text/plain", size_bytes: 10, uploaded_at: "2026-07-20T00:01:00Z" } : {}),
    ...overrides
  };
}

const allAllowed: StackCapabilities = { can_view: true, can_operate: true, can_approve: true, can_manage_access: true };

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function authValue(): AuthContextValue {
  return {
    me: { sub: "user_1", tenant_id: "tenant_123", display_name: "Test User", global_capabilities: { can_create_stack: false, can_publish_template: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

function seedCapabilities(queryClient: QueryClient, capabilities: StackCapabilities) {
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), { stack: stack(capabilities), templates: [] });
}

// The URL names the run by number; the screen finds its id in the template's
// runs list when the loaded pages hold it, and reads it by number when they
// don't. Every test here is about the run itself, so the list is seeded with
// run #1 unless a test has already put something else there.
function renderScreen(queryClient: QueryClient, auth?: AuthContextValue, runNumber = "1") {
  if (queryClient.getQueryData(queryKeys.templateRuns("tenant_123", "stpl_1")) === undefined) {
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "stpl_1"), pagesOf([run()]));
  }
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth ?? authValue()}>
        <MemoryRouter initialEntries={[`/stacks/stack_1/templates/stpl_1/runs/${runNumber}`]}>
          <Routes>
            <Route path="/stacks/:stackId/templates/:stackTemplateId/runs/:runNumber" element={<RunDetailScreen />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(asListBody(body)), { status, headers: { "content-type": "application/json" } });
}

function isDisabled(element: HTMLElement): boolean {
  return (element as HTMLButtonElement).disabled;
}

describe("RunDetailScreen", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("finds the run by its number within the template", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "stpl_1"), pagesOf([
      run({ id: "run_newer", run_number: 2 }),
      run({ id: "run_older", run_number: 1 })
    ]));
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_older"), run({ id: "run_older", run_number: 1, status: "failed", error_summary: "the older one" }));
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (String(input).includes("/template-runs/run_older/logs")) {
        return jsonResponse([]);
      }
      throw new Error(`unexpected fetch: ${String(input)}`);
    });

    renderScreen(queryClient, undefined, "1");

    // The breadcrumb names the run; the screen shows the run it resolved.
    expect(screen.getByTestId("run-detail-status").textContent).toContain("plan failed");
    expect(screen.getByText("the older one")).toBeTruthy();
  });

  it("says so when the template has no run with that number", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      if (String(input).endsWith("/stack-templates/stpl_1/runs/9")) {
        return jsonResponse({ type: "about:blank", title: "Not Found", status: 404 }, 404);
      }
      throw new Error(`unexpected fetch: ${String(input)}`);
    });

    renderScreen(queryClient, undefined, "9");

    expect((await screen.findByTestId("run-detail-missing")).textContent).toContain("#9");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  // The runs list loads a page at a time, so an old run's link can name one
  // the loaded pages don't hold. It is read by its number instead.
  it("opens a run beyond the loaded pages by its number", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    const older = run({ id: "run_old", run_number: 3, status: "completed", error_summary: "" });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/stack-templates/stpl_1/runs/3")) return jsonResponse(older);
      if (url.endsWith("/template-runs/run_old")) return jsonResponse(older);
      if (url.includes("/template-runs/run_old/logs")) return jsonResponse([]);
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient, undefined, "3");

    expect((await screen.findByTestId("run-detail-screen")).textContent).toContain("Run #3");
  });

  it("shows a loading state while the run query is pending", () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderScreen(testQueryClient());

    expect(screen.getByTestId("run-detail-loading")).toBeTruthy();
  });

  it("renders the shared boundary screen for a handled API error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ type: "about:blank", title: "Service Unavailable", detail: "service unavailable" }, 503));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-service-unavailable")).toBeTruthy());
  });

  it("renders the generic error state when the API returns 401", async () => {
    // The 401 drives client.ts's fetchWithAuth to navigate via
    // globalThis.location.assign; stub it so jsdom does not attempt (and
    // warn about) a real navigation.
    vi.stubGlobal("location", { ...window.location, assign: vi.fn() });
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ type: "about:blank", title: "Unauthorized", detail: "unauthorized" }, 401));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("run-detail-error")).toBeTruthy());
  });

  it("renders AccessDenied when the API returns 403", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ type: "about:blank", title: "Forbidden", detail: "forbidden" }, 403));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });

  it("renders NotFound when the API returns 404", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ type: "about:blank", title: "Not Found", detail: "not found" }, 404));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("route-not-found")).toBeTruthy());
  });

  it("renders a retryable generic error state for unhandled failures", async () => {
    let runCalls = 0;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/template-runs/run_1")) {
        runCalls += 1;
        if (runCalls === 1) {
          throw new TypeError("network down");
        }
        return jsonResponse(run());
      }
      if (url.endsWith("/logs")) {
        return jsonResponse([]);
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("run-detail-error")).toBeTruthy());
    fireEvent.click(screen.getByTestId("run-detail-retry"));
    await waitFor(() => expect(screen.getByTestId("run-detail-screen")).toBeTruthy());
    expect(fetchMock.mock.calls.filter(([reqInput]) => String(reqInput).endsWith("/template-runs/run_1")).length).toBe(2);
  });

  it("renders the run summary and stacks its logs, the latest open and the rest opening on click", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ status: "completed", run_number: 4 }));

    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/logs")) {
        return jsonResponse([runLog({ name: "plan-1" }), runLog({ name: "plan-3" })]);
      }
      if (url.endsWith("/logs/plan-3")) {
        return new Response("plan-3 log body", { status: 200, headers: { "content-type": "text/plain" } });
      }
      if (url.endsWith("/logs/plan-1")) {
        return new Response("init log body", { status: 200, headers: { "content-type": "text/plain" } });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient);

    expect(screen.getByTestId("run-detail-screen")).toBeTruthy();
    expect(screen.getByTestId("run-detail-status").textContent).toBe("no changes");
    expect(screen.getByTestId("run-detail-status").getAttribute("data-tone")).toBe("settled");
    expect(within(screen.getByTestId("run-logs-panel")).getByRole("heading", { name: "Logs" })).toBeTruthy();
    expect(screen.getByText("main @ abcdef1")).toBeTruthy();
    await waitFor(() => expect(screen.getByText("plan-3 log body")).toBeTruthy());
    expect(screen.getByTestId("log-scroll-area-plan-3").getAttribute("data-slot")).toBe("scroll-area");
    // Rows read the command that ran, under the phase that ran it.
    const initToggle = screen.getByRole("button", { name: "tofu init" });
    const planToggle = screen.getByRole("button", { name: "tofu plan" });
    expect(screen.getByRole("heading", { level: 5, name: "Plan" })).toBeTruthy();
    expect(initToggle.getAttribute("aria-expanded")).toBe("false");
    expect(planToggle.getAttribute("aria-expanded")).toBe("true");
    // A closed row does not fetch its log.
    expect(fetchMock.mock.calls.some(([reqInput]) => String(reqInput).endsWith("/logs/plan-1"))).toBe(false);

    // Opening one phase leaves the others as they were.
    fireEvent.click(initToggle);
    await waitFor(() => expect(screen.getByText("init log body")).toBeTruthy());
    expect(screen.getByText("plan-3 log body")).toBeTruthy();

    fireEvent.click(planToggle);
    expect(planToggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("plan-3 log body")).toBeNull();
    expect(screen.getByText("init log body")).toBeTruthy();
  });

  it("groups an approved run's logs under its plan and its apply, saying who started each", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({
        operation: "apply",
        executions: [
          { phase: "plan", actor: "user_123", actor_display_name: "Priya Raman", started_at: "2026-07-20T00:00:00Z" },
          { phase: "apply", actor: "user_456", actor_display_name: "Sam Ortiz", started_at: "2026-07-21T09:12:00Z" }
        ]
      })
    );
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/logs")) {
        return jsonResponse(["plan-1", "plan-2", "plan-3", "apply-1", "apply-2", "apply-3"].map((name) => runLog({ name })));
      }
      return new Response("log body", { status: 200, headers: { "content-type": "text/plain" } });
    });

    renderScreen(queryClient);

    const panel = screen.getByTestId("run-logs-panel");
    await waitFor(() => expect(within(panel).getAllByRole("button", { name: "tofu init" })).toHaveLength(2));
    const plan = within(panel).getByRole("heading", { level: 5, name: /^Plan/ });
    const apply = within(panel).getByRole("heading", { level: 5, name: /^Apply/ });
    expect(plan.textContent).toContain("started by Priya Raman");
    // The apply was started by approving the plan.
    expect(apply.textContent).toContain("approved by Sam Ortiz");
    // Each phase holds its own init, and the apply's apply opens as the latest.
    const applyGroup = apply.parentElement as HTMLElement;
    expect(within(applyGroup).getByRole("button", { name: "tofu init" })).toBeTruthy();
    expect(within(applyGroup).getByRole("button", { name: "tofu apply" }).getAttribute("aria-expanded")).toBe("true");
  });

  it("labels a log by its name when the API sends no command line", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run());
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/logs")) {
        return jsonResponse([runLog({ name: "plan-1", command: "refresh", command_line: "" })]);
      }
      return new Response("log body", { status: 200, headers: { "content-type": "text/plain" } });
    });

    renderScreen(queryClient);

    await waitFor(() => expect(screen.getByRole("button", { name: "plan-1" })).toBeTruthy());
  });

  it("opens each newer phase as it arrives, keeps the rows already open or closed, and fetches each log once", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    let current = run({ status: "running", step: "initializing" });
    let phases = ["plan-1"];
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), current);

    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/template-runs/run_1")) {
        return jsonResponse(current);
      }
      if (url.endsWith("/logs")) {
        return jsonResponse(phases.map((name) => runLog({ name })));
      }
      const phase = url.split("/logs/")[1];
      if (phase) {
        return new Response(`${phase} log body`, { status: 200, headers: { "content-type": "text/plain" } });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });
    // The run moving is what refetches its log list.
    function advance(step: TemplateRun["step"], nextPhases: string[]) {
      phases = nextPhases;
      current = run({ status: "running", step });
      act(() => queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), current));
    }

    renderScreen(queryClient);
    await waitFor(() => expect(screen.getByText("plan-1 log body")).toBeTruthy());

    advance("selecting_workspace", ["plan-1", "plan-2"]);
    await waitFor(() => expect(screen.getByText("plan-2 log body")).toBeTruthy());
    expect(screen.getByText("plan-1 log body")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "tofu workspace select" }));
    advance("planning", ["plan-1", "plan-2", "plan-3"]);
    await waitFor(() => expect(screen.getByText("plan-3 log body")).toBeTruthy());
    expect(screen.getByRole("button", { name: "tofu workspace select" }).getAttribute("aria-expanded")).toBe("false");
    expect(screen.getByText("plan-1 log body")).toBeTruthy();

    // The run moved three times; a log that was already fetched is not fetched again.
    const logFetches = (phase: string) =>
      fetchMock.mock.calls.filter(([reqInput]) => String(reqInput).endsWith(`/logs/${phase}`)).length;
    expect(logFetches("plan-1")).toBe(1);
    expect(logFetches("plan-2")).toBe(1);
  });

  it("says the logs are loading until the list arrives, and then that a running run has none yet", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    const running = run({ status: "running", step: "waiting_for_executor" });
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), running);
    let resolveLogs: ((response: Response) => void) | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/template-runs/run_1")) {
        return Promise.resolve(jsonResponse(running));
      }
      if (url.endsWith("/logs")) {
        return new Promise((resolve) => {
          resolveLogs = resolve;
        });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient);

    expect(screen.getByText("Loading logs…")).toBeTruthy();
    await waitFor(() => expect(resolveLogs).toBeDefined());
    resolveLogs?.(jsonResponse([]));
    await waitFor(() => expect(screen.getByText("No logs yet.")).toBeTruthy());
  });

  it("says a finished run with no logs has none", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ status: "canceled" }));
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => jsonResponse([]));

    renderScreen(queryClient);

    await waitFor(() => expect(screen.getByText("No logs.")).toBeTruthy());
  });

  it("says so when the run's logs, or one log, cannot be loaded", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run());
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500));

    renderScreen(queryClient);

    await waitFor(() => expect(screen.getByText("Could not load this run's logs.")).toBeTruthy());
  });

  it("shows a log loading, and then that it could not be loaded", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run());
    let resolveLog: ((response: Response) => void) | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const url = String(input);
      if (url.endsWith("/logs")) {
        return Promise.resolve(jsonResponse([runLog({ name: "plan-3" })]));
      }
      if (url.endsWith("/logs/plan-3")) {
        return new Promise((resolve) => {
          resolveLog = resolve;
        });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient);

    await waitFor(() => expect(screen.getByText("Loading…")).toBeTruthy());
    resolveLog?.(jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500));
    await waitFor(() => expect(screen.getByText("Could not load this log.")).toBeTruthy());
  });

  // The command still running has a live row, which follows its output as it
  // is written instead of asking for a body the API does not have yet.
  it("tails a live log, and loads its full body once it completes", async () => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    const running = run({ status: "running", step: "planning" });
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), running);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/template-runs/run_1")) return jsonResponse(running);
      if (url.endsWith("/logs")) return jsonResponse([runLog({ name: "plan-1" }), runLog({ name: "plan-3", state: "live" })]);
      if (url.endsWith("/logs/plan-3")) {
        return new Response("plan-3 full body", { status: 200, headers: { "content-type": "text/plain" } });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });
    const bodyFetches = () => fetchMock.mock.calls.filter(([reqInput]) => String(reqInput).endsWith("/logs/plan-3")).length;

    renderScreen(queryClient);

    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const source = FakeEventSource.instances[0];
    expect(source.url).toBe("/v1/tenants/tenant_123/template-runs/run_1/logs/plan-3/events");
    expect(screen.getByText("Streaming")).toBeTruthy();

    act(() => source.dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));
    act(() => source.dispatch("log", { offset: 12, end: 28, data: "Plan: 1 to add.\n" }, "2-0"));
    const panel = screen.getByTestId("log-scroll-area-plan-3");
    expect(panel.textContent).toContain("Planning...\nPlan: 1 to add.\n");
    // The dots are decoration; the status they stand for is read out once.
    expect(within(panel).getByRole("status").textContent).toContain("Streaming");

    act(() => source.dispatch("log_truncated", {}));
    expect(screen.getByText("Live output stops here; load the full log once the command finishes.")).toBeTruthy();

    act(() => source.dispatch("log_end", { state: "complete" }));
    expect(source.closeCalls).toBe(1);
    expect(screen.queryByText("Streaming")).toBeNull();
    expect(bodyFetches()).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Load the full log" }));
    await waitFor(() => expect(screen.getByText("plan-3 full body")).toBeTruthy());
    expect(bodyFetches()).toBe(1);
  });

  // The list refetches when the run moves, and can say a log has ended
  // before its stream does. What streamed stays on screen either way.
  function liveRowThatMovesOn(queryClient: QueryClient) {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    seedCapabilities(queryClient, allAllowed);
    let current = run({ status: "running", step: "planning" });
    let logs = [runLog({ name: "plan-3", state: "live" })];
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), current);
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/template-runs/run_1")) return jsonResponse(current);
      if (url.endsWith("/logs")) return jsonResponse(logs);
      if (url.endsWith("/logs/plan-3")) {
        return new Response("plan-3 full body", { status: 200, headers: { "content-type": "text/plain" } });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });
    const moveOn = (next: TemplateRunLog) => {
      logs = [next];
      current = run({ status: "failed", step: "planning" });
      act(() => queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), current));
    };
    const bodyFetches = () => fetchMock.mock.calls.filter(([reqInput]) => String(reqInput).endsWith("/logs/plan-3")).length;
    const listFetches = () => fetchMock.mock.calls.filter(([reqInput]) => String(reqInput).endsWith("/logs")).length;
    return { moveOn, bodyFetches, listFetches };
  }

  it("keeps a live log's output when its row turns lost, and says why", async () => {
    const queryClient = testQueryClient();
    const { moveOn, bodyFetches } = liveRowThatMovesOn(queryClient);

    renderScreen(queryClient);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    act(() => FakeEventSource.instances[0].dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));

    moveOn(runLog({ name: "plan-3", state: "lost", lost_reason: "executor_gone" }));

    await waitFor(() => expect(screen.getByText("Log lost: the executor stopped")).toBeTruthy());
    expect(screen.getByTestId("log-scroll-area-plan-3").textContent).toContain("Planning...\n");
    expect(screen.queryByText("Streaming")).toBeNull();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(bodyFetches()).toBe(0);
  });

  // Usually the stream carried every byte the stored log holds: the screen
  // already shows the whole log, so nothing is offered.
  it("keeps a live log's output when its row turns complete, and offers nothing when the stream carried it all", async () => {
    const queryClient = testQueryClient();
    const { moveOn, bodyFetches } = liveRowThatMovesOn(queryClient);

    renderScreen(queryClient);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const source = FakeEventSource.instances[0];
    act(() => source.dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));

    moveOn(runLog({ name: "plan-3", size_bytes: 12 }));
    act(() => source.dispatch("log_end", { state: "complete" }));

    await waitFor(() => expect(screen.queryByText("Streaming")).toBeNull());
    expect(screen.getByTestId("log-scroll-area-plan-3").textContent).toContain("Planning...\n");
    expect(screen.queryByRole("button", { name: "Load the full log" })).toBeNull();
    expect(bodyFetches()).toBe(0);
  });

  // A stream that missed output, say while Valkey was away, carried fewer
  // bytes than were stored: the stored log is offered, with why.
  it("offers the full log once the stream has ended short of what was stored", async () => {
    const queryClient = testQueryClient();
    const { moveOn, bodyFetches } = liveRowThatMovesOn(queryClient);

    renderScreen(queryClient);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const source = FakeEventSource.instances[0];
    act(() => source.dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));

    moveOn(runLog({ name: "plan-3", size_bytes: 40 }));
    await waitFor(() => expect(screen.queryByText("Streaming")).toBeNull());
    // The row is complete, but a last piece may still be on its way.
    expect(screen.queryByRole("button", { name: "Load the full log" })).toBeNull();

    act(() => source.dispatch("log_end", { state: "complete" }));
    expect(screen.getByText("Some output didn't stream.")).toBeTruthy();
    expect(bodyFetches()).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Load the full log" }));
    await waitFor(() => expect(screen.getByText("plan-3 full body")).toBeTruthy());
    expect(bodyFetches()).toBe(1);
  });

  // A stream can skip bytes, as when Valkey restarts mid-command and the
  // executor carries on from its own offset: the stream then ends at the
  // stored size, but what it showed has a hole in it.
  it("offers the full log when the stream skipped bytes, even if it ended at the stored size", async () => {
    const queryClient = testQueryClient();
    const { moveOn, bodyFetches } = liveRowThatMovesOn(queryClient);

    renderScreen(queryClient);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const source = FakeEventSource.instances[0];
    act(() => source.dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));
    act(() => source.dispatch("log", { offset: 30, end: 40, data: "1 to add.\n" }, "2-0"));

    moveOn(runLog({ name: "plan-3", size_bytes: 40 }));
    act(() => source.dispatch("log_end", { state: "complete" }));

    await waitFor(() => expect(screen.getByText("Some output didn't stream.")).toBeTruthy());
    expect(screen.getByRole("button", { name: "Load the full log" })).toBeTruthy();
    expect(bodyFetches()).toBe(0);
  });

  // The stream can end complete before the list says so. Loading the body
  // then, and again once the list carries uploaded_at, is one fetch: a
  // command's log is uploaded once.
  it("fetches the full log once when the list catches up after it was loaded", async () => {
    const queryClient = testQueryClient();
    const { moveOn, bodyFetches, listFetches } = liveRowThatMovesOn(queryClient);

    renderScreen(queryClient);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const source = FakeEventSource.instances[0];
    act(() => source.dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));
    act(() => source.dispatch("log_truncated", {}));
    act(() => source.dispatch("log_end", { state: "complete" }));

    fireEvent.click(screen.getByRole("button", { name: "Load the full log" }));
    await waitFor(() => expect(screen.getByText("plan-3 full body")).toBeTruthy());

    const listsBefore = listFetches();
    moveOn(runLog({ name: "plan-3", size_bytes: 40 }));
    await waitFor(() => expect(listFetches()).toBe(listsBefore + 1));
    // Give a refetch keyed on the new uploaded_at the chance to start.
    await act(() => new Promise((resolve) => setTimeout(resolve, 20)));
    expect(screen.getByText("plan-3 full body")).toBeTruthy();
    expect(bodyFetches()).toBe(1);
  });

  // A truncated stream always falls short, and says why itself. Loading the
  // stored body replaces the stream, which closes it.
  it("offers the full log of a truncated stream as soon as its row is complete", async () => {
    const queryClient = testQueryClient();
    const { moveOn, bodyFetches } = liveRowThatMovesOn(queryClient);

    renderScreen(queryClient);
    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    const source = FakeEventSource.instances[0];
    act(() => source.dispatch("log", { offset: 0, end: 12, data: "Planning...\n" }, "1-0"));
    act(() => source.dispatch("log_truncated", {}));

    moveOn(runLog({ name: "plan-3", size_bytes: 40 }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Load the full log" })).toBeTruthy());
    expect(screen.queryByText("Some output didn't stream.")).toBeNull();
    expect(source.closeCalls).toBe(0);

    fireEvent.click(screen.getByRole("button", { name: "Load the full log" }));
    await waitFor(() => expect(screen.getByText("plan-3 full body")).toBeTruthy());
    expect(bodyFetches()).toBe(1);
    expect(source.closeCalls).toBe(1);
  });

  it("closes a live log's stream when its row closes", async () => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    const running = run({ status: "running", step: "planning" });
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), running);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/template-runs/run_1")) return jsonResponse(running);
      if (url.endsWith("/logs")) return jsonResponse([runLog({ name: "plan-3", state: "live" })]);
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient);

    await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "tofu plan" }));
    expect(FakeEventSource.instances[0].closeCalls).toBe(1);
  });

  it("says why a lost log has no body, without asking for one", async () => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ status: "failed" }));
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/logs")) return jsonResponse([runLog({ name: "plan-3", state: "lost", lost_reason: "upload_failed" })]);
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient);

    await waitFor(() => expect(screen.getByText("Log lost: the upload failed")).toBeTruthy());
    expect(fetchMock.mock.calls.some(([reqInput]) => String(reqInput).includes("/logs/"))).toBe(false);
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it("fetches a complete log's body once, and follows no stream for it", async () => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run());
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.endsWith("/logs")) return jsonResponse([runLog({ name: "plan-3" })]);
      if (url.endsWith("/logs/plan-3")) {
        return new Response("plan-3 log body", { status: 200, headers: { "content-type": "text/plain" } });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });

    renderScreen(queryClient);

    await waitFor(() => expect(screen.getByText("plan-3 log body")).toBeTruthy());
    expect(fetchMock.mock.calls.filter(([reqInput]) => String(reqInput).endsWith("/logs/plan-3"))).toHaveLength(1);
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  // A subject from the identity provider is opaque (Dex's is base64 of a
  // protobuf), so it is never what a person reads.
  it("leads with a trail back to the template's runs and titles the run", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ run_number: 1 }));
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    const trail = screen.getByRole("navigation", { name: "Run" });
    expect(within(trail).getByRole("link", { name: "Runs" }).getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs");
    expect(within(trail).getByText("Run #1").getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("heading", { level: 3, name: "Run #1" })).toBeTruthy();
  });

  it("says an unfinished run has not finished", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      // The API omits completed_at until the run ends.
      run({ status: "waiting_approval", plan_summary: { add: 1, change: 0, destroy: 0 } })
    );
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(screen.getByText("Finished").nextElementSibling?.textContent).toBe("Not finished");
  });

  it("names who started the run rather than showing their subject", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({
        trigger_actor: "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs",
        trigger_actor_display_name: "Ada Lovelace"
      })
    );
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(screen.getByText("Started by").nextElementSibling?.textContent).toBe("Ada Lovelace");
    expect(screen.queryByText("CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs")).toBeNull();
  });

  it("renders the activity failure summary for a failed run", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ status: "failed", error_summary: "template run activity failed: log upload failed" })
    );
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(screen.getByText("template run activity failed: log upload failed")).toBeTruthy();
    // A finished run takes no actions, so none are offered.
    expect(screen.queryByRole("button", { name: /Cancel/ })).toBeNull();
  });

  it("offers Approve and Discard for a plan waiting for approval, and shows what it would change", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ status: "waiting_approval", plan_summary: { add: 2, change: 0, destroy: 1 } })
    );
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(isDisabled(screen.getByRole("button", { name: /^Approve$/ }))).toBe(false);
    expect(screen.getByRole("button", { name: /Discard/ })).toBeTruthy();
    expect(screen.getByRole("img", { name: "2 to add, 0 to change, 1 to destroy" })).toBeTruthy();
  });

  // Approving a destroy plan is what destroys, so it opens an accessible modal
  // confirmation before it calls the approval endpoint.
  it("traps focus in destroy confirmation, restores focus on cancel, and approves once", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ operation: "destroy", status: "waiting_approval", plan_summary: { add: 0, change: 0, destroy: 4 } })
    );
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") {
        return jsonResponse(run({ operation: "destroy", status: "approved", plan_summary: { add: 0, change: 0, destroy: 4 } }), 202);
      }
      if (String(input).endsWith("/template-runs/run_1")) {
        return jsonResponse(run({ operation: "destroy", status: "approved", plan_summary: { add: 0, change: 0, destroy: 4 } }));
      }
      return jsonResponse([]);
    });

    renderScreen(queryClient);
    const user = userEvent.setup();
    const destroyTrigger = screen.getByRole("button", { name: /Destroy 4/ });
    await user.click(destroyTrigger);

    const dialog = await screen.findByRole("alertdialog");
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
    await user.tab();
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
    await user.tab();
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
    await user.tab();
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(document.activeElement).toBe(destroyTrigger);
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(false);

    await user.click(destroyTrigger);
    const reopenedDialog = await screen.findByRole("alertdialog");
    await user.click(within(reopenedDialog).getByRole("button", { name: /Confirm destroy/ }));
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/template-runs/run_1/approve"),
        expect.objectContaining({ method: "POST" })
      )
    );
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
    // The approval's own answer moves the run off waiting_approval.
    await waitFor(() => expect(screen.queryByRole("button", { name: /Destroy 4/ })).toBeNull());
  });

  it("hides Approve when canApprove is denied", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, { ...allAllowed, can_approve: false });
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ status: "waiting_approval" }));
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(screen.queryByRole("button", { name: /^Approve$/ })).toBeNull();
  });

  // A run planning or applying cannot be stopped, so it offers no action,
  // whatever the viewer may do.
  it.each(["queued", "running"] as const)("offers nothing on a run that is %s", (status) => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ status }));
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(screen.queryByRole("button", { name: /Cancel|Discard|Approve/ })).toBeNull();
  });

  it("discards a plan waiting for approval", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ status: "waiting_approval", operation: "apply" }));
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      if (init?.method === "POST" && url.endsWith("/template-runs/run_1/discard")) {
        return new Response(null, { status: 204 });
      }
      if (url.endsWith("/template-runs/run_1")) {
        return jsonResponse(run({ status: "canceled", operation: "apply" }));
      }
      return jsonResponse([]);
    });

    renderScreen(queryClient);

    fireEvent.click(screen.getByRole("button", { name: /Discard/ }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        expect.stringContaining("/template-runs/run_1/discard"),
        expect.objectContaining({ method: "POST", body: JSON.stringify({ reason: "discarded from run detail" }) })
      )
    );
  });
});

describe("RunDetailScreen changes", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  const changes = [
    {
      address: "aws_db_instance.main", previous_address: "", module_address: "", mode: "managed", type: "aws_db_instance", name: "main",
      provider_name: "aws", deposed: "", action: "replace", create_before_destroy: false, action_reason: "replace_because_cannot_update",
      importing_id: "", replace_paths: [["storage_encrypted"]],
      before: { storage_encrypted: false, password: "(sensitive)" }, after: { storage_encrypted: true, password: "(sensitive)" }
    },
    {
      address: "aws_s3_bucket.audit_logs", previous_address: "", module_address: "", mode: "managed", type: "aws_s3_bucket", name: "audit_logs",
      provider_name: "aws", deposed: "", action: "destroy", create_before_destroy: false, action_reason: "delete_because_no_resource_config",
      importing_id: "", replace_paths: null, before: { bucket: "acme-prod-audit-logs" }, after: null
    },
    {
      address: "aws_iam_role.monitoring", previous_address: "", module_address: "", mode: "managed", type: "aws_iam_role", name: "monitoring",
      provider_name: "aws", deposed: "", action: "create", create_before_destroy: false, action_reason: "", importing_id: "",
      replace_paths: null, before: null, after: { name: "monitoring" }
    }
  ];

  function stubAPI() {
    return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = String(input);
      if (url.includes("/changes")) {
        return jsonResponse(changes);
      }
      if (url.endsWith("/logs")) {
        return jsonResponse([runLog({ name: "plan-3" })]);
      }
      if (url.endsWith("/logs/plan-3")) {
        return new Response("plan-3 log body", { status: 200, headers: { "content-type": "text/plain" } });
      }
      throw new Error(`unexpected fetch: ${url}`);
    });
  }

  it("opens a waiting plan on its changes, removals first, filtered by action", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ operation: "apply", status: "waiting_approval", plan_summary: { add: 2, change: 0, destroy: 2 }, plan_changes: { state: "complete", total: 3 } })
    );
    stubAPI();
    renderScreen(queryClient);

    const views = screen.getByRole("group", { name: "Run views" });
    expect(within(views).getByRole("button", { name: "Changes" }).getAttribute("aria-pressed")).toBe("true");
    await waitFor(() => expect(screen.getByTestId("run-changes")).toBeTruthy());
    expect(screen.queryByTestId("run-logs-panel")).toBeNull();

    const rows = screen.getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell")[1].textContent)).toEqual([
      "aws_db_instance.main",
      "aws_s3_bucket.audit_logs",
      "aws_iam_role.monitoring"
    ]);
    expect(within(rows[0]).getByText("storage_encrypted forces replacement")).toBeTruthy();
    expect(within(rows[1]).getByText("no longer in the configuration")).toBeTruthy();

    const filters = screen.getByRole("group", { name: "Show changes by action" });
    await userEvent.click(within(filters).getByRole("button", { name: /^Destroy\s?1$/ }));
    expect(screen.getAllByRole("row").slice(1)).toHaveLength(1);
    expect(screen.getByText("aws_s3_bucket.audit_logs")).toBeTruthy();
  });

  it("opens a row on what changes in it, never showing a sensitive value", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ operation: "apply", status: "waiting_approval", plan_changes: { state: "complete", total: 3 } })
    );
    stubAPI();
    renderScreen(queryClient);

    const address = await screen.findByRole("button", { name: "aws_db_instance.main" });
    expect(address.getAttribute("aria-expanded")).toBe("false");
    await userEvent.click(address);
    expect(address.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText(/storage_encrypted = false -> true/)).toBeTruthy();
    expect(screen.getByText("# forces replacement")).toBeTruthy();
    expect(screen.getByText("# (1 unchanged attribute hidden)")).toBeTruthy();
  });

  it("switches to the logs and keeps that view while the run polls", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ operation: "apply", status: "waiting_approval", plan_changes: { state: "complete", total: 3 } })
    );
    stubAPI();
    renderScreen(queryClient);

    await userEvent.click(screen.getByRole("button", { name: "Logs" }));
    await waitFor(() => expect(screen.getByText("plan-3 log body")).toBeTruthy());
    act(() => {
      queryClient.setQueryData(
        queryKeys.templateRun("tenant_123", "run_1"),
        run({ operation: "apply", status: "approved", plan_changes: { state: "complete", total: 3 } })
      );
    });
    expect(screen.getByRole("button", { name: "Logs" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByTestId("run-logs-panel")).toBeTruthy();
  });

  it("says when the list is cut short, and opens an unlisted plan on its logs", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ operation: "apply", status: "waiting_approval", plan_changes: { state: "truncated", total: 4210 } })
    );
    stubAPI();
    renderScreen(queryClient);
    expect(await screen.findByTestId("run-changes-truncated")).toBeTruthy();
    expect(screen.getByTestId("run-changes-truncated").textContent).toBe("Showing the first 3 of 4210 changes; the plan log has them all.");
    cleanup();

    const unlisted = testQueryClient();
    seedCapabilities(unlisted, allAllowed);
    unlisted.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ operation: "apply", status: "waiting_approval", plan_changes: { state: "unavailable", total: null } })
    );
    renderScreen(unlisted);
    expect(screen.getByRole("button", { name: "Logs" }).getAttribute("aria-pressed")).toBe("true");
    await userEvent.click(screen.getByRole("button", { name: "Changes" }));
    expect(screen.getByTestId("run-changes-note").textContent).toBe("openplan can't list this plan's changes; the plan log has them all.");
  });
});

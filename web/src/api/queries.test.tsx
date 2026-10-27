// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Template, TemplateRun, TemplateRunStatus, TemplateSyncStatus } from "./types";
import {
  POLL_INTERVAL_MS,
  runRefetchInterval,
  runsRefetchInterval,
  useAddTemplateToStackMutation,
  useApproveRunMutation,
  useDiscardRunMutation,
  useCreateStackMutation,
  useDeleteTemplateMutation,
  useRegisterTemplateMutation,
  useStacksQuery,
  useStackQuery,
  useStartTemplateRunMutation,
  useSyncTemplateMutation,
  useTemplateQuery,
  useTemplateRevisionVariablesQuery,
  useTemplateRevisionsQuery,
  useTemplateRunLogQuery,
  useTemplateRunLogsQuery,
  useTemplateRunQuery,
  useTemplateRunsQuery,
  useTemplatesQuery,
  templateRefetchInterval,
  templatesRefetchInterval,
  useUpdateStackTemplateConfigMutation,
  useUpgradeStackTemplateMutation
} from "./queries";
import { queryKeys } from "./queryKeys";
import { asListBody } from "./listTestSupport";

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

// A template whose latest sync reads status.
function syncingTemplate(status: TemplateSyncStatus): Template {
  return {
    id: "tpl_1",
    tenant_id: "tenant_123",
    repo_owner: "acme",
    repo_name: "infra",
    source_ref: "main",
    root_path: ".",
    name: "infra",
    description: "",
    tags: [],
    latest_template_revision_id: "rev_1",
    latest_sync: { status, error_summary: "", resolved_commit_sha: "", requested_at: "2026-10-21T09:00:00Z" },
    created_at: "2026-10-21T09:00:00Z"
  };
}

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(asListBody(body)), { status: 200, headers: { "content-type": "application/json" } });
}

describe("read-only query hooks", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("fetches the tenant's stacks", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ id: "stack_1" }]));
    const { result } = renderHook(() => useStacksQuery("tenant_123"), { wrapper: wrapper(testQueryClient()) });

    await waitFor(() => expect(result.current.data).toEqual([{ id: "stack_1" }]));
    expect(fetch).toHaveBeenCalledWith("/v1/tenants/tenant_123/stacks", expect.objectContaining({ method: "GET" }));
  });

  it("fetches the tenant's templates", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ id: "tpl_1" }]));
    const { result } = renderHook(() => useTemplatesQuery("tenant_123"), { wrapper: wrapper(testQueryClient()) });

    await waitFor(() => expect(result.current.data).toEqual([{ id: "tpl_1" }]));
    expect(fetch).toHaveBeenCalledWith("/v1/tenants/tenant_123/templates?limit=100", expect.objectContaining({ method: "GET" }));
  });

  it("fetches a template's revisions only when a template id is given", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ id: "rev_1" }]));
    const { result, rerender } = renderHook(({ templateID }: { templateID: string }) => useTemplateRevisionsQuery("tenant_123", templateID), {
      wrapper: wrapper(testQueryClient()),
      initialProps: { templateID: "" }
    });

    expect(result.current.fetchStatus).toBe("idle");
    expect(fetchMock).not.toHaveBeenCalled();

    rerender({ templateID: "tpl_1" });
    await waitFor(() => expect(result.current.data).toEqual([{ id: "rev_1" }]));
    expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/templates/tpl_1/revisions?limit=100", expect.objectContaining({ method: "GET" }));
  });

  it("fetches stack detail only when a stack id is given", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ stack: { id: "stack_1" }, templates: [] }));
    const { result, rerender } = renderHook(({ stackID }: { stackID: string }) => useStackQuery("tenant_123", stackID), {
      wrapper: wrapper(testQueryClient()),
      initialProps: { stackID: "" }
    });

    expect(result.current.fetchStatus).toBe("idle");
    expect(fetchMock).not.toHaveBeenCalled();

    rerender({ stackID: "stack_1" });
    await waitFor(() => expect(result.current.data).toEqual({ stack: { id: "stack_1" }, templates: [] }));
    expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/stacks/stack_1", expect.objectContaining({ method: "GET" }));
  });

  it("fetches template revision variables only when a revision id is given", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ name: "region" }]));
    const { result } = renderHook(() => useTemplateRevisionVariablesQuery("tenant_123", ""), {
      wrapper: wrapper(testQueryClient())
    });

    expect(result.current.fetchStatus).toBe("idle");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("fetches runs for a stack template", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ id: "run_1", status: "waiting_approval" }]));
    const { result } = renderHook(() => useTemplateRunsQuery("tenant_123", "stpl_1"), { wrapper: wrapper(testQueryClient()) });

    await waitFor(() => expect(result.current.data).toEqual([{ id: "run_1", status: "waiting_approval" }]));
    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stack-templates/stpl_1/runs",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("does not fetch runs when the stack template id is empty", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));
    const { result } = renderHook(() => useTemplateRunsQuery("tenant_123", ""), { wrapper: wrapper(testQueryClient()) });

    expect(result.current.fetchStatus).toBe("idle");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("polling-aware query hooks", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("does not fetch a template until it is given an id", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "tpl_1" }));
    const { result } = renderHook(() => useTemplateQuery("tenant_123", ""), { wrapper: wrapper(testQueryClient()) });

    expect(result.current.fetchStatus).toBe("idle");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // One case per status a sync can settle on, stalled included: the hook
  // reads syncing, then the settled status, then never asks again.
  it.each<TemplateSyncStatus>(["synced", "up_to_date", "invalid", "failed", "stalled"])(
    "polls a template while it syncs and stops once it reads %s",
    async (settled) => {
      const fetchMock = vi
        .spyOn(globalThis, "fetch")
        .mockResolvedValueOnce(jsonResponse(syncingTemplate("syncing")))
        .mockResolvedValue(jsonResponse(syncingTemplate(settled)));
      const { result } = renderHook(() => useTemplateQuery("tenant_123", "tpl_1"), { wrapper: wrapper(testQueryClient()) });

      await waitFor(() => expect(result.current.data?.latest_sync?.status).toBe(settled), { timeout: POLL_INTERVAL_MS * 3 });
      expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/templates/tpl_1", expect.objectContaining({ method: "GET" }));
      // Two intervals more without a third request.
      await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS * 2));
      expect(fetchMock).toHaveBeenCalledTimes(2);
    },
    POLL_INTERVAL_MS * 8
  );

  it("polls the templates list while a template syncs and stops once none does", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse([syncingTemplate("syncing")]))
      .mockResolvedValue(jsonResponse([syncingTemplate("failed")]));
    const { result } = renderHook(() => useTemplatesQuery("tenant_123"), { wrapper: wrapper(testQueryClient()) });

    await waitFor(() => expect(result.current.data?.[0].latest_sync?.status).toBe("failed"), { timeout: POLL_INTERVAL_MS * 3 });
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS * 2));
    expect(fetchMock).toHaveBeenCalledTimes(2);
  }, POLL_INTERVAL_MS * 8);

  it("fetches a template run's status regardless of poll option", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() =>
      Promise.resolve(jsonResponse({ id: "run_1", status: "running" }))
    );
    const { result: polled } = renderHook(() => useTemplateRunQuery("tenant_123", "run_1", { poll: true }), {
      wrapper: wrapper(testQueryClient())
    });
    await waitFor(() => expect(polled.current.data).toEqual({ id: "run_1", status: "running" }));

    const { result: unpolled } = renderHook(() => useTemplateRunQuery("tenant_123", "run_1", { poll: false }), {
      wrapper: wrapper(testQueryClient())
    });
    await waitFor(() => expect(unpolled.current.data).toEqual({ id: "run_1", status: "running" }));
  });

  describe("refetch interval selectors", () => {
    it("polls a template only while its latest sync is syncing", () => {
      expect(templateRefetchInterval(syncingTemplate("syncing"))).toBe(1500);
      for (const settled of ["synced", "up_to_date", "invalid", "failed", "stalled"] as const) {
        expect(templateRefetchInterval(syncingTemplate(settled))).toBe(false);
      }
      // Nothing to follow: not read yet, or never synced.
      expect(templateRefetchInterval(undefined)).toBe(false);
      expect(templateRefetchInterval({ ...syncingTemplate("syncing"), latest_sync: null })).toBe(false);
    });

    // Any one template still syncing keeps the list polled, so a reload in
    // the middle of a sync keeps following it.
    it("polls the templates list while any template syncs", () => {
      expect(templatesRefetchInterval([syncingTemplate("synced"), syncingTemplate("syncing")])).toBe(1500);
      expect(templatesRefetchInterval([syncingTemplate("synced"), syncingTemplate("stalled")])).toBe(false);
      expect(templatesRefetchInterval([{ ...syncingTemplate("syncing"), latest_sync: null }])).toBe(false);
      expect(templatesRefetchInterval([])).toBe(false);
      expect(templatesRefetchInterval(undefined)).toBe(false);
    });

    it("only polls a run when told to, and stops on terminal status", () => {
      expect(runRefetchInterval("running", true)).toBe(1500);
      expect(runRefetchInterval("completed", true)).toBe(false);
      expect(runRefetchInterval("running", false)).toBe(false);
    });

    // Polling a list that never loaded would put it back to pending on every
    // poll, so its failure would flicker in and out instead of staying put.
    it("polls a runs list unless its first load failed", () => {
      expect(runsRefetchInterval({ data: undefined, fetchFailureCount: 0 })).toBe(1500);
      expect(runsRefetchInterval({ data: undefined, fetchFailureCount: 1 })).toBe(false);
      expect(runsRefetchInterval({ data: [], fetchFailureCount: 0 })).toBe(30_000);
      expect(runsRefetchInterval({ data: [], fetchFailureCount: 2 })).toBe(30_000);
    });

    // Settled runs cannot change, so a list of only those need not be asked
    // for every 1.5s; one run still moving brings the fast poll back.
    it("polls a runs list fast only while a run in it is moving", () => {
      const run = (status: TemplateRunStatus) => ({ status }) as TemplateRun;
      expect(runsRefetchInterval({ data: [run("completed"), run("failed"), run("canceled")], fetchFailureCount: 0 })).toBe(30_000);
      expect(runsRefetchInterval({ data: [run("completed"), run("running")], fetchFailureCount: 0 })).toBe(1500);
      expect(runsRefetchInterval({ data: [run("queued")], fetchFailureCount: 0 })).toBe(1500);
      expect(runsRefetchInterval({ data: [run("approved")], fetchFailureCount: 0 })).toBe(1500);
      expect(runsRefetchInterval({ data: [run("waiting_approval")], fetchFailureCount: 0 })).toBe(30_000);
    });
  });

  it("keys run logs by status so a status change produces a refetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ phase: "plan" }]));
    const queryClient = testQueryClient();
    const { result, rerender } = renderHook(
      ({ statusTag }: { statusTag: string }) => useTemplateRunLogsQuery("tenant_123", "run_1", statusTag),
      { wrapper: wrapper(queryClient), initialProps: { statusTag: "running" } }
    );
    await waitFor(() => expect(result.current.data).toEqual([{ phase: "plan" }]));
    expect(fetchMock).toHaveBeenCalledTimes(1);

    rerender({ statusTag: "completed" });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(fetchMock).toHaveBeenLastCalledWith(
      "/v1/tenants/tenant_123/template-runs/run_1/logs",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("keeps showing the previous status's logs while the new status's fetch is in flight", async () => {
    let resolveSecondFetch: ((response: Response) => void) | undefined;
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementationOnce(() =>
      Promise.resolve(jsonResponse([{ phase: "plan" }]))
    );
    const queryClient = testQueryClient();
    const { result, rerender } = renderHook(
      ({ statusTag }: { statusTag: string }) => useTemplateRunLogsQuery("tenant_123", "run_1", statusTag),
      { wrapper: wrapper(queryClient), initialProps: { statusTag: "running" } }
    );
    await waitFor(() => expect(result.current.data).toEqual([{ phase: "plan" }]));

    fetchMock.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveSecondFetch = resolve;
        })
    );
    rerender({ statusTag: "completed" });

    // The new status key's fetch is now in flight; without keepPreviousData this would
    // momentarily be undefined, causing the logs panel to flash empty.
    await waitFor(() => expect(result.current.isFetching).toBe(true));
    expect(result.current.data).toEqual([{ phase: "plan" }]);
    expect(result.current.isPlaceholderData).toBe(true);

    resolveSecondFetch?.(jsonResponse([{ phase: "apply" }]));
    await waitFor(() => expect(result.current.data).toEqual([{ phase: "apply" }]));
    expect(result.current.isPlaceholderData).toBe(false);
  });

  it("does not carry one run's logs over to another run while its fetch is in flight", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementationOnce(() =>
      Promise.resolve(jsonResponse([{ phase: "plan" }]))
    );
    const queryClient = testQueryClient();
    const { result, rerender } = renderHook(
      ({ runID }: { runID: string }) => useTemplateRunLogsQuery("tenant_123", runID, "completed"),
      { wrapper: wrapper(queryClient), initialProps: { runID: "run_1" } }
    );
    await waitFor(() => expect(result.current.data).toEqual([{ phase: "plan" }]));

    fetchMock.mockImplementationOnce(() => new Promise(() => {}));
    rerender({ runID: "run_2" });

    await waitFor(() => expect(result.current.isFetching).toBe(true));
    expect(result.current.data).toBeUndefined();
  });

  // A command's log is uploaded once (its activity never retries), so one
  // name is one body, whenever the list learns its uploaded_at.
  it("fetches a single log's body as text, once per log", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() =>
      Promise.resolve(new Response("plan output\n", { status: 200, headers: { "content-type": "text/plain" } }))
    );
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useTemplateRunLogQuery("tenant_123", "run_1", "plan"), {
      wrapper: wrapper(queryClient)
    });

    await waitFor(() => expect(result.current.data).toBe("plan output\n"));
    // Opening the same log again reads the cache, although the client's
    // default staleTime would refetch on mount.
    const reopened = renderHook(() => useTemplateRunLogQuery("tenant_123", "run_1", "plan"), {
      wrapper: wrapper(queryClient)
    });
    expect(reopened.result.current.data).toBe("plan output\n");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("mutation hooks", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("registers a template and seeds its query with the sync it queued", async () => {
    const registered = syncingTemplate("syncing");
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(registered));
    const queryClient = testQueryClient();
    const { result } = renderHook(() => useRegisterTemplateMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ repo_owner: "acme", repo_name: "infra", source_ref: "main", root_path: "." });
    });

    expect(queryClient.getQueryData(queryKeys.template("tenant_123", "tpl_1"))).toEqual(registered);
  });

  it("syncs a template and seeds its query with the sync it queued", async () => {
    const synced = syncingTemplate("syncing");
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(synced));
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.template("tenant_123", "tpl_1"), syncingTemplate("failed"));
    const { result } = renderHook(() => useSyncTemplateMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync("tpl_1");
    });

    expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/templates/tpl_1/syncs", expect.objectContaining({ method: "POST" }));
    expect(queryClient.getQueryData(queryKeys.template("tenant_123", "tpl_1"))).toEqual(synced);
  });

  it("deletes a template, drops its reads and refetches the list", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.template("tenant_123", "tpl_1"), syncingTemplate("synced"));
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123", "tpl_1"), []);
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useDeleteTemplateMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync("tpl_1");
    });

    expect(fetch).toHaveBeenCalledWith("/v1/tenants/tenant_123/templates/tpl_1", expect.objectContaining({ method: "DELETE" }));
    expect(queryClient.getQueryData(queryKeys.template("tenant_123", "tpl_1"))).toBeUndefined();
    expect(queryClient.getQueryData(queryKeys.templateRevisions("tenant_123", "tpl_1"))).toBeUndefined();
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.templates("tenant_123"), exact: true });
  });

  it("creates a stack and invalidates the stacks list", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "stack_1" }));
    const queryClient = testQueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useCreateStackMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ name: "Acme Prod", slug: "", tags: {}, default_credential_ids: [] });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.stacks("tenant_123") });
  });

  it("installs a template on a stack and invalidates that stack's detail and the stacks list", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "stack_template_1" }));
    const queryClient = testQueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useAddTemplateToStackMutation("tenant_123", "stack_1"), {
      wrapper: wrapper(queryClient)
    });

    await act(async () => {
      await result.current.mutateAsync({ template_revision_id: "rev_1", config: {} });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.stack("tenant_123", "stack_1") });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.stacks("tenant_123") });
  });

  it("saves stack template config and invalidates that stack's detail", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "stack_template_1" }));
    const queryClient = testQueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useUpdateStackTemplateConfigMutation("tenant_123", "stack_1"), {
      wrapper: wrapper(queryClient)
    });

    await act(async () => {
      await result.current.mutateAsync({ stackTemplateID: "stack_template_1", body: { config: { region: "us-west-2" } } });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.stack("tenant_123", "stack_1") });
  });

  it("upgrades a stack template and invalidates that stack's detail", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "stack_template_1" }));
    const queryClient = testQueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useUpgradeStackTemplateMutation("tenant_123", "stack_1"), {
      wrapper: wrapper(queryClient)
    });

    await act(async () => {
      await result.current.mutateAsync({ stackTemplateID: "stack_template_1", body: { target_template_revision_id: "rev_2" } });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.stack("tenant_123", "stack_1") });
  });

  it("starts a run and seeds that run's query cache", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "run_1", status: "queued" }));
    const queryClient = testQueryClient();
    const { result } = renderHook(() => useStartTemplateRunMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ stackTemplateID: "stack_template_1", body: { operation: "plan" } });
    });

    expect(queryClient.getQueryData(queryKeys.templateRun("tenant_123", "run_1"))).toEqual({
      id: "run_1",
      status: "queued"
    });
  });

  it("approves a run and seeds that run's query cache with the approved run", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "run_1", stack_template_id: "st_1", status: "approved" }));
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), { id: "run_1", status: "waiting_approval" });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useApproveRunMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync("run_1");
    });

    expect(queryClient.getQueryData(queryKeys.templateRun("tenant_123", "run_1"))).toEqual({
      id: "run_1",
      stack_template_id: "st_1",
      status: "approved"
    });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.attention("tenant_123") });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.templateRuns("tenant_123", "st_1") });
  });

  it("discards a run and invalidates that run's query and its template's runs", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), { id: "run_1", stack_template_id: "st_1", status: "waiting_approval" });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");
    const { result } = renderHook(() => useDiscardRunMutation("tenant_123"), { wrapper: wrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ runID: "run_1", body: { reason: "manual stop" } });
    });

    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.templateRun("tenant_123", "run_1") });
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.templateRuns("tenant_123", "st_1") });
  });
});

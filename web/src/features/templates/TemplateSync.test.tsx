// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { formatDateTime } from "../../shared/formatTimestamp";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateLatestSync, TemplateRevision } from "../../api/types";
import RevisionsTab from "./RevisionsTab";
import TemplatePanel from "./TemplatePanel";
import { authValue, jsonResponse, latestSync, revision, seedTemplates, template, templateReadResponse, TENANT, testQueryClient } from "./testSupport";

const current = revision();
const other = revision({ id: "rev_net", template_id: "tpl_net", name: "network", root_path: "aws/network" });

function seed(): QueryClient {
  const queryClient = testQueryClient();
  seedTemplates(queryClient, [current, other]);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), []);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_net"), []);
  return queryClient;
}

// A fake API. Sync answers with tpl_1 syncing; each read of tpl_1 answers
// the next of polled, the last one from then on; the list and the revisions
// come from listed(). Anything else stays pending.
function serve({
  polled,
  listed = () => [current, other],
  syncAnswer
}: {
  polled: Array<Partial<TemplateLatestSync> | Response>;
  listed?: () => TemplateRevision[];
  syncAnswer?: Response;
}) {
  let reads = 0;
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const path = String(input);
    if (init?.method === "POST") {
      return syncAnswer ?? jsonResponse(template({ latest_sync: latestSync({ status: "syncing" }) }), 202);
    }
    if (path === `/v1/tenants/${TENANT}/templates/tpl_1`) {
      const next = polled[Math.min(reads++, polled.length - 1)];
      return next instanceof Response ? next : jsonResponse(template({ latest_sync: latestSync(next) }));
    }
    if (path.endsWith("/variables")) return jsonResponse([]);
    return templateReadResponse(input, listed()) ?? new Promise<Response>(() => {});
  });
}

function renderPanel(queryClient: QueryClient, canManageTemplates = true) {
  const router = createMemoryRouter(
    [{ path: "/templates/:templateId", element: <TemplatePanel />, children: [{ path: "revisions", element: <RevisionsTab /> }] }],
    { initialEntries: ["/templates/tpl_1/revisions"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canManageTemplates)}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

function syncButton(): HTMLButtonElement {
  return screen.getByTestId("template-sync") as HTMLButtonElement;
}

// Busy, not disabled: the button keeps focus while a sync runs.
function syncing(): boolean {
  expect(syncButton().disabled).toBe(false);
  return syncButton().getAttribute("aria-disabled") === "true";
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplateSync", () => {
  it("syncs the template by its id, with no body to retype", async () => {
    const fetchMock = serve({ polled: [{ status: "syncing" }] });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(true));
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(String(post?.[0])).toBe("/v1/tenants/tenant_123/templates/tpl_1/syncs");
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("template-registrations"))).toBe(false);
  });

  it("marks Sync busy while one runs, keeping focus, so clicks do not queue workflows", async () => {
    const fetchMock = serve({ polled: [{ status: "syncing" }] });
    renderPanel(seed());
    expect(syncing()).toBe(false);

    fireEvent.click(syncButton());
    await waitFor(() => expect(syncing()).toBe(true));
    fireEvent.click(syncButton());
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  });

  it("names the new commit, which becomes the latest", async () => {
    const fresh = revision({ id: "rev_2", resolved_commit_sha: "f17f983444455556666" });
    let synced = false;
    serve({
      polled: [{ status: "synced", resolved_commit_sha: "f17f983444455556666" }],
      listed: () => (synced ? [fresh, current, other] : [current, other])
    });
    renderPanel(seed());
    fireEvent.click(syncButton());
    synced = true;

    // The real flow: Sync answers syncing, then a poll POLL_INTERVAL_MS later
    // reads the outcome.
    await waitFor(() => expect(screen.getByTestId("revision-row-rev_2")).toBeTruthy(), { timeout: 3000 });
    expect(screen.getByTestId("template-sync-result").textContent).toBe("synced commit f17f983");
    expect(screen.getAllByTestId(/^revision-row-/)[0].getAttribute("data-testid")).toBe("revision-row-rev_2");
  });

  it("says the template is already up to date when the ref still names a known commit", async () => {
    serve({ polled: [{ status: "up_to_date" }] });
    renderPanel(seed());
    expect(screen.getByTestId("template-sync-result").textContent).toBe("");

    fireEvent.click(syncButton());
    await waitFor(() => expect(screen.getByTestId("template-sync-result").textContent).toBe("up to date"), { timeout: 3000 });
  });

  // Stalled is shown as a failure: the sync did not finish, and polling it
  // any longer would never end.
  it.each([
    [{ status: "failed", error_summary: "ref not found: main" }, "ref not found: main"],
    [{ status: "invalid", error_summary: "" }, "Sync failed"],
    [{ status: "stalled", error_summary: "" }, "Sync stalled"]
  ] as const)("shows why a sync failed, and offers Sync again (%o)", async (failed, message) => {
    serve({ polled: [failed] });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain(message), { timeout: 3000 });
    expect(syncing()).toBe(false);
  });

  it("shows a refused request rather than leaving the button spinning", async () => {
    serve({ polled: [{ status: "syncing" }], syncAnswer: jsonResponse({ type: "about:blank", title: "Forbidden", detail: "forbidden" }, 403) });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain("forbidden"));
    expect(syncing()).toBe(false);
  });

  it("shows a failing poll rather than leaving the button spinning", async () => {
    serve({ polled: [jsonResponse({ type: "about:blank", title: "Forbidden", detail: "forbidden" }, 403)] });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain("forbidden"), { timeout: 3000 });
    expect(syncing()).toBe(false);
  });

  // The template's latest sync, whoever started it and wherever: the list
  // already carries it, so the outcome shows with no request of its own.
  it.each([
    [{ status: "synced", resolved_commit_sha: "f17f983444455556666" }, "template-sync-result", "synced commit f17f983"],
    [{ status: "up_to_date" }, "template-sync-result", "up to date"],
    [{ status: "failed", error_summary: "ref not found: main" }, "template-sync-error", "ref not found: main"]
  ] as const)("shows the latest sync's outcome when the panel opens (%o)", (latest, testId, text) => {
    const queryClient = seed();
    queryClient.setQueryData(queryKeys.templates(TENANT), [
      template({ latest_sync: latestSync(latest) }),
      template({ id: "tpl_net", name: "network", root_path: "aws/network", latest_template_revision_id: "rev_net" })
    ]);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(queryClient);

    expect(screen.getByTestId(testId).textContent).toContain(text);
    expect(syncing()).toBe(false);
    // An outcome that was already in when the panel opened changed nothing
    // since the list and the revisions were read.
    expect(fetchMock).not.toHaveBeenCalled();
    expect(invalidate).not.toHaveBeenCalled();
  });

  // A reload in the middle of a sync started anywhere picks it up again.
  it("follows a sync already running when the panel opens, to its outcome", async () => {
    const fresh = revision({ id: "rev_2", resolved_commit_sha: "f17f983444455556666" });
    const queryClient = seed();
    queryClient.setQueryData(queryKeys.templates(TENANT), [
      template({ latest_sync: latestSync({ status: "syncing" }) }),
      template({ id: "tpl_net", name: "network", root_path: "aws/network", latest_template_revision_id: "rev_net" })
    ]);
    serve({
      polled: [{ status: "syncing" }, { status: "synced", resolved_commit_sha: "f17f983444455556666" }],
      listed: () => [fresh, current, other]
    });
    renderPanel(queryClient);

    expect(syncing()).toBe(true);
    expect(screen.getByTestId("template-sync-result").textContent).toMatch(/^syncing · started /);
    await waitFor(() => expect(screen.getByTestId("template-sync-result").textContent).toBe("synced commit f17f983"), { timeout: 4000 });
    await waitFor(() => expect(screen.getAllByTestId(/^revision-row-/)[0].getAttribute("data-testid")).toBe("revision-row-rev_2"));
  });

  // An outcome read on a later day is not news: each says when it ended,
  // and one still running or stalled says when it started.
  it.each([
    [{ status: "synced", resolved_commit_sha: "f17f983444455556666", completed_at: "2026-10-05T09:01:00Z" }, "template-sync-result", `synced commit f17f983 · ${formatDateTime("2026-10-05T09:01:00Z")}`],
    [{ status: "up_to_date", completed_at: "2026-10-05T09:01:00Z" }, "template-sync-result", `up to date · ${formatDateTime("2026-10-05T09:01:00Z")}`],
    [{ status: "syncing" }, "template-sync-result", `syncing · started ${formatDateTime("2026-10-05T09:00:00Z")}`],
    [{ status: "failed", error_summary: "ref not found: main", completed_at: "2026-10-05T09:01:00Z" }, "template-sync-error", `Sync failed · ${formatDateTime("2026-10-05T09:01:00Z")}`],
    [{ status: "stalled" }, "template-sync-error", `Sync stalled · started ${formatDateTime("2026-10-05T09:00:00Z")}`]
  ] as const)("says when the latest sync happened (%o)", (latest, testId, text) => {
    const queryClient = seed();
    queryClient.setQueryData(queryKeys.templates(TENANT), [template({ latest_sync: latestSync(latest) })]);
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(queryClient);

    expect(screen.getByTestId(testId).textContent).toContain(text);
    const when = screen.getByTestId("template-sync-when").querySelector("time");
    expect(when?.getAttribute("dateTime")).toBe(latest.status === "syncing" || latest.status === "stalled" ? "2026-10-05T09:00:00Z" : "2026-10-05T09:01:00Z");
  });

  // A stall has no words of its own and nothing broke that a person must fix
  // first: it says so once, and the way on is to sync again, offered only to
  // people who can.
  it.each([
    [true, "It didn't finish. Sync again."],
    [false, "It didn't finish."]
  ] as const)("says a stalled sync didn't finish, without repeating its headline (can publish: %s)", (canPublish, nextStep) => {
    const queryClient = seed();
    queryClient.setQueryData(queryKeys.templates(TENANT), [template({ latest_sync: latestSync({ status: "stalled", error_summary: "" }) })]);
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(queryClient, canPublish);

    const error = screen.getByTestId("template-sync-error");
    expect(error.textContent).toContain("Sync stalled");
    expect(error.textContent?.endsWith(nextStep)).toBe(true);
    expect(error.textContent).not.toContain("Sync failed");
    expect(error.querySelector("pre")).toBeNull();
  });

  // A failure that came without words says it once, in its headline.
  it("leaves out the details of a failure that came without any", () => {
    const queryClient = seed();
    queryClient.setQueryData(queryKeys.templates(TENANT), [template({ latest_sync: latestSync({ status: "invalid", error_summary: "" }) })]);
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(queryClient);

    const error = screen.getByTestId("template-sync-error");
    expect(error.textContent).toContain("Sync failed");
    expect(error.querySelector("pre")).toBeNull();
  });

  // The template's other actions sit on the name's line just before Sync,
  // as one group.
  it("puts the actions it is given just before Sync", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed());

    const remove = screen.getByTestId("template-delete");
    expect(remove.parentElement).toBe(syncButton().parentElement);
    expect(remove.nextElementSibling).toBe(syncButton());
  });

  it("is not offered to people who cannot publish templates", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed(), false);
    expect(screen.queryByTestId("template-sync")).toBeNull();
  });

  // Review focus 2: a sync belongs to its template. Moving to another must
  // not carry its spinner, result or error along.
  it("starts fresh on another template while a sync runs", async () => {
    serve({ polled: [{ status: "syncing" }] });
    const router = renderPanel(seed());
    fireEvent.click(syncButton());
    await waitFor(() => expect(syncing()).toBe(true));

    await act(async () => {
      await router.navigate("/templates/tpl_net/revisions");
    });
    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
    expect(syncing()).toBe(false);
    expect(screen.getByTestId("template-sync-result").textContent).toBe("");
  });

  // A sync the person walked away from can finish while they read another
  // template. The panel reads the revisions again on coming back, so the new
  // commit shows even though this Sync is long gone.
  it("shows a commit a sync registered while the person was on another template", async () => {
    const fresh = revision({ id: "rev_2", resolved_commit_sha: "f17f983444455556666" });
    let listed = [current, other];
    serve({ polled: [{ status: "syncing" }], listed: () => listed });
    const router = renderPanel(seed());
    fireEvent.click(syncButton());
    await waitFor(() => expect(syncing()).toBe(true));

    await act(async () => {
      await router.navigate("/templates/tpl_net/revisions");
    });
    // The sync completes server-side while the person is on network.
    listed = [fresh, current, other];
    await act(async () => {
      await router.navigate("/templates/tpl_1/revisions");
    });

    await waitFor(() => expect(screen.getAllByTestId(/^revision-row-/)[0].getAttribute("data-testid")).toBe("revision-row-rev_2"));
    expect(within(screen.getByTestId("template-source")).getByRole("link", { name: /^Latest commit: ?f17f983,/ })).toBeTruthy();
  });
});

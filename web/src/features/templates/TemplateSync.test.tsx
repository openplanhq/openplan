// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import RevisionsTab from "./RevisionsTab";
import TemplatePanel from "./TemplatePanel";
import { authValue, jsonResponse, registration, revision, TENANT, testQueryClient } from "./testSupport";

const current = revision();
const other = revision({ id: "rev_net", source_template_id: "tpl_net", name: "network", root_path: "aws/network" });

function seed(): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [current, other]);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), []);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_net"), []);
  return queryClient;
}

function renderPanel(queryClient: QueryClient, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [{ path: "/templates/:sourceTemplateId", element: <TemplatePanel />, children: [{ path: "revisions", element: <RevisionsTab /> }] }],
    { initialEntries: ["/templates/tpl_1/revisions"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

function syncButton(): HTMLButtonElement {
  return screen.getByTestId("template-sync") as HTMLButtonElement;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplateSync", () => {
  it("re-registers exactly the template's identity", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse(registration());
      return jsonResponse([current, other]);
    });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(true));
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(String(post?.[0])).toContain("/v1/tenants/tenant_123/template-revisions");
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ repo_owner: "acme", repo_name: "infra-modules", source_ref: "main", root_path: "aws/eks" });
  });

  it("disables Sync while one runs, so clicks do not queue workflows", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse(registration({ status: "running" }));
      return jsonResponse([current, other]);
    });
    renderPanel(seed());
    expect(syncButton().disabled).toBe(false);

    fireEvent.click(syncButton());
    await waitFor(() => expect(syncButton().disabled).toBe(true));
    fireEvent.click(syncButton());
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  });

  it("names the new commit, which becomes the latest", async () => {
    const fresh = revision({ id: "rev_2", resolved_commit_sha: "f17f983444455556666" });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) {
        return jsonResponse(registration({ template_revision_id: "rev_2", resolved_commit_sha: "f17f983444455556666" }));
      }
      return jsonResponse([fresh, current, other]);
    });
    renderPanel(seed());
    fireEvent.click(syncButton());

    // The real flow: a pending POST, then a poll POLL_INTERVAL_MS later.
    await waitFor(() => expect(screen.getByTestId("revision-row-rev_2")).toBeTruthy(), { timeout: 3000 });
    expect(screen.getByTestId("template-sync-result").textContent).toBe("Registered commit f17f983.");
    expect(screen.getAllByTestId(/^revision-row-/)[0].getAttribute("data-testid")).toBe("revision-row-rev_2");
  });

  it("says the template is already up to date when the ref still names a known commit", async () => {
    const unchanged = registration({ template_revision_id: "rev_1" });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      init?.method === "POST" || String(input).includes("/template-registrations/") ? jsonResponse(unchanged) : jsonResponse([current, other])
    );
    renderPanel(seed());
    expect(screen.getByTestId("template-sync-result").textContent).toBe("");

    fireEvent.click(syncButton());
    await waitFor(() => expect(screen.getByTestId("template-sync-result").textContent).toBe("Already up to date."));
  });

  it.each([
    [registration({ status: "failed", template_revision_id: "", error_summary: "ref not found: main" }), "ref not found: main"],
    [registration({ status: "invalid", template_revision_id: "", error_summary: "" }), "Sync failed"]
  ])("shows why a sync failed, and offers Sync again", async (failed, message) => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      init?.method === "POST" || String(input).includes("/template-registrations/") ? jsonResponse(failed) : jsonResponse([current, other])
    );
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain(message));
    expect(syncButton().disabled).toBe(false);
  });

  it("shows a refused request rather than leaving the button spinning", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse({ error: "forbidden", message: "forbidden" }, 403) : jsonResponse([current, other])
    );
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain("forbidden"));
    expect(syncButton().disabled).toBe(false);
  });

  it("shows a failing poll rather than leaving the button spinning", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending", template_revision_id: "" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse({ error: "forbidden", message: "forbidden" }, 403);
      return jsonResponse([current, other]);
    });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain("forbidden"), { timeout: 3000 });
    expect(syncButton().disabled).toBe(false);
  });

  it("is not offered to people who cannot publish templates", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed(), false);
    expect(screen.queryByTestId("template-sync")).toBeNull();
  });

  // Review focus 2: a sync belongs to its template. Moving to another must
  // not carry its spinner, result or error along.
  it("starts fresh on another template while a sync runs", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse(registration({ status: "running" }));
      return jsonResponse([current, other]);
    });
    const router = renderPanel(seed());
    fireEvent.click(syncButton());
    await waitFor(() => expect(syncButton().disabled).toBe(true));

    await act(async () => {
      await router.navigate("/templates/tpl_net/revisions");
    });
    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
    expect(syncButton().disabled).toBe(false);
    expect(screen.getByTestId("template-sync-result").textContent).toBe("");
  });
});

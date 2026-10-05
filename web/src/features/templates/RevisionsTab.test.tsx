// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import RevisionsTab from "./RevisionsTab";
import TemplatePanel from "./TemplatePanel";
import { authValue, revision, TENANT, testQueryClient } from "./testSupport";

const newest = revision({ id: "rev_3", resolved_commit_sha: "e3a7c4d000", created_at: "2026-09-30T10:00:00Z" });
const middle = revision({ id: "rev_2", resolved_commit_sha: "b51f08a000", created_at: "2026-09-14T10:00:00Z", status: "validating" });
const oldest = revision({ id: "rev_1", resolved_commit_sha: "0c9d2e6000", created_at: "2026-08-28T10:00:00Z" });

function seed(): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [newest, middle, oldest]);
  return queryClient;
}

function renderTab(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [{ path: "/templates/:sourceTemplateId", element: <TemplatePanel />, children: [{ path: "revisions", element: <RevisionsTab /> }] }],
    { initialEntries: ["/templates/tpl_1/revisions"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("RevisionsTab", () => {
  it("lists every commit registered from the ref, newest first, the newest marked latest", () => {
    renderTab(seed());

    expect(screen.getByTestId("template-revisions").textContent).toContain("Every commit registered from main, newest first.");
    const rows = screen.getAllByTestId(/^revision-row-/);
    expect(rows.map((row) => row.getAttribute("data-testid"))).toEqual(["revision-row-rev_3", "revision-row-rev_2", "revision-row-rev_1"]);
    expect(screen.getAllByTestId("revision-latest")).toHaveLength(1);
    expect(within(rows[0]).getByTestId("revision-latest").textContent).toBe("latest");
    expect(rows[2].textContent).toMatch(/28 Aug(ust)? 2026/);
    expect(screen.getByRole("table").querySelector("colgroup")).not.toBeNull();
  });

  it("links each commit on GitHub", () => {
    renderTab(seed());
    const link = within(screen.getByTestId("revision-row-rev_2")).getByRole("link", { name: /^b51f08a,/ });
    expect(link.getAttribute("href")).toBe("https://github.com/acme/infra-modules/commit/b51f08a000");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("says nothing about an active revision, and names any other state", () => {
    renderTab(seed());
    expect(within(screen.getByTestId("revision-row-rev_2")).getByText("validating")).toBeTruthy();
    expect(within(screen.getByTestId("revision-row-rev_1")).queryByText(/active|validating|failed/)).toBeNull();
  });
});

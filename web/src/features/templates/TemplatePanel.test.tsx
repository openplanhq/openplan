// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider, useLocation } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import { problemTypes } from "../../api/types";
import type { TemplateLatestSync, TemplateRevision } from "../../api/types";
import TemplatePanel from "./TemplatePanel";
import { authValue, jsonResponse, latestSync, revision, seedTemplates, template, templateReadResponse, TENANT, testQueryClient, variable } from "./testSupport";

const latest = revision({ id: "rev_2", resolved_commit_sha: "9e7d3b2aaaabbbbccccddddeeeeffff000011112", created_at: "2026-10-05T09:30:00Z" });
const older = revision({ id: "rev_1" });
const rds = revision({ id: "rev_rds", template_id: "tpl_rds", name: "rds", root_path: "aws/rds", description: "", tags: [] });
const cdn = revision({ id: "rev_cdn", template_id: "tpl_cdn", repo_name: "edge", name: "cloudfront-site", root_path: ".", source_ref: "v2.0.1" });

function seed(revisions: TemplateRevision[] = [latest, older, rds, cdn]): QueryClient {
  const queryClient = testQueryClient();
  seedTemplates(queryClient, revisions);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_2"), [
    variable({ template_revision_id: "rev_2" }),
    variable({ template_revision_id: "rev_2", name: "node_desired_size", type_expression: "number", required: false, has_default: true }),
    variable({ template_revision_id: "rev_2", name: "subnet_ids", type_expression: "list(string)" })
  ]);
  return queryClient;
}

function renderPanel(queryClient: QueryClient, path: string, canManageTemplates = true) {
  const router = createMemoryRouter(
    [
      // Where a delete lands, showing what it carried.
      { path: "/templates", element: <LandedOn /> },
      {
        path: "/templates/:templateId",
        element: <TemplatePanel />,
        children: [
          { path: "variables", element: <p data-testid="variables-content">variables</p> },
          { path: "revisions", element: <p data-testid="revisions-content">revisions</p> }
        ]
      }
    ],
    { initialEntries: [path] }
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

function LandedOn() {
  return <p data-testid="templates-index">{JSON.stringify(useLocation().state)}</p>;
}

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplatePanel header", () => {
  it("names the template and shows its latest revision's description and tags", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");

    expect(screen.getByRole("heading", { level: 2, name: "eks" })).toBeTruthy();
    expect(screen.getByTestId("template-description").textContent).toBe("An EKS cluster with one managed node group.");
    expect(screen.getByText("aws")).toBeTruthy();
    expect(screen.getByText("kubernetes")).toBeTruthy();
  });

  it("tells publishers, and only publishers, how to add a description", () => {
    renderPanel(seed(), "/templates/tpl_rds/variables");
    expect(screen.getByTestId("template-no-description").textContent).toContain("template.yaml");
    expect(screen.queryByTestId("template-description")).toBeNull();
    cleanup();

    renderPanel(seed(), "/templates/tpl_rds/variables", false);
    expect(screen.queryByTestId("template-no-description")).toBeNull();
  });

  it("links the repository, root path, ref and latest commit on GitHub, each named for a screen reader, with no date: the revision's is not the commit's", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");
    const details = within(screen.getByRole("list", { name: "Source on GitHub" }));

    expect(details.getByRole("link", { name: /^Repository: ?acme\/infra-modules,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules");
    expect(details.getByRole("link", { name: /^Root path: ?aws\/eks,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules/tree/main/aws/eks");
    expect(details.getByRole("link", { name: /^Ref: ?main,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules/tree/main");
    const commit = details.getByRole("link", { name: /^Latest commit: ?9e7d3b2,/ });
    expect(commit.getAttribute("href")).toBe("https://github.com/acme/infra-modules/commit/9e7d3b2aaaabbbbccccddddeeeeffff000011112");
    expect(screen.getByTestId("template-source").textContent).not.toMatch(/Oct/);
    expect(screen.getByTestId("template-source").querySelector("time")).toBeNull();
    for (const link of details.getAllByRole("link")) {
      expect(link.getAttribute("target")).toBe("_blank");
    }
  });

  it("calls a module at the root the repository root", () => {
    renderPanel(seed(), "/templates/tpl_cdn/variables");
    const root = within(screen.getByTestId("template-source")).getByRole("link", { name: /^Root path: ?repository root,/ });
    expect(root.getAttribute("href")).toBe("https://github.com/acme/edge/tree/v2.0.1");
  });

  it("lays the four facts out as one row of equal columns under the name, none cut short", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");
    const source = screen.getByTestId("template-source");
    expect(source.className).toContain("grid-cols-4");
    expect(source.querySelectorAll("li")).toHaveLength(4);
    expect(source.querySelector(".truncate")).toBeNull();
    expect(screen.getByRole("heading", { level: 2 }).compareDocumentPosition(source) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("says so when the URL names no registered template", () => {
    renderPanel(seed(), "/templates/gone/variables");
    expect(screen.getByTestId("template-missing").textContent).toBe("That template is not registered.");
  });

  it("says it is loading while the template's revisions load", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templates(TENANT), [template()]);
    renderPanel(queryClient, "/templates/tpl_1/variables");
    expect(screen.getByTestId("template-loading").textContent).toContain("Loading template…");
  });

  it("offers a retry when the template's revisions fail to load", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templates(TENANT), [template()]);
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500));
    renderPanel(queryClient, "/templates/tpl_1/variables");

    await waitFor(() => expect(screen.getByTestId("template-error").textContent).toContain("Something went wrong while loading this template."));
    expect(within(screen.getByTestId("template-error")).getByRole("button", { name: "Retry" })).toBeTruthy();
  });
});

describe("TemplatePanel tabs", () => {
  it("marks the current tab, each named by its word alone", () => {
    renderPanel(seed(), "/templates/tpl_1/revisions");
    const nav = within(screen.getByRole("navigation", { name: "Template sections" }));

    const variables = nav.getByRole("link", { name: "Variables" });
    expect(variables.getAttribute("href")).toBe("/templates/tpl_1/variables");
    expect(variables.getAttribute("aria-current")).toBeNull();

    const revisions = nav.getByRole("link", { name: "Revisions" });
    expect(revisions.getAttribute("href")).toBe("/templates/tpl_1/revisions");
    expect(revisions.getAttribute("aria-current")).toBe("page");
    expect(screen.getByTestId("revisions-content")).toBeTruthy();
  });
});

// A template exists before any sync has recorded a revision for it: its first
// sync is still running, or failed. The panel is its header alone, and the
// template's latest_sync says why there is nothing to install.
describe("TemplatePanel before a first revision", () => {
  function unsynced(latestSyncValue: TemplateLatestSync | null): QueryClient {
    const queryClient = seed();
    seedTemplates(queryClient, [latest, older], [
      template({ id: "tpl_new", name: "eks-fresh", description: "", tags: [], latest_template_revision_id: "", latest_sync: latestSyncValue })
    ]);
    return queryClient;
  }

  it("shows its name and source, with no commit, description, tabs or tab content", () => {
    renderPanel(unsynced(latestSync({ status: "failed", error_summary: "ref not found" })), "/templates/tpl_new/variables");

    expect(screen.getByRole("heading", { level: 2, name: "eks-fresh" })).toBeTruthy();
    const source = screen.getByTestId("template-source");
    expect(source.querySelectorAll("li")).toHaveLength(3);
    expect(within(source).queryByRole("link", { name: /^Latest commit/ })).toBeNull();
    expect(screen.queryByTestId("template-no-description")).toBeNull();
    expect(screen.queryByRole("navigation", { name: "Template sections" })).toBeNull();
    expect(screen.queryByTestId("variables-content")).toBeNull();
    expect(screen.queryByTestId("template-missing")).toBeNull();
  });

  it("says why when its first sync failed, in the server's words", () => {
    renderPanel(unsynced(latestSync({ status: "invalid", error_summary: "no .tf files in aws/ekss" })), "/templates/tpl_new/variables");

    const error = screen.getByTestId("template-sync-error");
    expect(within(error).getByText("Sync failed")).toBeTruthy();
    expect(error.textContent).toContain("No revision yet, so stacks can't add this template.");
    expect(error.textContent).toContain("no .tf files in aws/ekss");
  });

  it("names a first sync that stalled, without repeating its headline, and offers to sync again", () => {
    renderPanel(unsynced(latestSync({ status: "stalled", error_summary: "" })), "/templates/tpl_new/variables");

    const error = screen.getByTestId("template-sync-error");
    expect(error.textContent).toContain("Sync stalled");
    expect(error.textContent).toContain("No revision yet, so stacks can't add this template. Sync again.");
    expect(error.textContent).not.toContain("Sync failed");
    expect(error.querySelector("pre")).toBeNull();
  });

  it("follows a first sync still running until it lands a revision, then shows the template", async () => {
    const fresh = revision({ id: "rev_new", template_id: "tpl_new", name: "eks-fresh", resolved_commit_sha: "abc1234ffff" });
    let done = false;
    vi.mocked(globalThis.fetch).mockImplementation(async (input) => {
      const path = String(input).split("?")[0];
      if (path === `/v1/tenants/${TENANT}/templates/tpl_new`) {
        // The first read still finds it syncing; the next, synced.
        const answer = done
          ? template({ id: "tpl_new", name: "eks-fresh", latest_template_revision_id: "rev_new", latest_sync: latestSync({ status: "synced", resolved_commit_sha: "abc1234ffff" }) })
          : template({ id: "tpl_new", name: "eks-fresh", latest_template_revision_id: "", latest_sync: latestSync({ status: "syncing" }) });
        done = true;
        return jsonResponse(answer);
      }
      return templateReadResponse(input, [fresh, latest, older]) ?? new Promise<Response>(() => {});
    });
    renderPanel(unsynced(latestSync({ status: "syncing" })), "/templates/tpl_new/variables");

    expect(screen.getByTestId("template-sync-result").textContent).toMatch(/^syncing · started /);
    await waitFor(() => expect(screen.getByRole("navigation", { name: "Template sections" })).toBeTruthy(), { timeout: 4000 });
    expect(screen.getByTestId("template-sync-result").textContent).toBe("synced commit abc1234");
  });

  it("says no revision when no sync has been asked for", () => {
    renderPanel(unsynced(null), "/templates/tpl_new/variables");
    expect(screen.getByTestId("template-sync-result").textContent).toBe("no revision");
    expect(screen.queryByTestId("template-sync-error")).toBeNull();
  });

  it("tells people who cannot publish why, without offering Sync", () => {
    renderPanel(unsynced(latestSync({ status: "failed", error_summary: "ref not found" })), "/templates/tpl_new/variables", false);
    expect(screen.queryByTestId("template-sync")).toBeNull();
    expect(screen.getByTestId("template-sync-error").textContent).toContain("ref not found");
  });
});

describe("TemplatePanel delete", () => {
  function problem(status: number, type: string, detail: string): Response {
    return new Response(JSON.stringify({ type, title: "Refused", status, detail }), {
      status,
      headers: { "content-type": "application/problem+json" }
    });
  }

  // Answers the DELETE with this response and records it; reads stay
  // pending, as everything is seeded.
  function answerDelete(response: () => Response) {
    const calls: string[] = [];
    vi.mocked(globalThis.fetch).mockImplementation(async (input, init) => {
      if (init?.method === "DELETE") {
        calls.push(String(input));
        return response();
      }
      return templateReadResponse(input, [latest, older, rds, cdn]) ?? new Promise<Response>(() => {});
    });
    return calls;
  }

  it("is not offered to people who cannot publish templates", () => {
    renderPanel(seed(), "/templates/tpl_1/variables", false);
    expect(screen.queryByTestId("template-delete")).toBeNull();
  });

  it("asks first, in place, and sends nothing until confirmed", () => {
    const calls = answerDelete(() => new Response(null, { status: 204 }));
    renderPanel(seed(), "/templates/tpl_1/variables");

    const trigger = screen.getByTestId("template-delete");
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(trigger);

    const confirm = screen.getByTestId("template-delete-confirm");
    expect(confirm.textContent).toContain("Delete eks?");
    expect(confirm.textContent).toContain("Stacks can no longer add it, and its revisions go with it.");
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    // A stray Enter cancels rather than deletes.
    expect(document.activeElement).toBe(within(confirm).getByRole("button", { name: "Cancel" }));
    expect(calls).toHaveLength(0);
  });

  it("puts focus back on Delete when cancelled, by Cancel or Escape", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");
    const trigger = screen.getByTestId("template-delete");

    fireEvent.click(trigger);
    fireEvent.click(within(screen.getByTestId("template-delete-confirm")).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByTestId("template-delete-confirm")).toBeNull();
    expect(document.activeElement).toBe(trigger);

    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByTestId("template-delete-confirm"), { key: "Escape" });
    expect(screen.queryByTestId("template-delete-confirm")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("deletes once confirmed and goes to /templates, saying which template went", async () => {
    const calls = answerDelete(() => new Response(null, { status: 204 }));
    const router = renderPanel(seed(), "/templates/tpl_1/variables");

    fireEvent.click(screen.getByTestId("template-delete"));
    fireEvent.click(within(screen.getByTestId("template-delete-confirm")).getByRole("button", { name: "Delete template" }));

    await waitFor(() => expect(screen.getByTestId("templates-index").textContent).toBe(JSON.stringify({ deletedTemplate: "eks" })));
    expect(calls).toEqual([`/v1/tenants/${TENANT}/templates/tpl_1`]);
    // Replaced, so Back doesn't return to a template that is gone.
    expect(router.state.historyAction).toBe("REPLACE");
  });

  it("asks without listing what goes with it when there is no revision yet", () => {
    const queryClient = seed();
    queryClient.setQueryData(queryKeys.templates(TENANT), [template({ id: "tpl_new", name: "eks-typo", latest_template_revision_id: "", latest_sync: null })]);
    queryClient.setQueryData(queryKeys.templateRevisions(TENANT, "tpl_new"), []);
    renderPanel(queryClient, "/templates/tpl_new/variables");

    fireEvent.click(screen.getByTestId("template-delete"));
    const confirm = screen.getByTestId("template-delete-confirm");
    expect(within(confirm).getByText("Delete eks-typo?")).toBeTruthy();
    expect(confirm.textContent).not.toContain("revisions");
  });

  it("says a stack has installed it when the server refuses with template-in-use", async () => {
    answerDelete(() => problem(409, problemTypes.templateInUse, "A stack has installed this template, so it can't be deleted."));
    renderPanel(seed(), "/templates/tpl_1/variables");

    fireEvent.click(screen.getByTestId("template-delete"));
    fireEvent.click(within(screen.getByTestId("template-delete-confirm")).getByRole("button", { name: "Delete template" }));

    const error = await screen.findByTestId("template-delete-error");
    expect(within(error).getByText("Can't delete this template")).toBeTruthy();
    expect(error.textContent).toContain("A stack has installed it");
    expect(screen.queryByTestId("template-delete-confirm")).toBeNull();
    expect(document.activeElement).toBe(screen.getByTestId("template-delete"));
    expect(screen.queryByTestId("templates-index")).toBeNull();
  });

  it("shows any other refusal in the server's words", async () => {
    answerDelete(() => problem(403, "about:blank", "You may not publish templates."));
    renderPanel(seed(), "/templates/tpl_1/variables");

    fireEvent.click(screen.getByTestId("template-delete"));
    fireEvent.click(within(screen.getByTestId("template-delete-confirm")).getByRole("button", { name: "Delete template" }));

    const error = await screen.findByTestId("template-delete-error");
    expect(within(error).getByText("Could not delete this template")).toBeTruthy();
    expect(error.textContent).toContain("You may not publish templates.");
  });

  it("ends where a delete does when someone else deleted it first", async () => {
    answerDelete(() => problem(404, "about:blank", "Not found."));
    renderPanel(seed(), "/templates/tpl_1/variables");

    fireEvent.click(screen.getByTestId("template-delete"));
    fireEvent.click(within(screen.getByTestId("template-delete-confirm")).getByRole("button", { name: "Delete template" }));

    await waitFor(() => expect(screen.getByTestId("templates-index").textContent).toContain("eks"));
  });

  it("drops a question asked about another template", () => {
    const router = renderPanel(seed(), "/templates/tpl_1/variables");
    fireEvent.click(screen.getByTestId("template-delete"));
    expect(screen.getByTestId("template-delete-confirm")).toBeTruthy();

    void router.navigate("/templates/tpl_rds/variables");
    return waitFor(() => {
      expect(screen.getByRole("heading", { level: 2, name: "rds" })).toBeTruthy();
      expect(screen.queryByTestId("template-delete-confirm")).toBeNull();
    });
  });
});

// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { TemplateRevision } from "../../api/types";
import TemplatesPage from "./TemplatesPage";
import { authValue, jsonResponse, latestSync, revision, seedTemplates, template, testQueryClient } from "./testSupport";

// The page through the same route shape as router.tsx, with the panel's
// routes stubbed: these tests are about the frame, the header and the list.

const eks = revision({ id: "rev_eks", source_template_id: "tpl_eks", name: "eks", root_path: "aws/eks" });
const network = revision({
  id: "rev_net",
  source_template_id: "tpl_net",
  name: "network",
  root_path: "aws/network",
  source_ref: "v2.3.0",
  description: "A VPC with public and private subnets.",
  tags: ["networking"]
});
const cdn = revision({ id: "rev_cdn", source_template_id: "tpl_cdn", repo_name: "edge", name: "cloudfront-site", root_path: ".", source_ref: "v2.0.1", tags: [] });

function seed(revisions: TemplateRevision[] = [eks, network, cdn]): QueryClient {
  const queryClient = testQueryClient();
  seedTemplates(queryClient, revisions);
  return queryClient;
}

function renderPage(queryClient: QueryClient, path: string, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [
      {
        path: "/templates",
        element: <TemplatesPage />,
        children: [
          { index: true, element: <p data-testid="index-content">index</p> },
          { path: "new", element: <p data-testid="register-content">register</p> },
          { path: ":sourceTemplateId/variables", element: <p data-testid="variables-content">variables</p> },
          { path: ":sourceTemplateId/revisions", element: <p data-testid="revisions-content">revisions</p> }
        ]
      }
    ],
    { initialEntries: [path] }
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

beforeEach(() => {
  // Everything is seeded; anything else stays pending rather than reaching out.
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplatesPage header", () => {
  it("titles the page and counts templates, not revisions", () => {
    renderPage(seed([revision({ id: "rev_eks_2", source_template_id: "tpl_eks" }), eks, network, cdn]), "/templates");

    expect(screen.getByRole("heading", { level: 1, name: "Templates" })).toBeTruthy();
    expect(screen.getByTestId("page-count").textContent).toBe("3");
  });

  it("offers Register template only to people who can publish templates", () => {
    renderPage(seed(), "/templates");
    expect(screen.getByTestId("register-template-link").getAttribute("href")).toBe("/templates/new");
    cleanup();

    renderPage(seed(), "/templates", false);
    expect(screen.queryByTestId("register-template-link")).toBeNull();
  });
});

describe("TemplatesPage list", () => {
  it("lists templates by name in one list, each with its repository and ref", () => {
    renderPage(seed(), "/templates");

    const list = screen.getByRole("list", { name: "Templates" });
    expect(within(list).getAllByRole("link").map((link) => link.dataset.testid)).toEqual(["template-link-tpl_cdn", "template-link-tpl_eks", "template-link-tpl_net"]);
    expect(screen.queryByRole("heading", { name: "acme/infra-modules" })).toBeNull();
    expect(screen.getByTestId("template-link-tpl_eks").textContent).toBe("eksacme/infra-modules·main");
    expect(screen.getByTestId("template-link-tpl_cdn").textContent).toBe("cloudfront-siteacme/edge·v2.0.1");
  });

  it("opens on the first template without changing the URL", () => {
    const router = renderPage(seed(), "/templates");

    expect(screen.getByTestId("template-link-tpl_cdn").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("template-link-tpl_eks").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("index-content")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/templates");
  });

  it("selects the template in the URL", () => {
    renderPage(seed(), "/templates/tpl_net/revisions");

    expect(screen.getByTestId("template-link-tpl_net").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("template-link-tpl_eks").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("revisions-content")).toBeTruthy();
  });

  it.each([
    ["/templates", "/templates/tpl_net/variables"],
    ["/templates/tpl_eks/revisions", "/templates/tpl_net/revisions"],
    ["/templates/new", "/templates/tpl_net/variables"]
  ])("from %s, another template's row opens %s", (from, to) => {
    renderPage(seed(), from);
    expect(screen.getByTestId("template-link-tpl_net").getAttribute("href")).toBe(to);
  });

  it("selects nothing while registering", () => {
    renderPage(seed(), "/templates/new");
    expect(screen.getByTestId("register-content")).toBeTruthy();
    expect(screen.queryAllByRole("link", { current: true })).toHaveLength(0);
  });

  it("narrows the list as the person types, and says when nothing matches", () => {
    renderPage(seed(), "/templates");
    const filter = screen.getByRole("searchbox", { name: "Filter templates" });

    fireEvent.change(filter, { target: { value: "Networking" } });
    expect(screen.getByTestId("template-link-tpl_net")).toBeTruthy();
    expect(screen.queryByTestId("template-link-tpl_eks")).toBeNull();
    expect(screen.queryByTestId("template-group-acme/edge")).toBeNull();

    fireEvent.change(filter, { target: { value: "edge" } });
    expect(screen.getByTestId("template-link-tpl_cdn")).toBeTruthy();
    expect(screen.queryByTestId("template-link-tpl_net")).toBeNull();

    fireEvent.change(filter, { target: { value: "zzz" } });
    expect(screen.getByTestId("templates-filter-empty").textContent).toBe("No templates match this filter.");
  });

  it("says when there are no templates at all", () => {
    renderPage(seed([]), "/templates");
    expect(screen.getByTestId("templates-none").textContent).toBe("No templates yet.");
    expect(screen.getByTestId("page-count").textContent).toBe("0");
  });

  // A template exists from the moment it is registered, so one whose first
  // sync has not produced a revision is listed and counted, and says it has
  // nothing to install.
  it("lists a template no sync has produced a revision for yet, marked no revision", () => {
    const queryClient = testQueryClient();
    seedTemplates(queryClient, [eks], [template({ id: "tpl_new", name: "fresh", latest_template_revision_id: "" })]);
    renderPage(queryClient, "/templates");

    expect(screen.getByTestId("template-link-tpl_new").textContent).toContain("fresh");
    expect(screen.getByTestId("template-no-revision-tpl_new").textContent).toBe("no revision");
    expect(screen.queryByTestId("template-no-revision-tpl_eks")).toBeNull();
    expect(screen.getByTestId("page-count").textContent).toBe("2");
  });

  // Idle grey on primary-soft is 4.3:1, under AA; the selected row's label
  // takes the muted colour.
  it("keeps the no revision label readable on the selected row", () => {
    const queryClient = testQueryClient();
    seedTemplates(queryClient, [eks], [template({ id: "tpl_new", name: "fresh", latest_template_revision_id: "" })]);
    renderPage(queryClient, "/templates/tpl_new/variables");

    expect(screen.getByTestId("template-link-tpl_new").getAttribute("aria-current")).toBe("true");
    const label = screen.getByTestId("template-no-revision-tpl_new");
    expect(label.className).toContain("text-muted-foreground");
    expect(label.className).not.toContain("text-subtle-foreground");
  });

  // A sync that failed, was invalid or stalled waits on a platform engineer,
  // so the row says so in the panel's words; one still running, or one that
  // ended well, needs no one and marks nothing.
  it.each([
    ["failed", "sync failed"],
    ["invalid", "sync failed"],
    ["stalled", "sync stalled"],
    ["syncing", null],
    ["synced", null],
    ["up_to_date", null]
  ] as const)("marks a template whose latest sync is %s", (status, words) => {
    const queryClient = testQueryClient();
    seedTemplates(queryClient, [], [template({ id: "tpl_x", name: "x", latest_sync: latestSync({ status }) })]);
    renderPage(queryClient, "/templates");

    const mark = screen.queryByTestId("template-sync-trouble-tpl_x");
    expect(mark?.textContent ?? null).toBe(words);
    if (words) {
      expect(within(screen.getByTestId("template-link-tpl_x")).getByText(words)).toBeTruthy();
    }
  });

  // No revision is what a failed first sync leaves behind: the row tells it
  // once, by the failure. One still syncing has nothing to install yet.
  it.each([
    ["failed", "sync failed", false],
    ["stalled", "sync stalled", false],
    ["syncing", null, true]
  ] as const)("shows one mark on a template with no revision whose sync is %s", (status, words, noRevision) => {
    const queryClient = testQueryClient();
    seedTemplates(queryClient, [], [template({ id: "tpl_new", name: "fresh", latest_template_revision_id: "", latest_sync: latestSync({ status }) })]);
    renderPage(queryClient, "/templates");

    expect(screen.queryByTestId("template-sync-trouble-tpl_new")?.textContent ?? null).toBe(words);
    expect(screen.queryByTestId("template-no-revision-tpl_new") !== null).toBe(noRevision);
  });
});

// On a phone the page shows the list or the panel, never both. It is CSS, so
// these read the classes that switch it.
describe("TemplatesPage on a phone", () => {
  it("shows only the list on /templates", () => {
    renderPage(seed(), "/templates");

    expect(screen.getByTestId("templates-list-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("templates-panel-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.queryByRole("link", { name: "Templates" })).toBeNull();
  });

  it("shows only the panel on a template's address, with a way back to the list", () => {
    renderPage(seed(), "/templates/tpl_eks/variables");

    expect(screen.getByTestId("templates-list-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("templates-panel-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByRole("link", { name: "Templates" }).getAttribute("href")).toBe("/templates");
  });
});

describe("TemplatesPage loading and errors", () => {
  it("says it is loading while the templates load", () => {
    renderPage(testQueryClient(), "/templates");
    expect(screen.getByTestId("templates-loading").textContent).toContain("Loading templates…");
  });

  it("offers a retry when the templates fail to load", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500));
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("templates-error")).toBeTruthy());
    expect(screen.getByTestId("templates-error").textContent).toContain("Something went wrong while loading templates.");
    expect(screen.getByTestId("templates-retry")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  // A server fault's cause is only in the log, under the request id.
  it("shows the request id when the templates fail with a server fault", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () =>
      jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom", request_id: "8cf6602cb5083bde" }, 500)
    );
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("request-id").textContent).toBe("Request ID: 8cf6602cb5083bde"));
  });

  it("hands a refused request to the shared boundary", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ type: "about:blank", title: "Forbidden", detail: "forbidden" }, 403));
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });
});

describe("TemplatesPage after a delete", () => {
  it("says which template was deleted, and moves focus there", async () => {
    const router = renderPage(seed(), "/templates");
    await router.navigate("/templates", { replace: true, state: { deletedTemplate: "eks-typo" } });

    const note = await screen.findByTestId("template-deleted");
    expect(note.textContent).toBe("Deleted eks-typo.");
    expect(note.getAttribute("role")).toBe("status");
    expect(document.activeElement).toBe(note);
  });

  it("says nothing on an ordinary visit", () => {
    renderPage(seed(), "/templates");
    expect(screen.queryByTestId("template-deleted")).toBeNull();
  });
});

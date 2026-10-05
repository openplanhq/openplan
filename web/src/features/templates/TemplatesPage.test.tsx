// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRevision } from "../../api/types";
import TemplatesPage from "./TemplatesPage";
import { authValue, jsonResponse, revision, TENANT, testQueryClient } from "./testSupport";

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
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), revisions);
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
  it("lists templates under their repository, each with its path and ref", () => {
    renderPage(seed(), "/templates");

    const infra = screen.getByTestId("template-group-acme/infra-modules");
    expect(within(infra).getByRole("heading", { name: "acme/infra-modules" })).toBeTruthy();
    expect(within(infra).getAllByRole("link")).toHaveLength(2);
    expect(screen.getByTestId("template-link-tpl_eks").textContent).toContain("aws/eks · main");
    // A module at the root shows its ref alone.
    expect(screen.getByTestId("template-link-tpl_cdn").textContent).toBe("cloudfront-sitev2.0.1");
  });

  it("opens on the first template without changing the URL", () => {
    const router = renderPage(seed(), "/templates");

    expect(screen.getByTestId("template-link-tpl_eks").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("template-link-tpl_net").getAttribute("aria-current")).toBeNull();
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

  // Review focus 1: a template from before source ids is keyed on its
  // identity tuple, which is JSON, so its link must be encoded and must come
  // back to the same template.
  it("links and selects a template whose id is its identity tuple", () => {
    const legacy = revision({ id: "rev_old", source_template_id: "", repo_name: "legacy", root_path: "." });
    const id = JSON.stringify(["acme", "legacy", ".", "main"]);
    renderPage(seed([eks, legacy]), "/templates");

    const row = screen.getByTestId(`template-link-${id}`);
    expect(row.getAttribute("href")).toBe(`/templates/${encodeURIComponent(id)}/variables`);
    fireEvent.click(row);
    expect(screen.getByTestId(`template-link-${id}`).getAttribute("aria-current")).toBe("true");
  });

  // A slash in the tuple is encoded too, so the id stays one path segment.
  it("links and selects a template whose identity tuple has a slash in its root path", () => {
    const legacy = revision({ id: "rev_old", source_template_id: "", repo_name: "legacy", root_path: "modules/eks" });
    const id = JSON.stringify(["acme", "legacy", "modules/eks", "main"]);
    renderPage(seed([eks, legacy]), "/templates");

    const row = screen.getByTestId(`template-link-${id}`);
    expect(row.getAttribute("href")).toBe(`/templates/${encodeURIComponent(id)}/variables`);
    expect(row.getAttribute("href")).toContain("modules%2Feks");
    fireEvent.click(row);
    expect(screen.getByTestId(`template-link-${id}`).getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("template-link-tpl_eks").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("variables-content")).toBeTruthy();
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
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ error: "boom", message: "boom" }, 500));
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("templates-error")).toBeTruthy());
    expect(screen.getByTestId("templates-error").textContent).toContain("Something went wrong while loading templates.");
    expect(screen.getByTestId("templates-retry")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("hands a refused request to the shared boundary", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ error: "forbidden", message: "forbidden" }, 403));
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });
});

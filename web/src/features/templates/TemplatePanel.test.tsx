// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRevision } from "../../api/types";
import TemplatePanel from "./TemplatePanel";
import { authValue, revision, TENANT, testQueryClient, variable } from "./testSupport";

const latest = revision({ id: "rev_2", resolved_commit_sha: "9e7d3b2aaaabbbbccccddddeeeeffff000011112", created_at: "2026-10-05T09:30:00Z" });
const older = revision({ id: "rev_1" });
const rds = revision({ id: "rev_rds", source_template_id: "tpl_rds", name: "rds", root_path: "aws/rds", description: "", tags: [] });
const cdn = revision({ id: "rev_cdn", source_template_id: "tpl_cdn", repo_name: "edge", name: "cloudfront-site", root_path: ".", source_ref: "v2.0.1" });

function seed(revisions: TemplateRevision[] = [latest, older, rds, cdn]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), revisions);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_2"), [
    variable({ template_revision_id: "rev_2" }),
    variable({ template_revision_id: "rev_2", name: "node_desired_size", type_expression: "number", required: false, has_default: true })
  ]);
  return queryClient;
}

function renderPanel(queryClient: QueryClient, path: string, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [
      {
        path: "/templates/:sourceTemplateId",
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
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
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

  it("links the repository, root path, ref and latest commit on GitHub, with the commit's registration date", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");
    const details = within(screen.getByTestId("template-details"));

    expect(details.getByRole("link", { name: /^acme\/infra-modules,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules");
    expect(details.getByRole("link", { name: /^aws\/eks,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules/tree/main/aws/eks");
    expect(details.getByRole("link", { name: /^main,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules/tree/main");
    const commit = details.getByRole("link", { name: /^9e7d3b2,/ });
    expect(commit.getAttribute("href")).toBe("https://github.com/acme/infra-modules/commit/9e7d3b2aaaabbbbccccddddeeeeffff000011112");
    expect(screen.getByTestId("template-details").textContent).toMatch(/5 Oct(ober)? 2026/);
    for (const link of details.getAllByRole("link")) {
      expect(link.getAttribute("target")).toBe("_blank");
    }
  });

  it("calls a module at the root the repository root", () => {
    renderPanel(seed(), "/templates/tpl_cdn/variables");
    const root = within(screen.getByTestId("template-details")).getByRole("link", { name: /^the repository root,/ });
    expect(root.getAttribute("href")).toBe("https://github.com/acme/edge/tree/v2.0.1");
  });

  it("says so when the URL names no registered template", () => {
    renderPanel(seed(), "/templates/gone/variables");
    expect(screen.getByTestId("template-missing").textContent).toBe("That template is not registered.");
  });
});

describe("TemplatePanel tabs", () => {
  it("marks the current tab and counts the variables and revisions", () => {
    renderPanel(seed(), "/templates/tpl_1/revisions");
    const nav = within(screen.getByRole("navigation", { name: "Template sections" }));

    const variables = nav.getByRole("link", { name: "Variables 2" });
    expect(variables.getAttribute("href")).toBe("/templates/tpl_1/variables");
    expect(variables.getAttribute("aria-current")).toBeNull();

    const revisions = nav.getByRole("link", { name: "Revisions 2" });
    expect(revisions.getAttribute("href")).toBe("/templates/tpl_1/revisions");
    expect(revisions.getAttribute("aria-current")).toBe("page");
    expect(screen.getByTestId("revisions-content")).toBeTruthy();
  });
});

// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRevision } from "../../api/types";
import TemplatesIndexPanel from "./TemplatesIndexPanel";
import TemplatesPage from "./TemplatesPage";
import { authValue, revision, TENANT, testQueryClient, variable } from "./testSupport";

function seed(revisions: TemplateRevision[]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), revisions);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), [variable()]);
  return queryClient;
}

function renderIndex(queryClient: QueryClient, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [{ path: "/templates", element: <TemplatesPage />, children: [{ index: true, element: <TemplatesIndexPanel /> }] }],
    { initialEntries: ["/templates"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
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

describe("TemplatesIndexPanel", () => {
  it("shows the first template on its Variables tab", () => {
    renderIndex(seed([revision(), revision({ id: "rev_net", source_template_id: "tpl_net", name: "network" })]));

    expect(screen.getByRole("heading", { level: 2, name: "eks" })).toBeTruthy();
    expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy();
    const nav = within(screen.getByRole("navigation", { name: "Template sections" }));
    expect(nav.getByRole("link", { name: /^Variables/ }).getAttribute("aria-current")).toBe("page");
  });

  it("invites a publisher to register the first template", () => {
    renderIndex(seed([]));

    const empty = screen.getByTestId("templates-empty");
    expect(within(empty).getByRole("heading", { name: "No templates yet" })).toBeTruthy();
    expect(empty.textContent).toContain("Register a Terraform module from a Git repository to make it available to your stacks.");
    expect(within(empty).getByRole("link", { name: "Register template" }).getAttribute("href")).toBe("/templates/new");
  });

  it("tells everyone else who registers templates", () => {
    renderIndex(seed([]), false);

    const empty = screen.getByTestId("templates-empty");
    expect(empty.textContent).toContain("Templates appear here once someone who can publish registers one.");
    expect(within(empty).queryByRole("link")).toBeNull();
  });
});

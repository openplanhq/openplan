// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateVariable } from "../../api/types";
import TemplatePanel from "./TemplatePanel";
import VariablesTab from "./VariablesTab";
import { authValue, jsonResponse, revision, TENANT, testQueryClient, variable } from "./testSupport";

function seed(variables?: TemplateVariable[]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [revision()]);
  if (variables) {
    queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), variables);
  }
  return queryClient;
}

function renderTab(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [{ path: "/templates/:sourceTemplateId", element: <TemplatePanel />, children: [{ path: "variables", element: <VariablesTab /> }] }],
    { initialEntries: ["/templates/tpl_1/variables"] }
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

describe("VariablesTab", () => {
  it("lists the latest revision's variables: name, type, required or optional, description", () => {
    renderTab(
      seed([
        variable(),
        variable({ name: "subnet_ids", type_expression: "list(string)", description: "Private subnets." }),
        variable({ name: "node_desired_size", type_expression: "number", required: false, has_default: true, description: "Nodes." }),
        variable({ name: "tags", type_expression: "", required: false, has_default: true, description: "" })
      ])
    );

    expect(screen.getByTestId("template-variables").textContent).toContain("From the latest revision, 3f9c2a1.");
    const required = within(screen.getByTestId("template-variable-cluster_name"));
    expect(required.getByText("cluster_name")).toBeTruthy();
    expect(required.getByText("string")).toBeTruthy();
    expect(required.getByText("required").className).toContain("font-medium");
    expect(within(screen.getByTestId("template-variable-subnet_ids")).getByText("list(string)")).toBeTruthy();
    expect(within(screen.getByTestId("template-variable-node_desired_size")).getByText("optional")).toBeTruthy();
    // A variable with no type takes any value, as in Terraform.
    expect(within(screen.getByTestId("template-variable-tags")).getByText("any")).toBeTruthy();
    expect(screen.getByRole("table").querySelector("colgroup")).not.toBeNull();
  });

  it("says when the template takes no variables", () => {
    renderTab(seed([]));
    expect(screen.getByTestId("template-variables-none").textContent).toBe("This template takes no variables.");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("says it is loading while the variables load", () => {
    renderTab(seed());
    expect(screen.getByTestId("template-variables-loading").textContent).toContain("Loading variables…");
  });

  it("offers a retry when the variables fail to load", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ error: "boom", message: "boom" }, 500));
    renderTab(seed());

    await waitFor(() => expect(screen.getByTestId("template-variables-error")).toBeTruthy());
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse([variable()]));
    fireEvent.click(screen.getByTestId("template-variables-retry"));
    await waitFor(() => expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy());
  });

  it("follows the newest revision when a sync registers one", async () => {
    const queryClient = seed([variable()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_2"), [variable({ template_revision_id: "rev_2", name: "cluster_version" })]);
    renderTab(queryClient);
    expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy();

    act(() => {
      queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [
        revision({ id: "rev_2", resolved_commit_sha: "9e7d3b2aaaa" }),
        revision()
      ]);
    });
    await waitFor(() => expect(screen.getByTestId("template-variable-cluster_version")).toBeTruthy());
    expect(screen.getByTestId("template-variables").textContent).toContain("From the latest revision, 9e7d3b2.");
  });
});

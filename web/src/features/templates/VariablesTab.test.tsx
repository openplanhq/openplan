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
import { authValue, jsonResponse, revision, TENANT, testQueryClient, variable, seedTemplates } from "./testSupport";

function seed(variables?: TemplateVariable[]): QueryClient {
  const queryClient = testQueryClient();
  seedTemplates(queryClient, [revision()]);
  if (variables) {
    queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), variables);
  }
  return queryClient;
}

function renderTab(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [{ path: "/templates/:templateId", element: <TemplatePanel />, children: [{ path: "variables", element: <VariablesTab /> }] }],
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

  // Sensitive and required are separate facts: a secret can be either. Value
  // keeps saying required or optional, a sensitive tag follows the name, and
  // the note says where a sensitive value comes from.
  it("tags a sensitive variable by its name and keeps required or optional in Value", () => {
    renderTab(
      seed([
        variable({ name: "db_password", sensitive: true }),
        variable({ name: "api_token", sensitive: true, required: false, has_default: true }),
        variable()
      ])
    );

    const required = within(screen.getByTestId("template-variable-db_password"));
    expect(required.getByText("sensitive")).toBeTruthy();
    expect(required.getByText("required").className).toContain("font-medium");
    expect(required.queryByText("secret")).toBeNull();
    const optional = within(screen.getByTestId("template-variable-api_token"));
    expect(optional.getByText("sensitive")).toBeTruthy();
    expect(optional.getByText("optional")).toBeTruthy();
    expect(within(screen.getByTestId("template-variable-cluster_name")).queryByText("sensitive")).toBeNull();
    expect(screen.getByTestId("template-variables").textContent).toContain("A sensitive one gets its value from an Environment secret named TF_VAR_ and its name.");
  });

  // A tag under a long name makes the row two lines tall; the type, value and
  // description stay beside the name's first line instead of centring.
  it("sets every cell on the row's baseline", () => {
    renderTab(seed([variable({ name: "proxmox_ssh_private_key_data", sensitive: true })]));

    for (const cell of within(screen.getByTestId("template-variable-proxmox_ssh_private_key_data")).getAllByRole("cell")) {
      expect(cell.className).toContain("align-baseline");
      expect(cell.className).not.toContain("align-middle");
    }
  });

  it("wraps a long type only at its structure, never inside a word (#459)", () => {
    renderTab(seed([variable({ name: "node_pools", type_expression: "map(object({ size = optional(number), labels = optional(map(string)) }))" })]));
    const cell = within(screen.getByTestId("template-variable-node_pools")).getByText(/^map\(object/);
    expect(cell.textContent).toBe("map(object({ size = optional(number), labels = optional(map(string)) }))");
    // A break after each "(", "{" and ",": "optional(" then "map(string))".
    expect(cell.querySelectorAll("wbr")).toHaveLength(7);
    expect(cell.childNodes[cell.childNodes.length - 1].textContent).toBe("string)) }))");
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
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500));
    renderTab(seed());

    await waitFor(() => expect(screen.getByTestId("template-variables-error")).toBeTruthy());
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse([variable()]));
    fireEvent.click(screen.getByTestId("template-variables-retry"));
    await waitFor(() => expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy());
  });

  it("follows the newest revision when a sync registers one", async () => {
    // rev_2's variables are not cached, so they load. Until they do, the tab
    // says so, rather than show rev_1's variables under rev_2's commit.
    let resolveVariables: (response: Response) => void = () => {};
    vi.mocked(globalThis.fetch).mockImplementation(
      (input) =>
        new Promise((resolve) => {
          if (String(input).includes("/template-revisions/rev_2/variables")) {
            resolveVariables = resolve;
          }
        })
    );
    const queryClient = seed([variable()]);
    renderTab(queryClient);
    expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy();

    act(() => {
      seedTemplates(queryClient, [
        revision({ id: "rev_2", resolved_commit_sha: "9e7d3b2aaaa" }),
        revision()
      ]);
    });
    await waitFor(() => expect(screen.getByTestId("template-variables-loading").textContent).toContain("Loading variables…"));
    expect(within(screen.getByTestId("template-source")).getByRole("link", { name: /^Latest commit: ?9e7d3b2,/ })).toBeTruthy();
    expect(screen.queryByTestId("template-variable-cluster_name")).toBeNull();

    await act(async () => {
      resolveVariables(jsonResponse([variable({ template_revision_id: "rev_2", name: "cluster_version" })]));
    });
    await waitFor(() => expect(screen.getByTestId("template-variable-cluster_version")).toBeTruthy());
    expect(screen.getByTestId("template-variables").textContent).toContain("From the latest revision, 9e7d3b2.");
  });
});

// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider, useParams } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import RegisterTemplatePanel from "./RegisterTemplatePanel";
import { authValue, jsonResponse, registration, revision, TENANT, testQueryClient } from "./testSupport";

function Opened() {
  const { sourceTemplateId = "" } = useParams<{ sourceTemplateId: string }>();
  return <p data-testid="opened">{sourceTemplateId}</p>;
}

function seed(): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [revision()]);
  return queryClient;
}

function renderPanel(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [
      { path: "/templates", element: <p data-testid="templates-stub">templates</p> },
      { path: "/templates/new", element: <RegisterTemplatePanel /> },
      { path: "/templates/:sourceTemplateId/variables", element: <Opened /> }
    ],
    { initialEntries: ["/templates/new"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

function submit() {
  fireEvent.click(screen.getByTestId("register-template-submit"));
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("RegisterTemplatePanel", () => {
  it("asks for the repository, ref and root path, with today's defaults and a way back", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed());

    expect(screen.getByRole("heading", { level: 2, name: "Register template" })).toBeTruthy();
    expect((screen.getByLabelText("Owner") as HTMLInputElement).value).toBe("hashicorp");
    expect((screen.getByLabelText("Repository") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Ref") as HTMLInputElement).value).toBe("main");
    expect((screen.getByLabelText("Root path") as HTMLInputElement).value).toBe(".");
    expect(screen.getByLabelText("Root path").getAttribute("aria-describedby")).toBeTruthy();
    expect(screen.getByText("The module's directory in the repository, or . when the module is at its root.")).toBeTruthy();
    expect(screen.getByTestId("register-template-cancel").getAttribute("href")).toBe("/templates");
  });

  it("marks itself unsaved once a field is edited, so a background sign-in waits", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed());

    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "edge" } });
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  it("posts the fields, says it is registering, then opens the new template", async () => {
    const added = revision({ id: "rev_9", source_template_id: "tpl_9", repo_name: "edge", root_path: "." });
    let listed = [revision()];
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "running", template_revision_id: "" }));
      if (String(input).includes("/template-registrations/")) {
        listed = [added, revision()];
        return jsonResponse(registration({ template_revision_id: "rev_9" }));
      }
      return jsonResponse(listed);
    });
    renderPanel(seed());

    fireEvent.change(screen.getByLabelText("Owner"), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "edge" } });
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-progress").textContent).toContain("Registering acme/edge at main"));
    expect((screen.getByLabelText("Repository") as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(true);

    await waitFor(() => expect(screen.getByTestId("opened").textContent).toBe("tpl_9"), { timeout: 3000 });
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ repo_owner: "acme", repo_name: "edge", source_ref: "main", root_path: "." });
  });

  // Review focus 4: the identity was registered already. The registration
  // completes with that template's revision, which is on screen.
  it("opens the existing template when its identity was registered already", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      init?.method === "POST" || String(input).includes("/template-registrations/")
        ? jsonResponse(registration({ template_revision_id: "rev_1" }))
        : jsonResponse([revision()])
    );
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("opened").textContent).toBe("tpl_1"));
  });

  it.each([
    [registration({ status: "invalid", template_revision_id: "", error_summary: 'root path "aws/sqs-queues": directory does not exist' }), 'root path "aws/sqs-queues": directory does not exist'],
    [registration({ status: "failed", template_revision_id: "", error_summary: "" }), "Registration failed"]
  ])("says why a registration failed and keeps the fields", async (failed, reason) => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => jsonResponse(failed));
    renderPanel(seed());

    fireEvent.change(screen.getByLabelText("Root path"), { target: { value: "aws/sqs-queues" } });
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error")).toBeTruthy());
    const error = within(screen.getByTestId("register-template-error"));
    expect(error.getByRole("alert").textContent).toContain("This template could not be registered. Check the fields and register it again.");
    expect(screen.getByTestId("register-template-error").querySelector("pre")?.textContent).toBe(reason);
    expect((screen.getByLabelText("Root path") as HTMLInputElement).value).toBe("aws/sqs-queues");
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByTestId("opened")).toBeNull();
  });

  it("shows a refused request the same way", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse({ error: "invalid_request", message: "repo_name is required" }, 400) : jsonResponse([revision()])
    );
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error").textContent).toContain("repo_name is required"));
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
  });

  // Review focus 3: a poll that keeps failing must not leave the form busy.
  it("stops and says why when the registration cannot be read", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending", template_revision_id: "" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse({ error: "forbidden", message: "forbidden" }, 403);
      return jsonResponse([revision()]);
    });
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error").textContent).toContain("forbidden"), { timeout: 3000 });
    expect(screen.queryByTestId("register-template-progress")).toBeNull();
    expect((screen.getByLabelText("Owner") as HTMLInputElement).disabled).toBe(false);
  });
});

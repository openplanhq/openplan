// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider, useParams } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { TemplateLatestSync, TemplateRevision } from "../../api/types";
import RegisterTemplatePanel from "./RegisterTemplatePanel";
import TemplatesPage from "./TemplatesPage";
import { authValue, jsonResponse, latestSync, revision, seedTemplates, template, templateReadResponse, TENANT, testQueryClient } from "./testSupport";

const added = revision({ id: "rev_9", template_id: "tpl_9", repo_name: "edge", root_path: "." });

// A fake API. Registering answers with templateID syncing; each read of it
// answers the next of polled, the last one from then on; the list and the
// revisions come from listed(). Anything else stays pending.
function serve({
  templateID = "tpl_9",
  polled,
  listed = () => [revision()],
  registerAnswer,
  onRead
}: {
  templateID?: string;
  polled: Array<Partial<TemplateLatestSync> | Response>;
  listed?: () => TemplateRevision[];
  registerAnswer?: Response;
  onRead?: () => void;
}) {
  let reads = 0;
  const shape = { id: templateID, repo_name: "edge", root_path: ".", latest_template_revision_id: "" };
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    if (init?.method === "POST") {
      return registerAnswer ?? jsonResponse(template({ ...shape, latest_sync: latestSync({ status: "syncing" }) }), 202);
    }
    if (String(input) === `/v1/tenants/${TENANT}/templates/${templateID}`) {
      onRead?.();
      const next = polled[Math.min(reads++, polled.length - 1)];
      return next instanceof Response ? next : jsonResponse(template({ ...shape, latest_sync: latestSync(next) }));
    }
    return templateReadResponse(input, listed()) ?? new Promise<Response>(() => {});
  });
}

function Opened() {
  const { templateId = "" } = useParams<{ templateId: string }>();
  return <p data-testid="opened">{templateId}</p>;
}

function seed(): QueryClient {
  const queryClient = testQueryClient();
  seedTemplates(queryClient, [revision()]);
  return queryClient;
}

function renderPanel(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [
      { path: "/templates", element: <p data-testid="templates-stub">templates</p> },
      { path: "/templates/new", element: <RegisterTemplatePanel /> },
      { path: "/templates/:templateId/variables", element: <Opened /> }
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

// The panel inside the page, as router.tsx nests it: the page owns the list
// query's error, and draws it in place of the panel.
function renderInPage(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [
      {
        path: "/templates",
        element: <TemplatesPage />,
        children: [
          { path: "new", element: <RegisterTemplatePanel /> },
          { path: ":templateId/variables", element: <Opened /> }
        ]
      }
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
    let listed = [revision()];
    const fetchMock = serve({
      polled: [{ status: "synced" }],
      listed: () => listed,
      onRead: () => {
        listed = [added, revision()];
      }
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
    expect(String(post?.[0])).toBe("/v1/tenants/tenant_123/templates");
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ repo_owner: "acme", repo_name: "edge", source_ref: "main", root_path: "." });
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes("template-registrations"))).toBe(false);
  });

  // A live region inserted with its words already in it may go unannounced,
  // so the region is there, empty, before registering starts.
  it("says it is registering in a status region that was there before", async () => {
    serve({ polled: [{ status: "syncing" }] });
    renderPanel(seed());
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "edge" } });

    const status = screen.getByRole("status");
    expect(status.textContent).toBe("");
    submit();

    await waitFor(() => expect(status.textContent).toContain("Registering hashicorp/edge at main"));
    expect(status.contains(screen.getByTestId("register-template-progress"))).toBe(true);
  });

  // The sync has succeeded but the refetched list does not hold the template
  // with its revision yet, so it cannot open: the form stays busy, with
  // nothing to press twice and nothing to retype.
  it("stays busy until the refreshed list holds the new template", async () => {
    let releaseList: () => void = () => {};
    const listReleased = new Promise<void>((resolve) => {
      releaseList = resolve;
    });
    let listRequests = 0;
    const fake = serve({ polled: [{ status: "synced" }] });
    const answer = fake.getMockImplementation()!;
    fake.mockImplementation(async (input, init) => {
      if (String(input).startsWith(`/v1/tenants/${TENANT}/templates?`)) {
        listRequests += 1;
        await listReleased;
        return templateReadResponse(input, [added, revision()])!;
      }
      return answer(input, init);
    });
    renderPanel(seed());
    submit();

    // The list is read once the template is registered, since it exists from
    // then on, and again once the poll has read synced.
    await waitFor(() => expect(listRequests).toBeGreaterThan(1), { timeout: 3000 });
    expect(screen.getByTestId("register-template-progress")).toBeTruthy();
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText("Owner") as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByTestId("register-template-error")).toBeNull();

    releaseList();
    await waitFor(() => expect(screen.getByTestId("opened").textContent).toBe("tpl_9"));
  });

  // Synced, but the list that would open the template cannot be read. The
  // list query is in error, which the page draws in place of this panel, so
  // the form is not left busy with nothing to press.
  it("gives way to the page's retry when the refreshed list cannot be read", async () => {
    let listFails = false;
    const fake = serve({
      polled: [{ status: "synced" }],
      onRead: () => {
        listFails = true;
      }
    });
    const answer = fake.getMockImplementation()!;
    fake.mockImplementation(async (input, init) => {
      if (listFails && String(input).startsWith(`/v1/tenants/${TENANT}/templates?`)) {
        return jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500);
      }
      return answer(input, init);
    });
    renderInPage(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("templates-error")).toBeTruthy(), { timeout: 3000 });
    expect(screen.queryByTestId("register-template-progress")).toBeNull();

    listFails = false;
    fake.mockImplementation(async (input, init) => templateReadResponse(input, [added, revision()]) ?? answer(input, init));
    fireEvent.click(screen.getByTestId("templates-retry"));
    await waitFor(() => expect(screen.getByTestId("template-link-tpl_9")).toBeTruthy());
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
  });

  // Review focus 4: the source was registered already. Registering answers
  // with that template, syncing again, which reads up to date.
  it("opens the existing template when its source was registered already", async () => {
    serve({ templateID: "tpl_1", polled: [{ status: "up_to_date" }] });
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("opened").textContent).toBe("tpl_1"), { timeout: 3000 });
  });

  it.each([
    [{ status: "invalid", error_summary: 'root path "aws/sqs-queues": directory does not exist' }, 'root path "aws/sqs-queues": directory does not exist'],
    [{ status: "failed", error_summary: "" }, "Registration failed"],
    // Still running long after it was asked for: polling stops, and it reads
    // as failed.
    [{ status: "stalled", error_summary: "" }, "Registration failed"]
  ] as const)("says why a registration failed and keeps the fields (%o)", async (failed, reason) => {
    serve({ polled: [failed] });
    renderPanel(seed());

    fireEvent.change(screen.getByLabelText("Root path"), { target: { value: "aws/sqs-queues" } });
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error")).toBeTruthy(), { timeout: 3000 });
    expect(screen.getByTestId("register-template-error").getAttribute("role")).toBe("alert");
    expect(screen.getByTestId("register-template-error").textContent).toContain("Could not register this templateCheck the fields and register it again.");
    expect(screen.getByTestId("register-template-error").querySelector("pre")?.textContent).toBe(reason);
    expect((screen.getByLabelText("Root path") as HTMLInputElement).value).toBe("aws/sqs-queues");
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByTestId("opened")).toBeNull();
  });

  it("shows a refused request the same way", async () => {
    serve({
      polled: [{ status: "syncing" }],
      registerAnswer: jsonResponse(
        {
          type: "https://openplan.run/problems/validation-failed",
          title: "The request has invalid fields",
          detail: "repo_name is required",
          errors: [{ pointer: "#/repo_name", issue: "required", detail: "repo_name is required" }]
        },
        422
      )
    });
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error").textContent).toContain("repo_name is required"));
    expect(screen.getByTestId("register-template-error").textContent).toContain("Could not register this templateCheck the fields and register it again.");
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
  });

  // Review focus 3: a poll that keeps failing must not leave the form busy.
  it("stops and says why when the template cannot be read", async () => {
    serve({ polled: [jsonResponse({ type: "about:blank", title: "Forbidden", detail: "forbidden" }, 403)] });
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error")).toBeTruthy(), { timeout: 3000 });
    // It may still finish, so the lead says so rather than blame the fields.
    expect(screen.getByTestId("register-template-error").textContent).toContain(
      "Could not check on this registrationIt may still finish. Reload the page before registering it again."
    );
    expect(screen.getByTestId("register-template-error").querySelector("pre")?.textContent).toBe("forbidden");
    expect(screen.queryByTestId("register-template-progress")).toBeNull();
    expect((screen.getByLabelText("Owner") as HTMLInputElement).disabled).toBe(false);
  });
});

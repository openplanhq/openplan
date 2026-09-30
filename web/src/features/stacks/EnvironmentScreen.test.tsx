// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import EnvironmentScreen from "./EnvironmentScreen";
import { queryKeys } from "../../api/queryKeys";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";

function authValue(): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

const credential = { id: "credential_1", name: "TF_VAR_TEST", scope: "stack" as const, created_at: "2026-07-19T00:00:00Z" };

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function renderScreen(queryClient: QueryClient) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <MemoryRouter initialEntries={["/stacks/stack_1/environment"]}>
          <Routes>
            <Route path="/stacks/:stackId/environment" element={<EnvironmentScreen />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

describe("EnvironmentScreen", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("renders stack credential metadata without rendering secret values", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { id: "credential_1", name: "TF_VAR_TEST", scope: "stack", created_at: "2026-07-19T00:00:00Z" }
    ]);

    renderScreen(queryClient);

    expect(screen.getByTestId("environment-screen")).toBeTruthy();
    expect(screen.getByText("TF_VAR_TEST")).toBeTruthy();
    expect(screen.getByText(/Values are write-only/)).toBeTruthy();
    expect(screen.queryByText("secret-value")).toBeNull();
  });

  it("marks itself unsaved once a credential secret is typed, so SessionProvider's proactive re-auth defers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
    fireEvent.change(screen.getByLabelText(/Environment credential value/), { target: { value: "secret-value" } });
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  it("creates a stack credential through the existing API mutation", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: "credential_2", name: "TF_VAR_TEST", scope: "stack", created_at: "2026-07-19T00:00:00Z" }), {
        status: 201,
        headers: { "content-type": "application/json" }
      })
    );

    renderScreen(queryClient);
    fireEvent.change(screen.getByLabelText(/Environment credential name/), { target: { value: "TF_VAR_TEST" } });
    fireEvent.change(screen.getByLabelText(/Environment credential value/), { target: { value: "secret-value" } });
    fireEvent.click(screen.getByRole("button", { name: /Add/ }));

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/stacks/stack_1/credentials"),
      expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "TF_VAR_TEST", value: "secret-value" }) })
    ));
  });

  it("lists the credentials in a table, one row each, with a delete button", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      credential,
      { ...credential, id: "credential_2", name: "AWS_ACCESS_KEY_ID" }
    ]);

    renderScreen(queryClient);

    const table = screen.getByRole("table");
    expect(table.classList).toContain("table-fixed");
    expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Name", "Value", "Actions"]);
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell").slice(0, 2).map((cell) => cell.textContent))).toEqual([
      ["TF_VAR_TEST", "configured"],
      ["AWS_ACCESS_KEY_ID", "configured"]
    ]);
    expect(within(rows[1]).getByRole("button", { name: "Delete AWS_ACCESS_KEY_ID" })).toBeTruthy();
  });

  it("says so when no credentials are configured", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(screen.getByText("No credentials configured")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  // base.css gives every h2 the legacy 32px display type until PR 9.
  it("titles the panel with an h2 that sets its own type", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    const heading = screen.getByRole("heading", { level: 2, name: "Environment credentials" });
    for (const name of ["font-heading", "text-base", "font-medium", "tracking-normal"]) {
      expect(heading.classList).toContain(name);
    }
    expect(heading.closest('[data-slot="card"]')?.classList).toContain("text-foreground");
  });

  // The value is a secret: masked while typed, and gone from the page once
  // the server has it. A failed add keeps it, so the user can try again.
  it("masks the secret, and clears both fields once the credential is added", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    // A fresh Response per call: the POST, then the list refetch it triggers.
    // A body can be read only once.
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse(credential, 201) : jsonResponse([credential])
    );
    renderScreen(queryClient);
    const user = userEvent.setup();

    const name = screen.getByLabelText<HTMLInputElement>("Environment credential name");
    const value = screen.getByLabelText<HTMLInputElement>("Environment credential value");
    expect(value.type).toBe("password");
    await user.type(name, "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => expect(value.value).toBe(""));
    expect(name.value).toBe("");
    // Still the panel, now listing the new credential: the checks above mean
    // nothing if the screen has swapped the panel for an error.
    expect(await screen.findByRole("cell", { name: "TF_VAR_TEST" })).toBeTruthy();
    expect(value.isConnected).toBe(true);
    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
  });

  it("keeps what was typed, and says why, when the add fails", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "conflict", message: "A credential with that name already exists" }, 409)
    );
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Environment credential name"), "TF_VAR_TEST");
    await user.type(screen.getByLabelText("Environment credential value"), "secret-value");
    await user.click(screen.getByRole("button", { name: "Add" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("A credential with that name already exists");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.getByLabelText<HTMLInputElement>("Environment credential value").value).toBe("secret-value");
  });

  // The fields stay editable while an add is out. Clearing them when it lands
  // must not wipe the next credential, half typed.
  it("keeps what was typed while an add was in flight", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    let answerPost: (response: Response) => void = () => {};
    vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) =>
      init?.method === "POST"
        ? new Promise<Response>((resolve) => {
            answerPost = resolve;
          })
        : Promise.resolve(jsonResponse([credential]))
    );
    renderScreen(queryClient);
    const user = userEvent.setup();
    const name = screen.getByLabelText<HTMLInputElement>("Environment credential name");
    const value = screen.getByLabelText<HTMLInputElement>("Environment credential value");

    await user.type(name, "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Add" }));
    await user.clear(name);
    await user.type(name, "TF_VAR_NEXT");
    await user.clear(value);
    await user.type(value, "next-secret");
    answerPost(jsonResponse(credential, 201));

    expect(await screen.findByRole("cell", { name: "TF_VAR_TEST" })).toBeTruthy();
    expect(name.value).toBe("TF_VAR_NEXT");
    expect(value.value).toBe("next-secret");
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  it("says why, and keeps the row, when a delete fails", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [credential]);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "conflict", message: "The credential is still in use" }, 409)
    );
    renderScreen(queryClient);

    await userEvent.setup().click(screen.getByRole("button", { name: "Delete TF_VAR_TEST" }));

    expect((await screen.findByRole("alert")).textContent).toContain("The credential is still in use");
    expect(screen.getByRole("cell", { name: "TF_VAR_TEST" })).toBeTruthy();
  });

  it("asks for both fields before sending anything", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Environment credential name"), "TF_VAR_TEST");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect((await screen.findByRole("alert")).textContent).toContain("Name and value are required");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("uses shadcn fields for the credential form", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    for (const label of ["Environment credential name", "Environment credential value"]) {
      expect(screen.getByLabelText(label).getAttribute("data-slot")).toBe("input");
    }
  });

  // --legacy-touch-target gave every control 44px on a touch screen.
  it("gives every control a 44px target on coarse pointers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [credential]);

    renderScreen(queryClient);

    expect(screen.getByLabelText("Environment credential name").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByLabelText("Environment credential value").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: "Add" }).classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: "Delete TF_VAR_TEST" }).classList).toContain("pointer-coarse:size-11");
  });

  it("retries a failed load", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ error: "internal", message: "boom" }, 500))
      .mockResolvedValueOnce(jsonResponse([credential]));

    renderScreen(testQueryClient());

    const retry = await screen.findByTestId("environment-retry");
    expect(retry.classList).toContain("pointer-coarse:h-11");
    await userEvent.setup().click(retry);
    await screen.findByTestId("environment-screen");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("deletes a stack credential through the existing API mutation", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { id: "credential_1", name: "TF_VAR_TEST", scope: "stack", created_at: "2026-07-19T00:00:00Z" }
    ]);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(null, { status: 204 }));

    renderScreen(queryClient);
    fireEvent.click(screen.getByRole("button", { name: "Delete TF_VAR_TEST" }));

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stacks/stack_1/credentials/credential_1",
      expect.objectContaining({ method: "DELETE" })
    ));
  });
});

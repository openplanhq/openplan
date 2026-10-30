// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, getDefaultNormalizer, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import EnvironmentScreen from "./EnvironmentScreen";
import { queryKeys } from "../../api/queryKeys";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import { asListBody } from "../../api/listTestSupport";

function authValue(): AuthContextValue {
  return {
    me: { sub: "user_1", tenant_id: "tenant_123", display_name: "Test User", global_capabilities: { can_create_stack: false, can_manage_templates: false, can_manage_platform_access: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

const credential = { id: "credential_1", name: "TF_VAR_TEST", scope: "stack" as const, sensitive: true, created_at: "2026-07-19T00:00:00Z" };

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(asListBody(body)), { status, headers: { "content-type": "application/json" } });
}

// The two tabs' panels are both mounted, so a field is looked up in the
// panel it belongs to.
const secrets = () => within(screen.getByTestId("environment-secrets"));
const variables = () => within(screen.getByTestId("environment-variables"));
// The page's breadcrumb is a list too, so the entries are looked up in the card.
const card = () => within(screen.getByTestId("environment-card"));
// A secret's box is masked with -webkit-text-security, which jsdom's CSS
// parser drops, so this reads the property React set on the style object.
const masking = (element: HTMLElement) => (element.style as unknown as Record<string, string | undefined>).WebkitTextSecurity ?? "";

function renderScreen(queryClient: QueryClient) {
  // The trail names the stack, so the page reads it; seeded, it sends nothing.
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
    stack: {
      id: "stack_1",
      tenant_id: "tenant_123",
      name: "prod",
      slug: "prod",
      tags: {},
      default_credential_ids: [],
      created_by: "user_1",
      created_at: "2026-07-19T00:00:00Z",
      effective_capabilities: { can_view: true, can_operate: true, can_approve: true, can_manage_access: true }
    },
    templates: []
  });
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
    expect(secrets().getByText("Set as environment variables when Terraform runs. Write-only: a value can't be read back once added.")).toBeTruthy();
    expect(screen.queryByText("secret-value")).toBeNull();
  });

  it("marks itself unsaved once a credential secret is typed, so SessionProvider's proactive re-auth defers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
    fireEvent.change(secrets().getByLabelText("Value"), { target: { value: "secret-value" } });
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
    fireEvent.change(secrets().getByLabelText("Name"), { target: { value: "TF_VAR_TEST" } });
    fireEvent.change(secrets().getByLabelText("Value"), { target: { value: "secret-value" } });
    fireEvent.click(screen.getByRole("button", { name: "Add secret" }));

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith(
      expect.stringContaining("/stacks/stack_1/credentials"),
      expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "TF_VAR_TEST", value: "secret-value", sensitive: true }) })
    ));
  });

  // Midday times: the date is drawn in the runner's time zone, which the
  // test config does not pin, and midnight UTC is the day before in America.
  it("lists the credentials, one row each, with when each was added and a delete button", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { ...credential, created_at: "2026-07-19T12:00:00Z" },
      { ...credential, id: "credential_2", name: "AWS_ACCESS_KEY_ID", created_at: "2026-08-12T12:00:00Z" }
    ]);

    renderScreen(queryClient);

    const rows = within(card().getByRole("list")).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual(["TF_VAR_TESTAdded 19 Jul 2026", "AWS_ACCESS_KEY_IDAdded 12 Aug 2026"]);
    expect(within(rows[1]).getByRole("button", { name: "Delete AWS_ACCESS_KEY_ID" })).toBeTruthy();
  });

  // A name longer than the row truncates, and its title keeps the whole name
  // readable on hover.
  it("keeps a long credential name whole in its title", () => {
    const longName = "TF_VAR_A_VERY_LONG_CREDENTIAL_NAME_THAT_WILL_NOT_FIT_ON_A_PHONE_ROW";
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [{ ...credential, name: longName }]);

    renderScreen(queryClient);

    expect(screen.getByText(longName).getAttribute("title")).toBe(longName);
  });

  it("says so when no credentials are configured, in place of the list", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(secrets().getByText("No secrets in this environment")).toBeTruthy();
    expect(secrets().getByText("Secrets added here are available to every template in this stack.")).toBeTruthy();
    expect(variables().getByText("No variables in this environment")).toBeTruthy();
    expect(card().queryByRole("list")).toBeNull();
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

    const name = secrets().getByLabelText<HTMLInputElement>("Name");
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");
    expect(masking(value)).toBe("disc");
    await user.type(name, "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Add secret" }));

    await waitFor(() => expect(value.value).toBe(""));
    expect(name.value).toBe("");
    // Still the panel, now listing the new credential: the checks above mean
    // nothing if the screen has swapped the panel for an error.
    expect(await screen.findByText("TF_VAR_TEST")).toBeTruthy();
    expect(value.isConnected).toBe(true);
    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
  });

  it("shows the value being typed only while Show value is pressed", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderScreen(queryClient);
    const user = userEvent.setup();
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");
    const toggle = screen.getByRole("button", { name: "Show value" });

    expect(masking(value)).toBe("disc");
    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    await user.type(value, "secret-value");

    await user.click(toggle);
    expect(masking(value)).toBe("");
    expect(toggle.getAttribute("aria-pressed")).toBe("true");
    expect(value.value).toBe("secret-value");
    // Revealed, it still keeps autofill out, and the text away from
    // spellcheck and correction.
    expect(value.getAttribute("autocomplete")).toBe("off");
    expect(value.getAttribute("spellcheck")).toBe("false");
    expect(value.getAttribute("autocorrect")).toBe("off");
    expect(value.getAttribute("autocapitalize")).toBe("off");

    await user.click(toggle);
    expect(masking(value)).toBe("disc");
    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    expect(toggle.getAttribute("type")).toBe("button");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  // A PEM key is many lines, and a broken line break breaks the key. The box
  // hands CRLF over as LF, as a textarea does; nothing else is changed, the
  // trailing newline included.
  it("sends a pasted multi-line secret with its line breaks", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse(credential, 201) : jsonResponse([credential])
    );
    renderScreen(queryClient);
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");

    fireEvent.change(secrets().getByLabelText("Name"), { target: { value: "TF_VAR_ssh_private_key" } });
    fireEvent.change(value, { target: { value: "-----BEGIN KEY-----\r\nabc\n-----END KEY-----\n" } });
    expect(value.value).toBe("-----BEGIN KEY-----\nabc\n-----END KEY-----\n");
    fireEvent.click(screen.getByRole("button", { name: "Add secret" }));

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith(
      expect.stringContaining("/stacks/stack_1/credentials"),
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ name: "TF_VAR_ssh_private_key", value: "-----BEGIN KEY-----\nabc\n-----END KEY-----\n", sensitive: true })
      })
    ));
  });

  // Enter is a line break in a multi-line value, so it can't also add the
  // entry: a key pasted or typed a line at a time is never sent half done.
  it("Enter in the value box adds a line, not the entry", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderScreen(queryClient);
    const user = userEvent.setup();
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");

    // A name too, so an add, if Enter made one, would get past the check.
    await user.type(secrets().getByLabelText("Name"), "TF_VAR_TEST");
    await user.type(value, "a{Enter}b");

    expect(fetchSpy).not.toHaveBeenCalled();
    expect(value.value).toBe("a\nb");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("masks a secret until Show value", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    renderScreen(queryClient);
    const user = userEvent.setup();
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");
    const toggle = secrets().getByRole("button", { name: "Show value" });

    expect(toggle.getAttribute("aria-pressed")).toBe("false");
    expect(masking(value)).toBe("disc");

    await user.click(toggle);
    expect(toggle.getAttribute("aria-pressed")).toBe("true");
    expect(masking(value)).toBe("");
  });

  it("hides the Value box again after an add", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse(credential, 201) : jsonResponse([credential])
    );
    renderScreen(queryClient);
    const user = userEvent.setup();
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");

    await user.type(secrets().getByLabelText("Name"), "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Show value" }));
    expect(masking(value)).toBe("");
    await user.click(screen.getByRole("button", { name: "Add secret" }));

    await waitFor(() => expect(value.value).toBe(""));
    expect(masking(value)).toBe("disc");
    expect(screen.getByRole("button", { name: "Show value" }).getAttribute("aria-pressed")).toBe("false");
  });

  it("keeps the value shown when the add fails, so it can be checked", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500)
    );
    renderScreen(queryClient);
    const user = userEvent.setup();
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");

    await user.type(secrets().getByLabelText("Name"), "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Show value" }));
    await user.click(screen.getByRole("button", { name: "Add secret" }));

    await screen.findByRole("alert");
    expect(masking(value)).toBe("");
  });

  it("keeps what was typed, and says why, when the add fails", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse(
        {
          type: "https://openplan.run/problems/duplicate-credential-name",
          title: "The credential name is taken",
          detail: "A credential with this name already exists here.",
          errors: [{ pointer: "#/name", issue: "duplicate", detail: "A credential with this name already exists here." }]
        },
        409
      )
    );
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(secrets().getByLabelText("Name"), "TF_VAR_TEST");
    await user.type(secrets().getByLabelText("Value"), "secret-value");
    await user.click(screen.getByRole("button", { name: "Add secret" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("A credential with this name already exists here.");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(secrets().getByLabelText<HTMLTextAreaElement>("Value").value).toBe("secret-value");
    // The clash is the name's, so the name is the field marked, and it
    // points at the line that says why.
    const name = secrets().getByLabelText("Name");
    expect(name.getAttribute("aria-invalid")).toBe("true");
    expect(document.getElementById(name.getAttribute("aria-describedby")!)).toBe(alert);
    expect(secrets().getByLabelText("Value").getAttribute("aria-invalid")).toBeNull();
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
    const name = secrets().getByLabelText<HTMLInputElement>("Name");
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");

    await user.type(name, "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Add secret" }));
    await user.clear(name);
    await user.type(name, "TF_VAR_NEXT");
    await user.clear(value);
    await user.type(value, "next-secret");
    answerPost(jsonResponse(credential, 201));

    expect(await screen.findByText("TF_VAR_TEST")).toBeTruthy();
    expect(name.value).toBe("TF_VAR_NEXT");
    expect(value.value).toBe("next-secret");
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  it("says why, and keeps the row, when a delete fails", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [credential]);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ type: "about:blank", title: "Conflict", detail: "The credential is still in use" }, 409)
    );
    renderScreen(queryClient);

    await userEvent.setup().click(screen.getByRole("button", { name: "Delete TF_VAR_TEST" }));

    expect((await screen.findByRole("alert")).textContent).toContain("The credential is still in use");
    expect(screen.getByText("TF_VAR_TEST")).toBeTruthy();
  });

  it("asks for both fields before sending anything", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(secrets().getByLabelText("Name"), "TF_VAR_TEST");
    await user.click(screen.getByRole("button", { name: "Add secret" }));

    expect((await screen.findByRole("alert")).textContent).toContain("Name and value are required");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("uses shadcn fields for the credential form", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(secrets().getByLabelText("Name").getAttribute("data-slot")).toBe("input");
    // The Value box is shadcn's input group: its textarea and the reveal
    // button. A variable's is shadcn's Textarea.
    expect(secrets().getByLabelText("Value").getAttribute("data-slot")).toBe("input-group-control");
    expect(secrets().getByLabelText("Value").tagName).toBe("TEXTAREA");
    expect(variables().getByLabelText("Value").getAttribute("data-slot")).toBe("textarea");
  });

  // Every control takes a 44px target on a coarse pointer.
  it("gives every control a 44px target on coarse pointers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [credential]);

    renderScreen(queryClient);

    expect(secrets().getByLabelText("Name").classList).toContain("pointer-coarse:h-11");
    // A Value box grows with its content, so its one-line height comes
    // from its padding: 12px around an 18px line, inside a 1px border.
    expect(secrets().getByLabelText("Value").classList).toContain("pointer-coarse:py-3");
    expect(variables().getByLabelText("Value").classList).toContain("pointer-coarse:py-3");
    expect(screen.getByRole("button", { name: "Add secret" }).classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: "Delete TF_VAR_TEST" }).classList).toContain("pointer-coarse:size-11");
  });

  it("retries a failed load", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ type: "about:blank", title: "Internal Server Error", detail: "boom" }, 500))
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

  // #497: an entry's tab comes from its sensitive flag. Secrets list names
  // only; Variables show each value.
  it("splits the environment into Secrets and Variables by the sensitive flag", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { ...credential, created_at: "2026-07-19T12:00:00Z" },
      { ...credential, id: "credential_2", name: "AWS_REGION", sensitive: false, value: "eu-west-1", created_at: "2026-07-19T12:00:00Z" }
    ]);
    renderScreen(queryClient);
    const user = userEvent.setup();

    const switcher = screen.getByRole("group", { name: "Environment" });
    const secretsOption = within(switcher).getByRole("button", { name: "Secrets" });
    const variablesOption = within(switcher).getByRole("button", { name: "Variables" });
    // It opens on Secrets.
    expect(secretsOption.getAttribute("aria-pressed")).toBe("true");
    expect(within(card().getByRole("list")).getAllByRole("listitem").map((row) => row.textContent)).toEqual(["TF_VAR_TESTAdded 19 Jul 2026"]);
    expect(screen.getByRole("button", { name: "Add secret" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add variable" })).toBeNull();

    await user.click(variablesOption);

    expect(variablesOption.getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByTestId("environment-secrets").hidden).toBe(true);
    const rows = within(card().getByRole("list")).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual(["AWS_REGIONeu-west-1Added 19 Jul 2026"]);
    expect(within(rows[0]).getByText("eu-west-1").getAttribute("title")).toBe("eu-west-1");
    expect(screen.getByRole("button", { name: "Add variable" })).toBeTruthy();
    expect(variables().getByText("Set as environment variables when Terraform runs. Values are shown here, so keep credentials in Secrets.")).toBeTruthy();
  });

  // A variable can be a multi-line value, a CA certificate say: the list keeps
  // its lines apart instead of running them together.
  it("shows a multi-line plain value with its line breaks", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { ...credential, name: "CA_CERT", sensitive: false, value: "line1\nline2", created_at: "2026-07-19T12:00:00Z" }
    ]);
    renderScreen(queryClient);

    const shown = variables().getByText("line1\nline2", { normalizer: getDefaultNormalizer({ collapseWhitespace: false }) });
    expect(shown.getAttribute("title")).toBe("line1\nline2");
    // white-space: pre keeps each line on its own; truncate's nowrap would
    // fold them into one.
    expect(shown.classList).toContain("whitespace-pre");
    expect(shown.classList).not.toContain("truncate");
  });

  // An entry whose flag is missing is kept on the side that never shows a value.
  it("keeps an entry with no sensitive flag in Secrets", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { id: "credential_1", name: "TF_VAR_TEST", scope: "stack", created_at: "2026-07-19T12:00:00Z" }
    ]);
    renderScreen(queryClient);

    expect(secrets().getByText("TF_VAR_TEST")).toBeTruthy();
    expect(variables().getByText("No variables in this environment")).toBeTruthy();
  });

  it("adds a variable from the Variables tab as not sensitive, typed in plain text", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const added = { ...credential, id: "credential_2", name: "AWS_REGION", sensitive: false, value: "eu-west-1" };
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse(added, 201) : jsonResponse([added])
    );
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.click(screen.getByRole("button", { name: "Variables" }));
    const value = variables().getByLabelText<HTMLTextAreaElement>("Value");
    // A plain box: no mask and no Show value button.
    expect(masking(value)).toBe("");
    expect(variables().queryByRole("button", { name: "Show value" })).toBeNull();
    expect(value.getAttribute("spellcheck")).toBe("false");
    await user.type(variables().getByLabelText("Name"), "AWS_REGION");
    await user.type(value, "eu-west-1");
    await user.click(screen.getByRole("button", { name: "Add variable" }));

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledWith(
      expect.stringContaining("/stacks/stack_1/credentials"),
      expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "AWS_REGION", value: "eu-west-1", sensitive: false }) })
    ));
    expect(await variables().findByText("eu-west-1")).toBeTruthy();
    expect(value.value).toBe("");
  });

  // The row is a form: Enter in the Name box adds, and Show value only
  // toggles. Enter in the Value box is a line break (tested above).
  it("adds on Enter in the Name box, and Show value never submits", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse(credential, 201) : jsonResponse([credential])
    );
    renderScreen(queryClient);
    const user = userEvent.setup();
    const posts = () => fetchSpy.mock.calls.filter(([, init]) => init?.method === "POST").length;

    await user.type(secrets().getByLabelText("Name"), "TF_VAR_TEST");
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");
    await user.type(value, "secret-value");
    const toggle = secrets().getByRole("button", { name: "Show value" });
    await user.click(toggle);
    toggle.focus();
    await user.keyboard("{Enter}");
    expect(posts()).toBe(0);
    expect(value.value).toBe("secret-value");

    await user.type(secrets().getByLabelText("Name"), "{Enter}");
    await waitFor(() => expect(posts()).toBe(1));
    await waitFor(() => expect(value.value).toBe(""));
  });

  // Each tab keeps its own form: a secret half typed never shows up in the
  // Variables tab's plain box, and is still there on coming back.
  it("keeps a half-typed secret in Secrets when switching tabs", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(secrets().getByLabelText("Name"), "GITHUB_TOKEN");
    await user.type(secrets().getByLabelText("Value"), "ghp_secret");
    await user.click(screen.getByRole("button", { name: "Variables" }));

    expect(variables().getByLabelText<HTMLInputElement>("Name").value).toBe("");
    expect(variables().getByLabelText<HTMLTextAreaElement>("Value").value).toBe("");
    // Hidden, the draft still holds off the re-auth redirect.
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();

    await user.click(screen.getByRole("button", { name: "Secrets" }));
    const value = secrets().getByLabelText<HTMLTextAreaElement>("Value");
    expect(value.value).toBe("ghp_secret");
    expect(masking(value)).toBe("disc");
  });
});

// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import CreateStackScreen from "./CreateStackScreen";
import { queryKeys } from "../../api/queryKeys";
import type { Stack } from "../../api/types";

function stack(overrides: Partial<Stack> = {}): Stack {
  return {
    id: "stack_new",
    tenant_id: "tenant_123",
    name: "My Stack",
    slug: "my-stack",
    tags: {},
    default_credential_ids: [],
    created_by: "user_1",
    created_at: "2026-07-20T00:00:00Z",
    effectiveCapabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false },
    ...overrides
  };
}

function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: true, canPublishTemplate: true } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides,
  };
}

function renderScreen(queryClient?: QueryClient) {
  return render(
    <QueryClientProvider client={queryClient ?? testQueryClient()}>
      <AuthContext.Provider value={authValue()}>
        <MemoryRouter initialEntries={["/stacks/new"]}>
          <CreateStackScreen />
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

describe("CreateStackScreen", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it("renders the form with a name input and submit button", () => {
    renderScreen();

    expect(screen.getByLabelText("Name")).toBeTruthy();
    expect(screen.getByRole("button", { name: /create stack/i })).toBeTruthy();
  });

  it("renders a breadcrumb back to the stacks list", () => {
    renderScreen();

    const breadcrumb = screen.getByRole("navigation", { name: "Breadcrumb" });
    const link = within(breadcrumb).getByRole("link", { name: "Stacks" });
    expect(link.getAttribute("href")).toBe("/stacks");
  });

  it("disables the submit button when the name is empty", () => {
    renderScreen();

    const button = screen.getByRole("button", { name: /create stack/i });
    expect((button as HTMLButtonElement).disabled).toBe(true);
  });

  it("enables the submit button when the name is non-empty", () => {
    renderScreen();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    const button = screen.getByRole("button", { name: /create stack/i });
    expect((button as HTMLButtonElement).disabled).toBe(false);
  });

  it("marks itself unsaved once a name is typed, so SessionProvider's proactive re-auth defers", () => {
    renderScreen();

    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  it("creates the stack and navigates to its detail page on success", async () => {
    const created = stack();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(created));

    renderScreen();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    await waitFor(() => {
      // Navigation to new stack detail
      expect(screen.getByTestId("create-stack-success")).toBeTruthy();
    });
  });

  it("shows an error message when the mutation fails", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "conflict", message: "A stack with that slug already exists" }, 409)
    );

    renderScreen();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    await waitFor(() => {
      expect(screen.getByTestId("create-stack-error")).toBeTruthy();
      expect(screen.getByTestId("create-stack-error").textContent).toContain("A stack with that slug already exists");
    });
  });

  it("invalidates the stacks list query on success", async () => {
    const queryClient = testQueryClient();
    const created = stack();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(created));
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    renderScreen(queryClient);

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.stacks("tenant_123") });
    });
  });

  it("shows the busy spinner on the button while submitting", async () => {
    // Never resolve so we stay in pending state
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderScreen();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /creating/i })).toBeTruthy();
    });
  });

  it("does not submit when the name contains only whitespace", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");

    renderScreen();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "   " } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("trims whitespace from the name before submitting", async () => {
    const created = stack({ name: "My Stack" });
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(created));

    renderScreen();

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "  My Stack  " } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    await waitFor(() => {
      const body = JSON.parse(fetchSpy.mock.calls[0]?.[1]?.body as string);
      expect(body.name).toBe("My Stack");
    });
  });

  it("labels the name field with a shadcn Label and Input", () => {
    renderScreen();

    const field = screen.getByLabelText("Name");
    expect(field.getAttribute("data-slot")).toBe("input");
    expect(document.querySelector(`label[for="${field.id}"]`)?.getAttribute("data-slot")).toBe("label");
  });

  it("puts the cursor in the name field", () => {
    renderScreen();

    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
  });

  it("creates the stack from the keyboard", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(stack()));
    renderScreen();

    await userEvent.setup().type(screen.getByLabelText("Name"), "My Stack{Enter}");

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    expect(JSON.parse(fetchSpy.mock.calls[0]?.[1]?.body as string).name).toBe("My Stack");
  });

  // A slow answer must not turn a second Enter into a second stack.
  it("creates one stack when Enter is pressed again while the first is in flight", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
    renderScreen();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Name"), "My Stack{Enter}");
    await screen.findByRole("button", { name: /creating/i });
    await user.type(screen.getByLabelText("Name"), "{Enter}");

    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  // role="alert" is how a screen reader hears the failure. Alert sets it, so
  // the screen must not add a second one.
  it("announces a failed create once", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "conflict", message: "A stack with that slug already exists" }, 409)
    );
    renderScreen();

    await userEvent.setup().type(screen.getByLabelText("Name"), "My Stack{Enter}");

    await screen.findByTestId("create-stack-error");
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0].textContent).toContain("A stack with that slug already exists");
  });

  // --legacy-touch-target gave every control 44px on a touch screen.
  it("gives every control a 44px target on coarse pointers", () => {
    renderScreen();

    expect(screen.getByLabelText("Name").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: /create stack/i }).classList).toContain("pointer-coarse:h-11");
  });

  it("sits in a shadcn Card and sets its own text colour", () => {
    renderScreen();

    const form = screen.getByLabelText("Name").closest("form") as HTMLElement;
    expect(form.closest('[data-slot="card"]')).not.toBeNull();
    expect(form.closest("[data-unsaved], section")?.classList).toContain("text-foreground");
  });

  it("shows an inline error message for a handled API error status", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "unavailable", message: "service unavailable" }, 503)
    );

    renderScreen();

    // The mutation error alone won't trigger the boundary since
    // useQueryErrorBoundary is for query errors, not mutation errors.
    // This test verifies the component handles mutation errors inline.
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "My Stack" } });
    fireEvent.click(screen.getByRole("button", { name: /create stack/i }));

    await waitFor(() => {
      expect(screen.getByTestId("create-stack-error")).toBeTruthy();
    });
  });
});

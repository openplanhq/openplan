// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { AuthContext } from "../auth/AuthContext";
import type { AuthContextValue } from "../auth/AuthContext";

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: {
      sub: "user_1",
      tenant_id: "tenant_123",
      display_name: "Otto Operator",
      global_capabilities: { can_create_stack: true, can_publish_template: true, can_manage_platform_access: false },
    },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides,
  };
}

function TestAuthWrapper({ value = authValue(), children }: { value?: AuthContextValue; children: ReactNode }) {
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

async function shellRouter() {
  const { default: AppShell } = await import("./AppShell");
  return createMemoryRouter(
    [
      {
        path: "/",
        element: <AppShell />,
        children: [{ index: true, element: <div data-testid="outlet-content">child</div> }]
      }
    ],
    { initialEntries: ["/"] }
  );
}

async function shellMarkup() {
  const router = await shellRouter();
  return renderToStaticMarkup(
    <TestAuthWrapper>
      <RouterProvider router={router} />
    </TestAuthWrapper>
  );
}

async function renderShell(value = authValue()) {
  const router = await shellRouter();
  return render(
    <TestAuthWrapper value={value}>
      <RouterProvider router={router} />
    </TestAuthWrapper>
  );
}

describe("AppShell", () => {
  afterEach(() => {
    cleanup();
  });

  // openplan UI's TopBar marks the section you are in, below it too.
  it("marks the current section in the nav", async () => {
    const { default: AppShell } = await import("./AppShell");
    const router = createMemoryRouter([{ path: "/", element: <AppShell />, children: [{ path: "stacks/*", element: <div /> }] }], {
      initialEntries: ["/stacks/stack_1/templates"]
    });
    render(
      <TestAuthWrapper>
        <RouterProvider router={router} />
      </TestAuthWrapper>
    );

    const nav = within(screen.getByRole("navigation", { name: "Primary" }));
    const stacks = nav.getByRole("link", { name: "Stacks" });
    expect(stacks.getAttribute("aria-current")).toBe("page");
    expect(stacks.classList).toContain("bg-muted");
    expect(stacks.classList).toContain("text-foreground");
    const templates = nav.getByRole("link", { name: "Templates" });
    expect(templates.getAttribute("aria-current")).toBeNull();
    expect(templates.classList).toContain("text-muted-foreground");
  });

  it("offers Access only to people who manage platform access", async () => {
    await renderShell();
    expect(within(screen.getByRole("navigation", { name: "Primary" })).queryByRole("link", { name: "Access" })).toBeNull();
    cleanup();

    const admin = authValue();
    admin.me!.global_capabilities.can_manage_platform_access = true;
    await renderShell(admin);
    expect(within(screen.getByRole("navigation", { name: "Primary" })).getByRole("link", { name: "Access" }).getAttribute("href")).toBe("/access");
  });

  it("renders nav, an identity slot, and routed content, with no tenant in the header", async () => {
    await renderShell();
    expect(within(screen.getByRole("banner")).queryByText(/tenant/i)).toBeNull();

    const markup = await shellMarkup();

    expect(markup).toContain('href="/stacks"');
    expect(markup).toContain('href="/templates"');
    expect(markup).toContain('data-testid="identity-menu"');
    expect(markup).not.toContain("<input");
    expect(markup).toContain('data-testid="outlet-content"');
  });

  it("displays the user's display name and a logout control", async () => {
    const markup = await shellMarkup();

    expect(markup).toContain("Otto Operator");
    expect(markup).toContain('data-testid="logout-button"');
  });

  it("renders the debug IDs panel when in dev mode", async () => {
    const markup = await shellMarkup();

    expect(markup).toContain('data-testid="debug-panel"');
    expect(markup).toContain("IDs (debug)");
    expect(markup).toContain('data-testid="debug-user-sub"');
    expect(markup).toContain("user_1");
    expect(markup).toContain('data-testid="debug-tenant"');
    expect(markup).toContain("tenant_123");
  });

  it("logs out from the identity menu", async () => {
    const logout = vi.fn();
    await renderShell(authValue({ logout }));

    await userEvent.click(screen.getByRole("button", { name: "Log out" }));
    expect(logout).toHaveBeenCalledTimes(1);
  });

  it("shows a loading line, and no logout control, while the session resolves", async () => {
    await renderShell(authValue({ me: null, status: "loading" }));

    expect(screen.getByTestId("identity-loading").textContent).toBe("Loading...");
    expect(screen.queryByRole("button", { name: "Log out" })).toBeNull();
  });

  it("skips straight to the page content", async () => {
    await renderShell();

    expect(screen.getByRole("link", { name: "Skip to content" }).getAttribute("href")).toBe("#main-content");
    const main = screen.getByRole("main");
    expect(main.id).toBe("main-content");
    expect(main.getAttribute("tabindex")).toBe("-1");
  });

  // Every control takes a 44px target on a coarse pointer.
  it("gives its controls a 44px target on coarse pointers", async () => {
    await renderShell();

    const controls = [
      screen.getByRole("button", { name: "Log out" }),
      ...within(screen.getByRole("navigation", { name: "Primary" })).getAllByRole("link")
    ];
    for (const control of controls) {
      expect(control.classList).toContain("pointer-coarse:h-11");
    }
  });

  // Some IdPs make the email the display name. It must wrap inside the
  // header instead of pushing the page sideways.
  it("lets a long display name wrap", async () => {
    await renderShell();

    expect(screen.getByTestId("identity-display-name").classList).toContain("wrap-anywhere");
  });
});

// The header is sticky and opaque. Whatever opens over scrolled content must
// paint above it: the Combobox and Select popups, which Base UI portals to the
// body, and the access screen's role list, which does too. Update this test when an overlay
// is added or moves.
describe("AppShell layering", () => {
  const read = (path: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), path), "utf8");
  // The bare z-N, not a variant's such as md:z-10.
  const zIndex = (classes: string) => Number(classes.match(/(?:^|\s)z-(\d+)(?=\s|$)/)?.[1]);
  const positioner = (path: string, primitive: string) =>
    read(path).match(new RegExp(`<${primitive}\\.Positioner[^>]*?className="([^"]*)"`))?.[1] ?? "";

  it("keeps the sticky header below the overlays that open over it", () => {
    const header = zIndex(read("AppShell.tsx").match(/<header className="([^"]*)"/)?.[1] ?? "");
    const overlays = {
      "Combobox popup": zIndex(positioner("../components/ui/combobox.tsx", "ComboboxPrimitive")),
      "Select popup": zIndex(positioner("../components/ui/select.tsx", "SelectPrimitive")),
      "RoleSelect popup": zIndex(positioner("../features/access/RoleSelect.tsx", "Select"))
    };

    expect(header).toBeGreaterThan(0);
    for (const [overlay, z] of Object.entries(overlays)) {
      expect(z, overlay).toBeGreaterThan(header);
    }
  });
});

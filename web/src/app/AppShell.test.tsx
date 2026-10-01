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
      tenantID: "tenant_123",
      displayName: "Otto Operator",
      globalCapabilities: { isPlatformAdmin: false, canCreateStack: true, canPublishTemplate: true },
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
  vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
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
    vi.unstubAllEnvs();
  });

  it("renders nav, an identity slot, a static tenant indicator, and routed content", async () => {
    const markup = await shellMarkup();

    expect(markup).toContain('href="/stacks"');
    expect(markup).toContain('href="/templates"');
    expect(markup).toContain('data-testid="identity-menu"');
    expect(markup).toContain('data-testid="shell-tenant-context"');
    expect(markup).toContain(">tenant_123<");
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

  // --legacy-touch-target gave these controls 44px on touch screens.
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

  // config.ts allows a 128-character tenant ID, and some IdPs make the email
  // the display name. Each must wrap inside the header instead of pushing the
  // page sideways on a phone.
  it("lets a long tenant ID or display name wrap", async () => {
    await renderShell();

    expect(screen.getByTestId("shell-tenant-context").classList).toContain("wrap-anywhere");
    expect(screen.getByTestId("identity-display-name").classList).toContain("wrap-anywhere");
  });

  // Tailwind's hover: applies only where the device can hover, but the legacy
  // a:hover underline also matches a link just tapped on a touch screen.
  it("keeps the header links undecorated on touch screens too", async () => {
    await renderShell();

    const links = [
      screen.getByRole("link", { name: "Skip to content" }),
      ...within(screen.getByRole("navigation", { name: "Primary" })).getAllByRole("link")
    ];
    for (const link of links) {
      expect(link.classList).toContain("no-underline");
      expect(link.classList).not.toContain("hover:no-underline");
    }
  });
});

// The header is sticky and opaque. Whatever opens over scrolled content must
// paint above it: the Combobox and Select popups, which Base UI portals to the
// body, and the undo banner on stack access. Update this test when an overlay
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
      "undo banner": zIndex(
        read("../features/stacks/StackAccessScreen.tsx").match(/<div\s+role="status"\s+className="([^"]*)"/)?.[1] ?? ""
      )
    };

    expect(header).toBeGreaterThan(0);
    for (const [overlay, z] of Object.entries(overlays)) {
      expect(z, overlay).toBeGreaterThan(header);
    }
  });
});

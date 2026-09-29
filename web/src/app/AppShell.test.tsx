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

  // config.ts allows a 128-character tenant ID. It must wrap inside the header
  // instead of pushing the page sideways on a phone.
  it("lets a long tenant ID wrap", async () => {
    await renderShell();

    expect(screen.getByTestId("shell-tenant-context").classList).toContain("wrap-anywhere");
  });
});

// The header is sticky and opaque. Legacy overlays that open over scrolled
// content must paint above it until they migrate: .search-dropdown on stack
// access (PR 5) and .undo-banner. Update this test when either leaves
// features.css.
describe("AppShell layering", () => {
  const read = (path: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), path), "utf8");
  const zIndexOf = (selector: string) =>
    Number(read("../styles/features.css").match(new RegExp(`\\${selector} \\{[^}]*z-index: (\\d+);`))?.[1]);

  it("keeps the sticky header below the legacy overlays", () => {
    const header = Number(read("AppShell.tsx").match(/<header className="[^"]*\bz-(\d+)\b/)?.[1]);

    expect(header).toBeGreaterThan(0);
    expect(header).toBeLessThan(zIndexOf(".search-dropdown"));
    expect(header).toBeLessThan(zIndexOf(".undo-banner"));
  });
});

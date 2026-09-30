// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import { RouteTab, RouteTabs } from "./RouteTabs";

afterEach(cleanup);

// The value the route selects, as a screen derives it from its pathname.
const SECTIONS: Record<string, string> = { "/s": "overview", "/s/templates": "templates", "/s/access": "access" };

function Screen() {
  const { pathname } = useLocation();
  return (
    <>
      <RouteTabs value={SECTIONS[pathname] ?? null} label="Stack sections">
        <RouteTab value="overview" to="/s" end>
          Overview
        </RouteTab>
        <RouteTab value="templates" to="/s/templates">
          Templates
        </RouteTab>
        <RouteTab value="access" to="/s/access">
          Access
        </RouteTab>
      </RouteTabs>
      <p data-testid="pathname">{pathname}</p>
    </>
  );
}

function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="*" element={<Screen />} />
      </Routes>
    </MemoryRouter>
  );
  return userEvent.setup();
}

const pathname = () => screen.getByTestId("pathname").textContent;

describe("RouteTabs", () => {
  it("is a tab list named by its label, with a link for each tab", () => {
    renderAt("/s/templates");
    const list = screen.getByRole("tablist", { name: "Stack sections" });
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((tab) => tab.textContent)).toEqual(["Overview", "Templates", "Access"]);
    expect(tabs.map((tab) => tab.getAttribute("href"))).toEqual(["/s", "/s/templates", "/s/access"]);
    expect(tabs.every((tab) => list.contains(tab))).toBe(true);
  });

  // The route picks the tab, and the link agrees with it.
  it("selects the route's tab and marks its link as the current page", () => {
    renderAt("/s/templates");
    const templates = screen.getByRole("tab", { name: "Templates" });
    expect(templates.getAttribute("aria-selected")).toBe("true");
    expect(templates.getAttribute("aria-current")).toBe("page");
    for (const name of ["Overview", "Access"]) {
      expect(screen.getByRole("tab", { name }).getAttribute("aria-selected")).toBe("false");
    }
  });

  it("follows a clicked tab to its route, and the selection goes with it", async () => {
    const user = renderAt("/s/templates");
    await user.click(screen.getByRole("tab", { name: "Access" }));
    expect(pathname()).toBe("/s/access");
    expect(screen.getByRole("tab", { name: "Access" }).getAttribute("aria-selected")).toBe("true");
  });

  // The tab pattern: one Tab stop for the row, arrows between tabs, Enter to open.
  it("takes one Tab stop, moves with the arrow keys, and opens the focused tab with Enter", async () => {
    const user = renderAt("/s/templates");
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Templates" }));

    await user.keyboard("{ArrowRight}");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Access" }));
    expect(pathname()).toBe("/s/templates");

    await user.keyboard("{Enter}");
    expect(pathname()).toBe("/s/access");
  });

  it("selects nothing on a route that belongs to no tab", () => {
    renderAt("/s/elsewhere");
    expect(screen.getAllByRole("tab").map((tab) => tab.getAttribute("aria-selected"))).toEqual(["false", "false", "false"]);
  });

  // --legacy-touch-target gave every tab 44px on a touch screen. The list
  // drops its fixed height there so the taller tabs fit inside it.
  it("gives every tab a 44px target on coarse pointers", () => {
    renderAt("/s");
    expect(screen.getByRole("tablist").classList).toContain("pointer-coarse:h-auto");
    for (const tab of screen.getAllByRole("tab")) {
      expect(tab.classList).toContain("pointer-coarse:h-11");
    }
  });

  // base.css underlines every bare <a> on hover until PR 9.
  it("keeps the legacy link underline off its tabs", () => {
    renderAt("/s");
    for (const tab of screen.getAllByRole("tab")) {
      expect(tab.classList).toContain("no-underline");
    }
  });
});

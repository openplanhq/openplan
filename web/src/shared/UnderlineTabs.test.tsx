// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import UnderlineTabs from "./UnderlineTabs";

afterEach(cleanup);

describe("UnderlineTabs", () => {
  it("draws its tabs as links in a named nav, the current one marked, with optional counts", () => {
    render(
      <MemoryRouter>
        <UnderlineTabs
          label="Template sections"
          tabs={[
            { to: "/templates/tpl_1/variables", label: "Variables", current: true, count: 5 },
            { to: "/templates/tpl_1/revisions", label: "Revisions", current: false }
          ]}
        />
      </MemoryRouter>
    );
    const nav = screen.getByRole("navigation", { name: "Template sections" });
    const variables = within(nav).getByRole("link", { name: "Variables 5" });
    expect(variables.getAttribute("href")).toBe("/templates/tpl_1/variables");
    expect(variables.getAttribute("aria-current")).toBe("page");
    expect(variables.className).toContain("border-primary");

    const revisions = within(nav).getByRole("link", { name: "Revisions" });
    expect(revisions.getAttribute("aria-current")).toBeNull();
    expect(revisions.className).toContain("border-transparent");
  });
});

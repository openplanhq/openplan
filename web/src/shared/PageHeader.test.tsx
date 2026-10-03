// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import PageHeader from "./PageHeader";

function renderHeader(props: Parameters<typeof PageHeader>[0]) {
  return render(
    <MemoryRouter>
      <PageHeader {...props} />
    </MemoryRouter>
  );
}

describe("PageHeader", () => {
  afterEach(cleanup);

  it("sets the title in openplan UI's page-title style", () => {
    renderHeader({ title: "Stacks" });

    const title = screen.getByRole("heading", { level: 1, name: "Stacks" }).classList;
    for (const name of ["text-page-title", "font-semibold", "tracking-title"]) {
      expect(title).toContain(name);
    }
  });

  it("shows the count in the neutral count pill", () => {
    renderHeader({ title: "Stacks", count: 6 });

    const pill = screen.getByTestId("page-count");
    expect(pill.textContent).toBe("6");
    expect(pill.classList).toContain("bg-muted-strong");
    expect(pill.classList).not.toContain("bg-muted");
  });

  it("ends its trail with the page itself, as plain current text", () => {
    renderHeader({ title: "Needs attention", trail: [{ label: "Stacks", to: "/stacks" }] });

    const trail = within(screen.getByRole("navigation", { name: "Breadcrumb" }));
    expect(trail.getByRole("link", { name: "Stacks" }).getAttribute("href")).toBe("/stacks");
    const current = trail.getByText("Needs attention");
    expect(current.getAttribute("aria-current")).toBe("page");
    expect(current.getAttribute("role")).toBeNull();
    expect(trail.getAllByRole("link")).toHaveLength(1);
  });

  it("draws no trail without one", () => {
    renderHeader({ title: "Stacks" });

    expect(screen.queryByRole("navigation", { name: "Breadcrumb" })).toBeNull();
  });
});

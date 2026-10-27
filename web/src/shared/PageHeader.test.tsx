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
    for (const name of ["text-page-title"]) {
      expect(title).toContain(name);
    }
  });

  // A title is the word alone: no count pill beside it (owner's rule, #518).
  it("draws no count beside the title", () => {
    renderHeader({ title: "Stacks" });

    expect(screen.queryByTestId("page-count")).toBeNull();
    expect(screen.getByRole("heading", { level: 1 }).parentElement?.textContent).not.toMatch(/\d/);
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

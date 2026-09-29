// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import Breadcrumb from "./Breadcrumb";
import type { Crumb } from "./Breadcrumb";

afterEach(cleanup);

function renderCrumbs(items: Crumb[], props: { detail?: string; className?: string } = {}) {
  render(
    <MemoryRouter>
      <Breadcrumb items={items} {...props} />
    </MemoryRouter>
  );
  return screen.getByRole("navigation", { name: "Breadcrumb" });
}

describe("Breadcrumb", () => {
  it("links each ancestor and makes the current page the h1", () => {
    const nav = renderCrumbs([{ label: "Templates", to: "/templates" }, { label: "vpc" }], { detail: "acme/vpc · main" });

    expect(within(nav).getByRole("link", { name: "Templates" }).getAttribute("href")).toBe("/templates");
    const heading = within(nav).getByRole("heading", { level: 1 });
    expect(heading.textContent).toBe("vpc");
    expect(heading.getAttribute("aria-current")).toBe("page");
    expect(within(nav).getByText("acme/vpc · main")).toBeTruthy();
  });

  // Screen readers announce the list's length; the chevrons between crumbs
  // must not count toward it.
  it("hides the separators from assistive tech", () => {
    const nav = renderCrumbs([{ label: "Stacks", to: "/stacks" }, { label: "payments", to: "/stacks/s1" }, { label: "Run #4" }]);

    const separators = nav.querySelectorAll('[data-slot="breadcrumb-separator"]');
    expect(separators).toHaveLength(2);
    separators.forEach((separator) => expect(separator.getAttribute("aria-hidden")).toBe("true"));
    expect(within(nav).getAllByRole("listitem")).toHaveLength(3);
  });

  it("shows an ancestor without a path as text", () => {
    const nav = renderCrumbs([{ label: "Archive" }, { label: "vpc" }]);

    expect(within(nav).queryByRole("link")).toBeNull();
    expect(within(nav).getByText("Archive")).toBeTruthy();
  });

  // A header row sets the breadcrumb beside a button and spaces the row itself.
  it("spaces the page below it unless the caller replaces the spacing", () => {
    expect(renderCrumbs([{ label: "Stacks" }]).classList).toContain("mb-6");
    cleanup();
    const nav = renderCrumbs([{ label: "Stacks" }], { className: "mb-0" });
    expect(nav.classList).toContain("mb-0");
    expect(nav.classList).not.toContain("mb-6");
  });

  it("renders the detail slot only when there is a detail", () => {
    expect(renderCrumbs([{ label: "vpc" }]).querySelector('[data-slot="breadcrumb-detail"]')).toBeNull();
    cleanup();
    const nav = renderCrumbs([{ label: "vpc" }], { detail: "acme/vpc · main" });
    expect(nav.querySelector('[data-slot="breadcrumb-detail"]')?.textContent).toBe("acme/vpc · main");
  });

  // Stack and template names are user-chosen and can be long.
  it("lets a long page name wrap rather than widen the page", () => {
    const nav = renderCrumbs([{ label: "a".repeat(120) }]);
    expect(within(nav).getByRole("heading", { level: 1 }).classList).toContain("wrap-anywhere");
  });
});

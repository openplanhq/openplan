// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import StyleGuide from "./StyleGuide";

afterEach(cleanup);

describe("StyleGuide", () => {
  it("renders without a backend, an auth provider, or a router", () => {
    // The whole point of this page is that it mounts standalone: the app
    // itself cannot render without an identity provider, so anything this gallery needed
    // from context would make it useless for viewing the design system.
    render(<StyleGuide />);
    expect(screen.getByTestId("styleguide")).toBeTruthy();
  });

  it("renders the real status tones rather than copies of their markup", () => {
    render(<StyleGuide />);
    // StatusBadge marks its tone. If the gallery drew a copy of the markup
    // instead, this would break, which is the point: the gallery must not
    // drift from the components.
    const section = screen.getByTestId("sg-theme");
    for (const tone of ["settled", "progress", "waiting", "failed", "canceled"]) {
      expect(section.querySelector(`[data-tone="${tone}"]`), `missing tone ${tone}`).toBeTruthy();
    }
  });

  it("shows every role badge", () => {
    render(<StyleGuide />);
    const section = screen.getByTestId("sg-theme");
    for (const role of ["owner", "operator", "approver", "viewer"]) {
      expect(section.querySelector(`[data-role="${role}"]`), `missing role ${role}`).toBeTruthy();
    }
  });

  it("shows every Badge variant", () => {
    render(<StyleGuide />);
    const section = screen.getByTestId("sg-theme");
    for (const variant of ["default", "secondary", "outline", "destructive", "success", "progress", "warning", "muted"]) {
      expect(section.querySelector(`[data-slot="badge"][data-variant="${variant}"]`), `missing ${variant}`).toBeTruthy();
    }
  });

  it("renders the real Breadcrumb without a router around the page", () => {
    render(<StyleGuide />);
    const nav = within(screen.getByTestId("sg-theme")).getByRole("navigation", { name: "Breadcrumb" });
    expect(within(nav).getByRole("heading", { level: 1 }).textContent).toBe("Run #4");
  });

  it("renders the real LogSteps in the shadcn section", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    expect(section.getByRole("button", { name: "plan" }).getAttribute("aria-expanded")).toBe("true");
  });

  it("shows every shadcn Button variant", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    for (const name of ["Default", "Outline", "Secondary", "Ghost", "Destructive", "Link"]) {
      expect(section.getByRole("button", { name })).toBeTruthy();
    }
  });

  it("shows the components PR 3 added", () => {
    render(<StyleGuide />);
    const section = screen.getByTestId("sg-theme");
    for (const slot of ["card", "alert", "input", "label", "empty"]) {
      expect(section.querySelector(`[data-slot="${slot}"]`), `missing ${slot}`).toBeTruthy();
    }
    // The Label is wired to its Input, as every form on the app must be.
    expect(within(section).getByLabelText("Stack name").getAttribute("data-slot")).toBe("input");
  });

  it("shows the components PR 4 added", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    const table = section.getByRole("table");
    expect(table.getAttribute("data-slot")).toBe("table");
    expect(table.classList).toContain("table-fixed");
    expect(table.querySelector("colgroup")).not.toBeNull();
    expect(section.getByRole("tablist").getAttribute("data-variant")).toBe("line");
    expect(section.getByRole("tab", { name: "Templates" }).getAttribute("aria-selected")).toBe("true");
  });

  it("shows the components PR 5 added", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    const combobox = section.getByRole("combobox", { name: "Combobox specimen" });
    expect(combobox.getAttribute("aria-expanded")).toBe("false");
    expect(combobox.closest('[data-slot="input-group"]')).not.toBeNull();
    const select = section.getByRole("combobox", { name: "Default role" });
    expect(select.getAttribute("data-slot")).toBe("select-trigger");
    expect(select.querySelector('[data-slot="select-value"]')?.textContent).toBe("Viewer");
    const field = section.getByRole("textbox", { name: "Input group specimen" });
    expect(field.closest('[data-slot="input-group"]')).not.toBeNull();
    expect(section.getByLabelText("Description").getAttribute("data-slot")).toBe("textarea");
  });

  it("renders the real RouteMessage", () => {
    render(<StyleGuide />);
    const message = within(screen.getByTestId("sg-theme")).getByTestId("sg-route-message");
    expect(message.getAttribute("data-slot")).toBe("empty");
  });

  // The showcase is left only to the stacks and registry empty states. The
  // service-unavailable screen it used to show is a RouteMessage now.
  it("shows the showcase with copy from a screen that still uses it", () => {
    render(<StyleGuide />);
    expect(screen.getByText("No templates yet")).toBeTruthy();
    expect(screen.queryByText("Authorization service unavailable")).toBeNull();
  });

  // base.css sizes every bare h1 at 40px, and the intro's is the one bare h1
  // left, so the gallery sizes it down on a phone itself.
  it("sizes its intro heading down on phones", () => {
    const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), "styleguide.css"), "utf8");
    expect(css).toMatch(/@media \(max-width: 760px\) \{\s*\.sg__intro h1 \{\s*font-size: var\(--legacy-text-2xl\);/);
  });

  it("swatches every theme colour", () => {
    const { container } = render(<StyleGuide />);
    for (const name of ["background", "foreground", "primary", "secondary", "muted", "accent", "destructive", "success", "warning", "border"]) {
      expect(container.querySelector(`[data-swatch="${name}"]`), `missing --${name}`).toBeTruthy();
    }
  });

  it("links to every section it documents", () => {
    const { container } = render(<StyleGuide />);
    const links = Array.from(container.querySelectorAll(".sg__nav a")).map((a) => a.getAttribute("href"));
    expect(links.length).toBeGreaterThan(0);
    for (const href of links) {
      const id = href?.replace("#", "") ?? "";
      expect(container.querySelector(`#${id}`), `nav links to missing section #${id}`).toBeTruthy();
    }
  });
});

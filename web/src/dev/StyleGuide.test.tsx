// @vitest-environment jsdom
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

  it("shows every shadcn Button variant", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    for (const name of ["Default", "Outline", "Secondary", "Ghost", "Destructive", "Link"]) {
      expect(section.getByRole("button", { name })).toBeTruthy();
    }
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

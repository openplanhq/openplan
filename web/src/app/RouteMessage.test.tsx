// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { SearchX } from "lucide-react";
import { afterEach, describe, expect, it } from "vitest";
import RouteMessage from "./RouteMessage";

afterEach(cleanup);

function renderMessage(action?: React.ReactNode) {
  return render(
    <RouteMessage
      icon={SearchX}
      title="Page not found"
      description="The page you were looking for doesn't exist."
      testId="route-not-found"
      action={action}
    />
  );
}

describe("RouteMessage", () => {
  it("renders the title as its one heading, an h1", () => {
    renderMessage();
    const headings = screen.getAllByRole("heading");
    expect(headings).toHaveLength(1);
    expect(headings[0].tagName).toBe("H1");
    expect(headings[0].textContent).toBe("Page not found");
  });

  it("explains the title below it", () => {
    renderMessage();
    expect(screen.getByText("The page you were looking for doesn't exist.")).toBeTruthy();
  });

  it("is shadcn's Empty, carrying the screen's test id", () => {
    renderMessage();
    expect(screen.getByTestId("route-not-found").getAttribute("data-slot")).toBe("empty");
  });

  // A page-level EmptyState draws its icon plain in muted, not on shadcn's tile.
  it("draws the icon plain, not on a tile", () => {
    const { container } = renderMessage();
    expect(container.querySelector('[data-slot="empty-icon"]')?.getAttribute("data-variant")).toBe("default");
    expect(container.querySelector("svg")?.classList).toContain("text-muted-foreground");
  });

  it("renders the action after the message", () => {
    renderMessage(<a href="/stacks">Back to stacks</a>);
    const link = screen.getByRole("link", { name: "Back to stacks" });
    const description = screen.getByText("The page you were looking for doesn't exist.");
    expect(description.compareDocumentPosition(link) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("offers nothing when no action is given", () => {
    renderMessage();
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("hides the icon from assistive technology", () => {
    const { container } = renderMessage();
    expect(container.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });

  // Preflight leaves a heading with the body's type, so the h1 sets its own.
  it("sets its own type", () => {
    renderMessage();
    const heading = screen.getByRole("heading", { level: 1 }).classList;
    for (const name of ["font-heading", "text-title"]) {
      expect(heading).toContain(name);
    }
  });
});

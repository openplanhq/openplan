// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import NotFound from "./NotFound";

afterEach(cleanup);

// Inside a router: the page's way back is a Link.
function renderNotFound() {
  return render(
    <MemoryRouter initialEntries={["/nope"]}>
      <NotFound />
    </MemoryRouter>
  );
}

describe("NotFound", () => {
  it("renders a 404 message", () => {
    renderNotFound();

    expect(screen.getByTestId("route-not-found").getAttribute("data-slot")).toBe("empty");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Page not found");
  });

  it("offers a way back to the stacks index", () => {
    renderNotFound();

    expect(screen.getByRole("link", { name: "Back to stacks" }).getAttribute("href")).toBe("/stacks");
  });
});

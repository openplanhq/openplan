// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import AccessDenied from "./AccessDenied";

afterEach(cleanup);

// Inside a router: the page's way back is a Link.
function renderAccessDenied() {
  return render(
    <MemoryRouter initialEntries={["/stacks/stack_1/access"]}>
      <AccessDenied />
    </MemoryRouter>
  );
}

describe("AccessDenied", () => {
  it("renders a not-permitted message", () => {
    renderAccessDenied();

    expect(screen.getByTestId("route-access-denied").getAttribute("data-slot")).toBe("empty");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Not permitted");
  });

  it("offers a way back to the stacks index", () => {
    renderAccessDenied();

    expect(screen.getByRole("link", { name: "Back to stacks" }).getAttribute("href")).toBe("/stacks");
  });
});

// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import ServiceUnavailable from "./ServiceUnavailable";

const reload = vi.fn();

beforeEach(() => {
  // jsdom leaves location.reload unimplemented; the stub records the call.
  vi.stubGlobal("location", { reload });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  reload.mockReset();
});

describe("ServiceUnavailable", () => {
  // A 503 is missing server setup (an encryption key, a queue), which only
  // an operator can fix, so the page sends the person to one.
  it("renders the service-unavailable banner", () => {
    render(<ServiceUnavailable />);

    expect(screen.getByTestId("route-service-unavailable").getAttribute("data-slot")).toBe("empty");
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Service unavailable");
    expect(screen.getByText("openplan can't do this right now. Contact your operator.")).toBeTruthy();
  });

  // The setup may be fixed by the time the person reads this, so Retry
  // reloads rather than sending them anywhere.
  it("offers a Retry that reloads the page", async () => {
    render(<ServiceUnavailable />);

    await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }));

    expect(reload).toHaveBeenCalledTimes(1);
  });
});

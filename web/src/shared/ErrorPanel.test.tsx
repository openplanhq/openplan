// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import ErrorPanel from "./ErrorPanel";

afterEach(cleanup);

describe("ErrorPanel", () => {
  it("announces a headline, the next step and the server's words as written", () => {
    const raw = "clone: exit status 128\nfatal: Remote branch nope not found";
    render(
      <ErrorPanel title="Sync failed" details={raw}>
        Check the ref.
      </ErrorPanel>
    );
    const panel = screen.getByRole("alert");
    expect(panel.querySelector("p")?.textContent).toBe("Sync failed");
    expect(panel.textContent).toContain("Check the ref.");
    // Kept as written, line breaks and all.
    expect(panel.querySelector("pre")?.textContent).toBe(raw);
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("keeps collapsed details behind Show details until asked for", () => {
    render(<ErrorPanel title="Sync failed" details="raw detail" detailsCollapsed />);
    const toggle = screen.getByRole("button", { name: "Show details" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("raw detail")).toBeNull();

    fireEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Hide details" }).getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByText("raw detail").id).toBe(toggle.getAttribute("aria-controls"));
  });

  it("draws no details block for empty details", () => {
    render(<ErrorPanel title="Sync failed" details="  " />);
    expect(screen.getByRole("alert").querySelector("pre")).toBeNull();
  });
});

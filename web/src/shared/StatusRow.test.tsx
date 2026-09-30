// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import StatusRow from "./StatusRow";

afterEach(cleanup);

describe("StatusRow", () => {
  it("labels a status and shows it as a badge of its tone", () => {
    render(<StatusRow label="Registration" value="pending_validation" />);
    const row = screen.getByText("Registration").parentElement!;
    expect(row.getAttribute("data-status")).toBe("pending_validation");
    expect(within(row).getByText("pending_validation").getAttribute("data-tone")).toBe("waiting");
  });
});

// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import StatusBadge from "./StatusBadge";
import { statusGlyph } from "./statusTone";
import type { StatusTone } from "./statusTone";

afterEach(cleanup);

describe("StatusBadge", () => {
  // The tone table in docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md.
  it.each([
    ["settled", "success"],
    ["failed", "destructive"],
    ["progress", "progress"],
    ["waiting", "warning"],
    ["canceled", "muted"]
  ] as [StatusTone, string][])("paints the %s tone as the %s variant", (tone, variant) => {
    render(<StatusBadge tone={tone}>some status</StatusBadge>);
    const badge = screen.getByText("some status");
    expect(badge.getAttribute("data-slot")).toBe("badge");
    expect(badge.getAttribute("data-variant")).toBe(variant);
    expect(badge.getAttribute("data-tone")).toBe(tone);
  });

  it("pairs the words with the tone's glyph, hidden from assistive tech", () => {
    render(<StatusBadge tone="failed">failed</StatusBadge>);
    const glyph = screen.getByText(statusGlyph("failed"));
    expect(glyph.getAttribute("aria-hidden")).toBe("true");
    expect(glyph.parentElement).toBe(screen.getByText("failed"));
  });

  it("passes a title and test ID through to the badge", () => {
    render(
      <StatusBadge tone="waiting" title="waiting_approval" data-testid="run-status">
        Awaiting approval
      </StatusBadge>
    );
    const badge = screen.getByTestId("run-status");
    expect(badge.getAttribute("title")).toBe("waiting_approval");
    expect(badge.textContent).toContain("Awaiting approval");
  });
});

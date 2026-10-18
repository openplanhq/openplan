// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { Check, LoaderCircle } from "lucide-react";
import { afterEach, describe, expect, it } from "vitest";
import StatusLabel from "./StatusLabel";

function iconOf(label: string): SVGElement {
  const icon = screen.getByText(label).querySelector("svg");
  expect(icon).not.toBeNull();
  return icon as SVGElement;
}

describe("StatusLabel", () => {
  afterEach(cleanup);

  // A working state's icon standing still reads as a run that is stuck.
  it("turns the icon of a state still at work", () => {
    render(
      <StatusLabel icon={LoaderCircle} tone="settled">
        planning · initializing
      </StatusLabel>
    );
    expect(iconOf("planning · initializing").classList).toContain("animate-spin");
  });

  it("keeps every other state's icon still", () => {
    render(
      <StatusLabel icon={Check} tone="settled">
        applied
      </StatusLabel>
    );
    expect(iconOf("applied").classList).not.toContain("animate-spin");
  });
});

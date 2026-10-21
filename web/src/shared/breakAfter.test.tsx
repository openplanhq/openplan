// @vitest-environment jsdom
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Breakable, piecesAfter } from "./breakAfter";

describe("piecesAfter", () => {
  it("cuts after each separator, keeping it on the piece before", () => {
    expect(piecesAfter("modules/networking/transit-gateway", "/-")).toEqual(["modules/", "networking/", "transit-", "gateway"]);
    expect(piecesAfter("optional(map(string))", "({[,")).toEqual(["optional(", "map(", "string))"]);
  });

  it("leaves a value with no separator, or one ending in one, whole", () => {
    expect(piecesAfter("main", "/-")).toEqual(["main"]);
    expect(piecesAfter("release/", "/-")).toEqual(["release/"]);
  });
});

describe("Breakable", () => {
  it("puts a break opportunity between pieces and keeps the text whole", () => {
    const { container } = render(<Breakable pieces={["map(", "string)"]} />);
    expect(container.textContent).toBe("map(string)");
    expect(container.querySelectorAll("wbr")).toHaveLength(1);
  });
});

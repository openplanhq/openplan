import { describe, expect, it } from "vitest";
import { buttonClass } from "./buttonClass";

const classes = (value: string) => value.split(/\s+/);

describe("buttonClass", () => {
  it("pads 12px at the sides and 10px on an icon's side", () => {
    const primary = classes(buttonClass());
    expect(primary).toEqual(expect.arrayContaining(["px-3", "has-data-[icon=inline-start]:pl-2.5", "has-data-[icon=inline-end]:pr-2.5"]));
    expect(primary).not.toContain("px-2.5");
    expect(primary).not.toContain("has-data-[icon=inline-start]:pl-2");
  });

  it("draws focus as a 2px solid ring outline instead of shadcn's soft ring", () => {
    for (const variant of ["primary", "outline", "section"] as const) {
      const button = classes(buttonClass(variant));
      expect(button).toEqual(expect.arrayContaining(["focus-visible:ring-0", "focus-visible:outline-solid", "focus-visible:outline-2", "focus-visible:outline-ring"]));
      expect(button).not.toContain("focus-visible:ring-3");
      expect(button).not.toContain("focus-visible:border-ring");
    }
  });

  it("is 32px tall by default and 36px at lg", () => {
    expect(classes(buttonClass())).toContain("h-8");
    expect(classes(buttonClass("outline", "lg"))).toContain("h-9");
    expect(classes(buttonClass("outline", "lg"))).not.toContain("h-8");
  });

  it("pads a section button 10px at both sides, in 13px text", () => {
    const section = classes(buttonClass("section"));
    expect(section).toEqual(expect.arrayContaining(["px-2.5", "text-meta", "has-data-[icon=inline-start]:pl-2.5"]));
    expect(section).not.toContain("px-3");
    expect(section).not.toContain("text-sm");
  });
});

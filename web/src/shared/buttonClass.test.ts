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
    for (const variant of ["primary", "outline", "section", "subtle", "icon"] as const) {
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

  it("draws an icon button 32px square in muted text, with no side padding", () => {
    const icon = classes(buttonClass("icon"));
    expect(icon).toEqual(expect.arrayContaining(["size-8", "text-muted-foreground"]));
    expect(icon).not.toContain("px-3");
  });

  it("draws a quiet button as 12px muted text with no padding, fill or weight", () => {
    const quiet = classes(buttonClass("quiet"));
    expect(quiet).toEqual(
      expect.arrayContaining(["text-xs", "text-muted-foreground", "hover:text-foreground", "hover:underline", "font-normal", "px-0", "h-auto"])
    );
    expect(quiet).not.toContain("text-primary");
    expect(quiet).not.toContain("font-medium");
    expect(quiet).not.toContain("px-2");
    expect(quiet).not.toContain("h-6");
  });

  it("draws a subtle button 32px tall in muted text with no border, turning foreground on hover", () => {
    const subtle = classes(buttonClass("subtle"));
    expect(subtle).toEqual(expect.arrayContaining(["h-8", "text-muted-foreground", "hover:text-foreground", "hover:bg-muted", "px-3"]));
    expect(subtle).not.toContain("bg-background");
    expect(subtle).not.toContain("border-border");
  });

  it("draws a small button 28px tall in 13px text, padded 8px on the icon's side and 10px on the other, with 8px corners", () => {
    const small = classes(buttonClass("subtle", "sm"));
    expect(small).toEqual(
      expect.arrayContaining(["h-7", "text-meta", "px-2.5", "has-data-[icon=inline-start]:pl-2", "rounded-md", "gap-1.5", "text-muted-foreground"])
    );
    expect(small).not.toContain("h-8");
    expect(small).not.toContain("px-3");
    expect(small).not.toContain("has-data-[icon=inline-start]:pl-2.5");
    expect(small).not.toContain("rounded-lg");
    expect(small).not.toContain("text-sm");
  });
});

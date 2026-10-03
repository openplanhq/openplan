import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";

describe("cn", () => {
  it("resolves through the @/ alias and lets the later Tailwind class win", () => {
    expect(cn("px-2 text-sm", undefined, "px-4")).toBe("text-sm px-4");
  });

  // theme.css's own steps conflict with Tailwind's like the built-in ones do.
  it.each([
    ["text-sm", "text-meta"],
    ["text-2xl", "text-page-title"],
    ["rounded-xl", "rounded-panel"],
    ["leading-snug", "leading-label"],
    ["tracking-tight", "tracking-title"]
  ])("lets %s give way to %s", (earlier, later) => {
    expect(cn(earlier, later)).toBe(later);
  });

  it("keeps a theme text size apart from a text colour", () => {
    expect(cn("text-meta", "text-muted-foreground")).toBe("text-meta text-muted-foreground");
  });
});

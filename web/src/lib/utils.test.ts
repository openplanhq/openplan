import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";

describe("cn", () => {
  it("resolves through the @/ alias and lets the later Tailwind class win", () => {
    expect(cn("px-2 text-sm", undefined, "px-4")).toBe("text-sm px-4");
  });
});

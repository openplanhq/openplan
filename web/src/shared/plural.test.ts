import { describe, expect, it } from "vitest";
import { pluralCount, pluralWord } from "./plural";

describe("pluralWord", () => {
  it.each([
    [1, "template"],
    [4, "templates"]
  ])("template count word for %i", (n, word) => expect(pluralWord(n, "template", "templates")).toBe(word));
});

describe("pluralCount", () => {
  it("singularises one revision", () => {
    expect(pluralCount(1, "revision", "revisions")).toBe("1 revision");
    expect(pluralCount(4, "revision", "revisions")).toBe("4 revisions");
  });
});

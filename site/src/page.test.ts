import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const SITE = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (path: string) => readFileSync(join(SITE, path), "utf8");

const HTML = read("index.html");
const CSS = read("src/styles.css");
const THEME = read("../web/src/styles/theme.css");

describe("site/index.html", () => {
  it("holds no colour literal", () => {
    for (const source of [HTML, CSS]) {
      expect(source).not.toMatch(/#[0-9a-f]{3,8}\b/i);
      expect(source).not.toMatch(/\b(?:rgba?|hsla?|oklch)\(/i);
    }
    const paint = [...HTML.matchAll(/\b(?:fill|stroke)="([^"]*)"/g)].map((m) => m[1]);
    expect(paint.filter((value) => value !== "none" && value !== "currentColor")).toEqual([]);    // Arbitrary values and inline styles are the other ways a colour gets in.
    expect(HTML).not.toMatch(/-\[(?!\d)[a-z]+\]/i);
    expect(HTML).not.toMatch(/style="[^"]*(?:color|fill|stroke)/i);
    // The stylesheet only imports: anything else in it is a rule the guards above do not read.
    const rules = CSS.replace(/\/\*[\s\S]*?\*\//g, "").split("\n").map((line) => line.trim()).filter(Boolean);
    expect(rules.filter((line) => !line.startsWith("@import "))).toEqual([]);
  });

  it("uses only theme colours that theme.css defines", () => {
    const used = new Set(
      [...HTML.matchAll(/\b(?:bg|text|border|fill|stroke)-([a-z]+(?:-[a-z]+)*)\b/g)]
        .map((m) => m[1])
        .filter((name) => !/^(?:none|sm|lg|xl|base|meta|row-title|page-title|panel-title|t|b|l|r|x|y|width|dasharray|linecap|linejoin)$/.test(name))
    );
    const defined = new Set([...THEME.matchAll(/--color-([a-z-]+):/g)].map((m) => m[1]));
    expect([...used].filter((name) => !defined.has(name))).toEqual([]);
  });

  it("hides every drawing from assistive technology and puts no words in one", () => {
    const svgs = [...HTML.matchAll(/<svg\b[^>]*>/g)].map((m) => m[0]);
    expect(svgs.length).toBeGreaterThan(0);
    expect(svgs.filter((tag) => !tag.includes('aria-hidden="true"'))).toEqual([]);
    expect(HTML).not.toMatch(/<text\b/);
  });

  it("gives every image alternative text", () => {
    const imgs = [...HTML.matchAll(/<img\b[^>]*>/g)].map((m) => m[0]);
    expect(imgs.filter((tag) => !/\balt="[^"]+"/.test(tag))).toEqual([]);
  });

  it("ships no JavaScript", () => {
    expect(HTML).not.toMatch(/<script\b/);
  });

  it("lists the eight features, each drawing with a primary accent", () => {
    const features = HTML.split("<li data-feature").slice(1).map((chunk) => chunk.split("</li>")[0]);
    expect(features).toHaveLength(8);
    for (const feature of features) {
      const drawing = feature.split("<svg")[1]?.split("</svg>")[0] ?? "";
      expect(drawing).not.toBe("");
      expect(drawing).toMatch(/\b(?:stroke|fill)-primary(?![\w-])/);
      expect(feature).toMatch(/<h3\b[^>]*>[^<]+<\/h3>/);
    }
  });

  it("ends on a footer whose painting exists in public/", () => {
    const footer = HTML.split("<footer")[1] ?? "";
    expect(footer).toMatch(/<img\b[^>]*\bsrc="\/footer-train\.jpg"/);
    expect(() => read("public/footer-train.jpg")).not.toThrow();
    expect(footer).toContain("Apache 2.0");
  });
});

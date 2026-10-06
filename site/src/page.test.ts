import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const SITE = join(dirname(fileURLToPath(import.meta.url)), "..");
const read = (path: string) => readFileSync(join(SITE, path), "utf8");

const HTML = read("index.html");
const CSS = read("src/styles.css");
const DRAWINGS = read("src/drawings.css");
const THEME = read("../web/src/styles/theme.css");

const FEATURES = HTML.split("<li data-feature").slice(1).map((chunk) => chunk.split("</li>")[0]);
const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, "");

describe("site/index.html", () => {
  it("holds no colour literal", () => {
    for (const source of [HTML, CSS, DRAWINGS]) {
      expect(source).not.toMatch(/#[0-9a-f]{3,8}\b/i);
      expect(source).not.toMatch(/\b(?:rgba?|hsla?|oklch)\(/i);
    }
    const paint = [...HTML.matchAll(/\b(?:fill|stroke)="([^"]*)"/g)].map((m) => m[1]);
    expect(paint.filter((value) => value !== "none" && value !== "currentColor")).toEqual([]);
    // Arbitrary values and inline styles are the other ways a colour gets in.
    expect(HTML).not.toMatch(/-\[(?!\d)[a-z]+\]/i);
    expect(HTML).not.toMatch(/style="[^"]*(?:color|fill|stroke)/i);
    // styles.css only imports: anything else in it is a rule the guards here do not read.
    const rules = stripComments(CSS).split("\n").map((line) => line.trim()).filter(Boolean);
    expect(rules.filter((line) => !line.startsWith("@import "))).toEqual([]);
  });

  it("names no colour in drawings.css but theme properties and transparent", () => {
    // Every word in a declaration's value must be a known non-colour keyword,
    // so a named colour (red, white, ...) cannot slip in.
    const keywords = new Set([
      "transparent", "none", "important", "infinite", "linear", "ease", "ease-in-out", "ease-out",
      "fill-box", "radial-gradient", "translatex", "translatey", "scale", "scalex", "to", "px", "s"
    ]);
    const values = [...stripComments(DRAWINGS).matchAll(/[\w-]+\s*:\s*([^;{}]+)[;}]/g)]
      .map((m) => m[1].replace(/var\(--[\w-]+\)/g, ""));
    expect(values.length).toBeGreaterThan(0);
    const words = values.flatMap((value) => value.toLowerCase().match(/[a-z][a-z-]*/g) ?? []);
    expect(words.filter((word) => !keywords.has(word) && !word.startsWith("a-"))).toEqual([]);
  });

  it("stops every drawing animation for people who ask for reduced motion", () => {
    const block = stripComments(DRAWINGS).split(/@media\s*\(prefers-reduced-motion:\s*reduce\)/)[1] ?? "";
    expect(block).toMatch(/animation:\s*none/);
  });

  it("uses only theme colours that theme.css defines", () => {
    const used = new Set(
      [...HTML.matchAll(/\b(?:bg|text|border|fill|stroke)-([a-z]+(?:-[a-z]+)*)\b/g)]
        .map((m) => m[1])
        .filter((name) => !/^(?:none|xs|sm|lg|xl|base|meta|row-title|page-title|panel-title|center|t|b|l|r|x|y|width|dasharray|linecap|linejoin)$/.test(name))
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
    expect(FEATURES).toHaveLength(8);
    for (const feature of FEATURES) {
      const drawing = feature.split("<svg")[1]?.split("</svg>")[0] ?? "";
      expect(drawing).not.toBe("");
      expect(drawing).toMatch(/\b(?:stroke|fill)-primary(?![\w-])/);
      expect(feature).toMatch(/<h3\b[^>]*>[^<]+<\/h3>/);
    }
  });

  it("numbers every feature and gives it a detail tag", () => {
    FEATURES.forEach((feature, index) => {
      expect(feature).toContain(`>0${index + 1} / 08<`);
      expect(feature).toMatch(/<span\b[^>]*\bdata-tag\b[^>]*>[^<]+<\/span>/);
    });
  });

  it("closes on a call to action that links to the README's setup steps", () => {
    const main = HTML.split("<main")[1]?.split("</main>")[0] ?? "";
    const cta = main.split("</section>").at(-2) ?? "";
    expect(cta).toContain("docker compose -f docker-compose.release.yaml up -d");
    expect(cta).toMatch(/<a\b[^>]*\bhref="https:\/\/github\.com\/openplanhq\/openplan#running-it-locally"/);
  });

  it("ends on a footer whose painting exists in public/", () => {
    const footer = HTML.split("<footer")[1] ?? "";
    expect(footer).toMatch(/<img\b[^>]*\bsrc="\/footer-train\.jpg"/);
    expect(() => read("public/footer-train.jpg")).not.toThrow();
    expect(footer).toContain("Apache 2.0");
    expect(footer).toContain("Painting by J. M. W. Turner");
  });
});

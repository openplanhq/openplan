import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const STYLES_DIR = dirname(fileURLToPath(import.meta.url));

/** Legacy stylesheets subject to the palette rules. tokens.css defines the
    palette; theme.css is the shadcn theme and has its own guard. */
function convertedStylesheets(): string[] {
  return ["base.css", "features.css", "primitives.css"];
}

function read(name: string): string {
  // Comments may legitimately mention hex values while explaining a decision.
  return readFileSync(join(STYLES_DIR, name), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
}

function readAll(): string {
  return convertedStylesheets().map(read).join("\n");
}

const REQUIRED_TOKENS = [
  "--legacy-color-bg", "--legacy-color-fg", "--legacy-color-muted", "--legacy-color-muted-fg",
  "--legacy-color-border", "--legacy-color-card",
  "--legacy-color-accent", "--legacy-color-accent-2", "--legacy-color-accent-fg", "--legacy-gradient-accent",
  "--legacy-color-accent-soft", "--legacy-color-accent-border",
  "--legacy-color-success", "--legacy-color-warning", "--legacy-color-danger",
  "--legacy-color-success-dot", "--legacy-color-warning-dot",
  "--legacy-font-display", "--legacy-font-body", "--legacy-font-mono",
  "--legacy-radius-sm", "--legacy-radius-md", "--legacy-radius-lg", "--legacy-radius-xl", "--legacy-radius-full",
  "--legacy-shadow-sm", "--legacy-shadow-md", "--legacy-shadow-lg", "--legacy-shadow-xl",
  "--legacy-shadow-accent", "--legacy-shadow-accent-lg", "--legacy-shadow-ring",
  "--legacy-ease-out", "--legacy-duration-fast", "--legacy-duration-lift", "--legacy-duration-entrance"
];

describe("tokens.css", () => {
  it("defines every required custom property", () => {
    const tokens = readFileSync(join(STYLES_DIR, "tokens.css"), "utf8");
    for (const token of REQUIRED_TOKENS) {
      expect(tokens, `missing ${token}`).toContain(`${token}:`);
    }
  });

  it("uses the AA-safe status colours for text", () => {
    const tokens = readFileSync(join(STYLES_DIR, "tokens.css"), "utf8");
    // The text colours must remain darker than the brighter dot/fill variants.
    expect(tokens).toMatch(/--legacy-color-success:\s*#166534/i);
    expect(tokens).toMatch(/--legacy-color-warning:\s*#92400E/i);
    expect(tokens).toMatch(/--legacy-color-danger:\s*#B91C1C/i);
    expect(tokens).toMatch(/--legacy-color-muted-fg:\s*#475569/i);
  });
});

describe.each(convertedStylesheets())("%s", (name) => {
  it("contains no raw hex colors", () => {
    const matches = read(name).match(/#[0-9a-fA-F]{3,8}\b/g) ?? [];
    expect(matches, `use var(--legacy-color-*) instead of ${matches.join(", ")}`).toEqual([]);
  });

  it("uses a radius token for every border-radius", () => {
    // The \s* must live INSIDE the lookahead. Left outside it can backtrack to
    // zero-width, so the lookahead inspects " var(...)" rather than "var(...)",
    // succeeds, and every compliant declaration is reported as a violation.
    const matches =
      read(name).match(/border-radius:(?!\s*var\(--legacy-radius-[a-z]+\)\s*;)[^;]+;/g) ?? [];
    expect(matches, `use var(--legacy-radius-*): ${matches.join(", ")}`).toEqual([]);
  });

  it("uses a shadow token for every box-shadow", () => {
    const matches =
      read(name).match(/box-shadow:(?!\s*var\(--legacy-shadow-[a-z-]+\)\s*;)[^;]+;/g) ?? [];
    expect(matches, `use var(--legacy-shadow-*): ${matches.join(", ")}`).toEqual([]);
  });
});

/** A declaration block: its selector and its properties. Comments are already
    stripped by read(), so a value mentioning px in prose cannot trip these. */
function parseRules(css: string): { selector: string; decl: Record<string, string> }[] {
  const parsed: { selector: string; decl: Record<string, string> }[] = [];
  for (const match of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const selector = match[1].trim().replace(/\s+/g, " ");
    // At-rule preludes (@media, @supports) wrap rules rather than declaring any.
    if (!selector || selector.startsWith("@")) continue;
    const decl: Record<string, string> = {};
    for (const part of match[2].split(";")) {
      const colon = part.indexOf(":");
      if (colon === -1) continue;
      decl[part.slice(0, colon).trim()] = part.slice(colon + 1).trim();
    }
    parsed.push({ selector, decl });
  }
  return parsed;
}

const SCALES: Record<string, Record<number, string>> = {
  "font-size": { 12: "--legacy-text-xs", 14: "--legacy-text-sm", 16: "--legacy-text-base", 18: "--legacy-text-lg",
                 20: "--legacy-text-xl", 24: "--legacy-text-2xl", 32: "--legacy-text-3xl", 40: "--legacy-text-4xl",
                 52: "--legacy-text-5xl", 64: "--legacy-text-6xl", 84: "--legacy-text-7xl" },
  "border-radius": { 4: "--legacy-radius-sm", 6: "--legacy-radius-md", 8: "--legacy-radius-lg",
                     12: "--legacy-radius-xl", 9999: "--legacy-radius-full" },
  spacing: { 4: "--legacy-space-1", 8: "--legacy-space-2", 12: "--legacy-space-3", 16: "--legacy-space-4",
             20: "--legacy-space-5", 24: "--legacy-space-6", 32: "--legacy-space-8", 40: "--legacy-space-10",
             48: "--legacy-space-12", 64: "--legacy-space-16", 96: "--legacy-space-24" }
};

const SIZED_PROPS = ["font-size", "border-radius", "gap", "padding", "margin",
                     "min-height", "min-width",
                     "padding-top", "padding-bottom", "padding-left", "padding-right"];

/** Deliberately off-scale sizes. Each carries its reason so that silencing a
    finding stays a decision rather than a habit. */
const OFF_SCALE_ALLOWED = new Set([
  "body|min-width"                      // minimum supported viewport width, not a spacing value
]);

/** Geist tightens tracking in three tiers as type grows: -0.06em at 40px and
    above, -0.04em from 24 to 32px, -0.02em below that. */
function expectedTracking(px: number): string {
  if (px >= 40) return "--legacy-tracking-tightest";
  if (px >= 24) return "--legacy-tracking-tighter";
  return "--legacy-tracking-tight";
}

describe.each(convertedStylesheets())("%s token discipline", (name) => {
  const parsed = parseRules(read(name));

  it("expresses every size as a token", () => {
    const violations: string[] = [];
    for (const { selector, decl } of parsed) {
      for (const prop of SIZED_PROPS) {
        const value = decl[prop];
        if (!value || value.includes("var(")) continue;
        if (OFF_SCALE_ALLOWED.has(`${selector}|${prop}`)) continue;
        for (const [, num, unit] of value.matchAll(/(-?\d*\.?\d+)(rem|px)/g)) {
          const px = parseFloat(num) * (unit === "rem" ? 16 : 1);
          // 0 resets, 1px hairlines and 2px rings are not scale values.
          if (px === 0 || px === 1 || px === 2) continue;
          const scale = SCALES[prop] ?? SCALES.spacing;
          const token = scale[px];
          violations.push(
            `${selector} { ${prop}: ${value} } -> ${px}px, ` +
              (token ? `use var(${token})` : "off-scale")
          );
        }
      }
    }
    expect(violations, violations.join("\n")).toEqual([]);
  });

  // --legacy-control-height only governs when nothing else sets the height. Add vertical
  // padding and the line box plus padding overshoots it, so the token goes inert
  // and the control silently keeps its old size. This has bitten buttons, inputs,
  // the app header and .runtime-value.
  it("never defeats a height token with vertical padding", () => {
    const violations: string[] = [];
    for (const { selector, decl } of parsed) {
      const heightToken = ["--legacy-control-height", "--legacy-touch-target"].find((t) =>
        decl["min-height"]?.includes(t)
      );
      if (!heightToken) continue;
      const shorthand = decl.padding?.split(/\s+/)[0];
      const vertical = [shorthand, decl["padding-top"], decl["padding-bottom"]].find(
        (v) => v && v !== "0" && v !== "0px"
      );
      if (vertical) {
        violations.push(`${selector} { min-height: var(${heightToken}); padding: ${vertical} ... }`);
      }
    }
    expect(violations, `set vertical padding to 0 so the token governs:\n${violations.join("\n")}`)
      .toEqual([]);
  });

  it("matches letter-spacing to the size tier", () => {
    const violations: string[] = [];
    for (const { selector, decl } of parsed) {
      const size = decl["font-size"];
      const spacing = decl["letter-spacing"];
      if (!size?.includes("--legacy-text-") || !spacing?.includes("--legacy-tracking-tight")) continue;
      const token = size.match(/--legacy-text-[\w-]+/)?.[0];
      const px = Object.entries(SCALES["font-size"]).find(([, t]) => t === token)?.[0];
      if (!px) continue;
      const want = expectedTracking(Number(px));
      const have = spacing.match(/--legacy-tracking-[\w-]+/)?.[0];
      if (have !== want) {
        violations.push(`${selector} { ${token} is ${px}px } has ${have}, wants ${want}`);
      }
    }
    expect(violations, violations.join("\n")).toEqual([]);
  });
});

describe("accessibility fallbacks", () => {
  it("gives the featured panel a forced-colors border fallback", () => {
    const css = readAll();
    expect(css).toMatch(/@media\s*\(forced-colors:\s*active\)/);
    expect(css).toMatch(/\.panel--featured\s*\{[^}]*border-color:\s*CanvasText/);
  });
});

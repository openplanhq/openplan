import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const STYLES_DIR = dirname(fileURLToPath(import.meta.url));
const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, "");
const theme = () => stripComments(readFileSync(join(STYLES_DIR, "theme.css"), "utf8"));

function rootVariables(css: string): Record<string, string> {
  const root = css.match(/:root\s*\{([^}]*)\}/)?.[1] ?? "";
  return Object.fromEntries([...root.matchAll(/--([\w-]+):\s*([^;]+);/g)].map(([, k, v]) => [k, v.trim()]));
}

type Rgb = [number, number, number];

function rgb(value: string): Rgb {
  const match = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(value);
  if (!match) throw new Error(`not a 6-digit hex colour: ${value}`);
  return [parseInt(match[1], 16), parseInt(match[2], 16), parseInt(match[3], 16)];
}

/** WCAG 2.x relative luminance. */
function luminance([r, g, b]: Rgb): number {
  const channel = (c: number) => {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/** `bg-success/10` paints the colour at 10% alpha over whatever is below it. */
function tint(colour: Rgb, alpha: number, over: Rgb): Rgb {
  return colour.map((c, i) => Math.round(alpha * c + (1 - alpha) * over[i])) as Rgb;
}

const COLOURS = [
  "background", "foreground", "card", "card-foreground", "popover", "popover-foreground",
  "primary", "primary-foreground", "secondary", "secondary-foreground", "muted", "muted-foreground",
  "accent", "accent-foreground", "destructive", "success", "warning", "border", "input", "ring"
];

// Text on its surface must meet WCAG AA for normal text.
const TEXT_PAIRS: [string, string][] = [
  ["foreground", "background"],
  ["card-foreground", "card"],
  ["popover-foreground", "popover"],
  ["primary-foreground", "primary"],
  ["secondary-foreground", "secondary"],
  ["accent-foreground", "accent"],
  ["muted-foreground", "muted"],
  ["muted-foreground", "background"],
  ["primary", "background"],
  ["destructive", "background"],
  ["success", "background"],
  ["warning", "background"]
];

// Badges set a colour's text on a 10% tint of itself.
const TINTED = ["primary", "destructive", "success", "warning"];

describe("theme.css", () => {
  const vars = rootVariables(theme());
  const colour = (name: string) => rgb(vars[name]);

  it("defines every theme colour as 6-digit hex, and the radius", () => {
    for (const name of COLOURS) expect(() => colour(name), `--${name}`).not.toThrow();
    expect(vars.radius).toBe("0.625rem");
  });

  // `shadcn add` appends a component's variables (sidebar, chart) to :root as
  // oklch(), which the contrast checks cannot read. Every colour must be hex.
  it("writes every :root colour as 6-digit hex", () => {
    const offenders = Object.entries(vars).filter(([name, value]) => name !== "radius" && !/^#[0-9a-f]{6}$/i.test(value));
    expect(offenders, `convert to hex: ${offenders.map(([n, v]) => `--${n}: ${v}`).join(", ")}`).toEqual([]);
  });

  it.each(TEXT_PAIRS)("--%s on --%s meets 4.5:1", (fg, bg) => {
    expect(contrast(colour(fg), colour(bg))).toBeGreaterThanOrEqual(4.5);
  });

  it.each(TINTED)("--%s on its own 10%% tint meets 4.5:1", (name) => {
    const surface = tint(colour(name), 0.1, colour("background"));
    expect(contrast(colour(name), surface)).toBeGreaterThanOrEqual(4.5);
  });

  it("draws the focus ring at 3:1 or better", () => {
    expect(contrast(colour("ring"), colour("background"))).toBeGreaterThanOrEqual(3);
  });

  // shadcn components carry dark: classes. Tailwind's default dark: variant
  // follows prefers-color-scheme and would half-darken them on a dark OS.
  it("scopes dark: to a .dark ancestor and defines no dark theme", () => {
    expect(theme()).toContain("@custom-variant dark (&:is(.dark *));");
    expect(theme()).not.toMatch(/\.dark\s*\{/);
  });

  it("clears Tailwind's built-in palette", () => {
    expect(theme()).toMatch(/--color-\*:\s*initial;/);
  });
});

describe("styles.css entry", () => {
  const lines = stripComments(readFileSync(join(STYLES_DIR, "..", "styles.css"), "utf8"))
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  // A layer's position is fixed where it is first declared, so this statement
  // must come before Tailwind's own. It puts legacy between Preflight (base)
  // and the utilities.
  it("fixes the layer order before anything else", () => {
    expect(lines[0]).toBe("@layer theme, base, legacy, components, utilities;");
    expect(lines.slice(1).every((line) => line.startsWith("@import "))).toBe(true);
  });

  it.each(["tokens", "base", "primitives", "features"])("imports %s.css into the legacy layer", (sheet) => {
    expect(lines).toContain(`@import "./styles/${sheet}.css" layer(legacy);`);
  });
});

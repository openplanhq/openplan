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
  "accent", "accent-foreground", "destructive", "success", "warning", "border", "input", "ring",
  "canvas", "primary-strong", "subtle-foreground", "separator", "divider", "dashed-border",
  "muted-strong", "primary-soft", "primary-tint", "warning-soft", "destructive-soft",
  "code-foreground", "tag-foreground"
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
  ["warning", "background"],
  // openplan UI's canvas pages set the same text on the grey ground.
  ["foreground", "canvas"],
  ["muted-foreground", "canvas"],
  ["primary", "canvas"],
  ["warning", "canvas"],
  ["subtle-foreground", "background"],
  ["subtle-foreground", "canvas"],
  ["code-foreground", "background"],
  ["tag-foreground", "canvas"],
  ["muted-foreground", "muted-strong"],
  ["primary-strong", "primary-soft"],
  ["foreground", "primary-soft"],
  ["foreground", "primary-tint"],
  ["muted-foreground", "primary-soft"],
  ["warning", "warning-soft"],
  ["destructive", "destructive-soft"]
];

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

// The theme's values pass on their own, but components paint them translucent:
// hover:bg-primary/80 under white text is 4.11:1 with the brand blue. Read the
// alphas out of the components so a regenerated one is re-checked.
const uiSource = (file: string) => readFileSync(join(STYLES_DIR, "..", "components", "ui", file), "utf8");
const variantIn = (source: string) => (name: string) =>
  source.match(new RegExp(`\\b${name}:\\s*"([^"]*)"`))?.[1] ?? "";

function alpha(classes: string, utility: string): number {
  const escaped = utility.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = classes.match(new RegExp(`(?:^|\\s)${escaped}/(\\d+)(?:\\s|$)`));
  if (!match) throw new Error(`${utility}/<n> not found in "${classes}"`);
  return Number(match[1]) / 100;
}

describe("Button variants on theme.css", () => {
  const vars = rootVariables(theme());
  const colour = (name: string) => rgb(vars[name]);
  const variant = variantIn(uiSource("button.tsx"));
  const background = () => colour("background");

  it("keeps the default button's hover at 4.5:1", () => {
    const surface = tint(colour("primary"), alpha(variant("default"), "hover:bg-primary"), background());
    expect(contrast(colour("primary-foreground"), surface)).toBeGreaterThanOrEqual(4.5);
  });

  it.each(["bg-destructive", "hover:bg-destructive"])("keeps the destructive button's %s tint at 4.5:1", (utility) => {
    const surface = tint(colour("destructive"), alpha(variant("destructive"), utility), background());
    expect(contrast(colour("destructive"), surface)).toBeGreaterThanOrEqual(4.5);
  });
});

// StatusBadge and RoleBadge set each colour's text on a tint of that colour.
describe("Badge variants on theme.css", () => {
  const vars = rootVariables(theme());
  const colour = (name: string) => rgb(vars[name]);
  const variant = variantIn(uiSource("badge.tsx"));

  it.each([
    ["destructive", "destructive"],
    ["success", "success"],
    ["progress", "primary"],
    ["warning", "warning"]
  ])("keeps the %s badge's text on its tint at 4.5:1", (name, themeColour) => {
    const classes = variant(name);
    expect(classes.split(/\s+/)).toContain(`text-${themeColour}`);
    const surface = tint(colour(themeColour), alpha(classes, `bg-${themeColour}`), colour("background"));
    expect(contrast(colour(themeColour), surface)).toBeGreaterThanOrEqual(4.5);
  });

  // A badge rendered as a link fades on hover, as the default button does.
  it("keeps the default badge's link hover at 4.5:1", () => {
    const surface = tint(colour("primary"), alpha(variant("default"), "[a]:hover:bg-primary"), colour("background"));
    expect(contrast(colour("primary-foreground"), surface)).toBeGreaterThanOrEqual(4.5);
  });

  // TEXT_PAIRS above already checks muted-foreground on muted.
  it("paints the muted badge with a checked pair", () => {
    expect(variant("muted").split(/\s+/)).toEqual(expect.arrayContaining(["bg-muted", "text-muted-foreground"]));
  });
});

describe("styles.css entry", () => {
  const lines = stripComments(readFileSync(join(STYLES_DIR, "..", "styles.css"), "utf8"))
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  // The entry is imports only: the app declares no cascade layer of its own,
  // so Tailwind's own order (theme, base, components, utilities) stands.
  it("is nothing but imports", () => {
    expect(lines.every((line) => line.startsWith("@import "))).toBe(true);
  });

  it("imports the theme last, so it wins over Tailwind's defaults", () => {
    expect(lines).toContain('@import "./styles/theme.css";');
    expect(lines[lines.length - 1]).toBe('@import "./styles/theme.css";');
  });

  it("imports no stylesheet that is not Tailwind, shadcn, or the theme", () => {
    expect(lines).toEqual([
      '@import "tailwindcss";',
      '@import "tw-animate-css";',
      '@import "shadcn/tailwind.css";',
      '@import "@fontsource-variable/geist";',
      '@import "@fontsource-variable/geist-mono";',
      '@import "./styles/theme.css";'
    ]);
  });
});

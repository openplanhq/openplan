import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");

function files(dir: string, keep: (path: string) => boolean): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return files(path, keep);
    return keep(path) ? [path] : [];
  });
}

const isTest = (path: string) => /\.test\.tsx?$/.test(path);
// Class strings live in .ts helpers too (buttonClass.ts, fieldClass.ts).
const isSource = (path: string) => /\.tsx?$/.test(path) && !path.endsWith(".d.ts") && !isTest(path);
// Vendored shadcn components use arbitrary values themselves (ring-[3px]).
const isVendored = (path: string) => relative(SRC_DIR, path).startsWith(`components${sep}ui${sep}`);

/** Every whitespace-separated token in every string literal. Over-collects on
    purpose; the rules below only match Tailwind-shaped tokens. */
function classTokens(source: string): string[] {
  return [...source.matchAll(/"([^"\n]*)"|'([^'\n]*)'|`([^`]*)`/g)]
    .map((m) => m[1] ?? m[2] ?? m[3])
    .flatMap((text) => text.split(/\s+/))
    .filter(Boolean);
}

/** The utility after its variants: `md:hover:p-4` -> `p-4`. Colons inside
    brackets or parentheses (`supports-[display:grid]:`) are not variant ends. */
function utilityOf(token: string): string {
  let depth = 0;
  let start = 0;
  for (let i = 0; i < token.length; i++) {
    const ch = token[i];
    if (ch === "[" || ch === "(") depth++;
    else if (ch === "]" || ch === ")") depth--;
    else if (ch === ":" && depth === 0) start = i + 1;
  }
  return token.slice(start);
}

/** `w-[37px]`, `bg-(--brand)`, `[mask-type:luminance]`. Arbitrary VARIANTS
    (`data-[state=open]:`) are selectors, not values, and are allowed. */
function isArbitraryValue(token: string): boolean {
  const utility = utilityOf(token);
  return /-\[[^\]]+\]/.test(utility) || /-\([^)]+\)/.test(utility) || /^\[[\w-]+:.+\]$/.test(utility);
}

const PALETTE = "slate|gray|zinc|neutral|stone|mauve|olive|mist|taupe|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose";

/** Tailwind's built-in colours, which theme.css clears so they generate nothing. */
function isPaletteColour(token: string): boolean {
  const utility = utilityOf(token);
  return new RegExp(`^[a-z-]+-(?:${PALETTE})-\\d{2,3}(?:/\\d+)?$`).test(utility)
    || /^[a-z-]+-(?:black|white)(?:\/\d+)?$/.test(utility);
}


// The type scale, as theme.css defines it: the only text sizes there are, each
// named for its role and each with its own line height. Adding a role is a
// design decision: add it to openplan UI's tokens.json, theme.css,
// src/lib/utils.ts and this list together.
const TYPE_ROLES = ["page-title", "title", "wordmark", "section-title", "label", "body", "meta", "caption", "log"];
// Vendored shadcn components keep two of Tailwind's names; theme.css sets them
// to caption and body exactly. App code uses the role names.
const VENDORED_STEPS = ["xs", "sm"];
// The one line height a class may set apart from a size's own (names on one
// line in a row), and the one vendored components add (Label's leading-none).
const APP_LEADING = ["leading-label"];
const VENDORED_LEADING = [...APP_LEADING, "leading-none"];
// text-* utilities that are not sizes.
const TEXT_NON_SIZE = ["left", "center", "right", "justify", "start", "end", "wrap", "nowrap", "balance", "pretty", "ellipsis", "clip", "current", "inherit", "transparent"];

const THEME = readFileSync(join(SRC_DIR, "styles", "theme.css"), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
const THEME_COLOURS = [...THEME.matchAll(/--color-([\w-]+):/g)].map((m) => m[1]);

/** Why a type utility is off the scale, or null when it is on it: an
    arbitrary size, line height or tracking, a size that is not a role (a
    Tailwind step in app code, a typo), a size with a line-height shorthand
    (text-sm/6), or a line height other than the one allowed. */
function offScale(token: string, vendored = false): string | null {
  const utility = utilityOf(token).replace(/^!|!$/g, "");
  if (/^(?:text|leading|tracking|font)-[[(]/.test(utility)) return "arbitrary type value";
  const text = /^text-([\w-]+?)(?:\/([\w.[\]()-]+))?$/.exec(utility);
  if (text) {
    const [, name, slash] = text;
    if (THEME_COLOURS.includes(name) || TEXT_NON_SIZE.includes(name)) return null;
    if (!TYPE_ROLES.includes(name) && !(vendored && VENDORED_STEPS.includes(name))) return "size off the scale";
    return slash === undefined ? null : "line height beside a size; the role carries its own";
  }
  if (/^leading-/.test(utility) && !(vendored ? VENDORED_LEADING : APP_LEADING).includes(utility)) return "line height off the scale";
  return null;
}

describe("class matchers", () => {
  it.each(["w-[37px]", "grid-cols-[1fr_2fr]", "md:p-[3px]", "bg-(--brand)", "[mask-type:luminance]", "hover:[mask-type:luminance]"])(
    "flags %s as an arbitrary value",
    (token) => expect(isArbitraryValue(token)).toBe(true)
  );
  it.each(["data-[state=open]:bg-muted", "has-[>svg]:px-3", "[&_svg]:size-4", "supports-[display:grid]:grid", "p-4", "bg-success/10", "status-tone--${tone}"])(
    "allows %s",
    (token) => expect(isArbitraryValue(token)).toBe(false)
  );
  it.each(["bg-blue-500", "hover:text-red-600", "border-zinc-200", "text-white", "bg-black/50"])(
    "flags %s as a palette colour",
    (token) => expect(isPaletteColour(token)).toBe(true)
  );
  it.each(["bg-primary", "text-muted-foreground", "bg-success/10", "text-sm", "grid-cols-2", "w-full", "z-50"])(
    "allows %s",
    (token) => expect(isPaletteColour(token)).toBe(false)
  );
  it.each(["text-[13px]", "text-[0.8rem]", "text-lg", "text-base", "text-sm", "md:text-xs", "text-panel-title", "text-meta/6", "leading-6", "leading-tight", "leading-[1.4]", "tracking-[0.1em]", "font-[550]"])(
    "flags %s as off the type scale",
    (token) => expect(offScale(token)).not.toBeNull()
  );
  it.each(["text-meta", "text-caption", "hover:text-title", "text-muted-foreground", "text-primary/80", "text-left", "text-ellipsis", "leading-label", "tracking-wide", "font-medium"])(
    "allows %s on the type scale",
    (token) => expect(offScale(token)).toBeNull()
  );
  it("lets vendored components keep text-xs, text-sm and leading-none", () => {
    expect(["text-xs", "text-sm", "leading-none"].map((token) => offScale(token, true))).toEqual([null, null, null]);
  });
});

describe("app code", () => {
  const appFiles = files(SRC_DIR, (p) => isSource(p) && !isVendored(p));
  const tokens = appFiles.flatMap((path) => classTokens(readFileSync(path, "utf8")).map((token) => ({ path, token })));
  const report = (hits: { path: string; token: string }[]) =>
    hits.map(({ path, token }) => `${relative(SRC_DIR, path)}: ${token}`).join("\n");

  it("finds the files it guards", () => {
    expect(appFiles.length).toBeGreaterThan(20);
  });

  // Hand-tuned values are what the 2026-08-15 decision rejected Tailwind over.
  it("uses no arbitrary values", () => {
    const hits = tokens.filter(({ token }) => isArbitraryValue(token));
    expect(hits, `use a scale step instead:\n${report(hits)}`).toEqual([]);
  });

  it("uses no built-in palette colours", () => {
    const hits = tokens.filter(({ token }) => isPaletteColour(token));
    expect(hits, `use a theme colour instead:\n${report(hits)}`).toEqual([]);
  });

  // 2026-10-22: twelve sizes had grown from eight steps, three of them a
  // pixel apart, with line heights that changed with the parent.
  it("sets type only from the scale", () => {
    const hits = tokens.filter(({ token }) => offScale(token) !== null);
    expect(hits, `use a type role (text-${TYPE_ROLES.join(", text-")}):\n${report(hits)}`).toEqual([]);
  });
});

describe("vendored components", () => {
  const vendored = files(join(SRC_DIR, "components", "ui"), isSource);
  const hits = vendored.flatMap((path) =>
    classTokens(readFileSync(path, "utf8"))
      .filter((token) => offScale(token, true) !== null)
      .map((token) => `${relative(SRC_DIR, path)}: ${token}`)
  );

  it("set type only from the scale", () => {
    expect(vendored.length).toBeGreaterThan(5);
    expect(hits, "use text-xs, text-sm or a type role").toEqual([]);
  });
});

describe("type scale", () => {
  const sizes = [...THEME.matchAll(/--text-([\w-]+):/g)].map((m) => m[1]).filter((name) => !name.includes("--"));

  it("clears Tailwind's own steps, so nothing off the scale renders", () => {
    expect(THEME).toMatch(/--text-\*:\s*initial;/);
  });

  it("defines exactly the roles, each with its own line height", () => {
    expect(sizes.sort()).toEqual([...TYPE_ROLES, ...VENDORED_STEPS].sort());
    for (const size of sizes) expect(THEME, `--text-${size} needs a line height`).toMatch(new RegExp(`--text-${size}--line-height:`));
  });
});

describe("raw type", () => {
  // A font-size or line-height property, or a font shorthand; custom
  // properties (--text-meta--line-height) are theme tokens, not properties.
  const CSS = /(?:^|[\s;{])(?:font-size|line-height|letter-spacing|font)\s*:/m;
  // An inline style setting type: style={{ fontSize: ... }}.
  const INLINE = /\b(?:fontSize|lineHeight|letterSpacing)\s*:/;
  const guarded = files(SRC_DIR, (p) => p.endsWith(".css") || isSource(p));

  it.each(guarded.map((p) => [relative(SRC_DIR, p), p]))("%s sets no type outside the scale", (_, path) => {
    const text = readFileSync(path, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
    const hit = (path.endsWith(".css") ? CSS : INLINE).exec(text);
    expect(hit?.[0] ?? null, "set type with a text-<role> class, not a raw value").toBeNull();
  });
});

describe("raw colours", () => {
  const RAW = /#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab|lab|lch|hwb)\(/g;
  const EXEMPT = [join("styles", "theme.css")];
  const guarded = files(SRC_DIR, (p) => (p.endsWith(".css") || p.endsWith(".tsx")) && !isTest(p))
    .filter((p) => !EXEMPT.includes(relative(SRC_DIR, p)));

  it.each(guarded.map((p) => [relative(SRC_DIR, p), p]))("%s has no colour literals", (_, path) => {
    const text = readFileSync(path, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
    const hits = text.match(RAW) ?? [];
    expect(hits, `colours belong in theme.css: ${hits.join(", ")}`).toEqual([]);
  });
});

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
});

describe("app code", () => {
  const appFiles = files(SRC_DIR, (p) => p.endsWith(".tsx") && !isTest(p) && !isVendored(p));
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

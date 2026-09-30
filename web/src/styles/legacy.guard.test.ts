import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// The stylesheets from before the shadcn migration. They sit in the `legacy`
// cascade layer until PR 9 of
// docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md deletes them,
// and this file goes with them.
const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");
const LEGACY_SHEETS = [
  "styles/tokens.css",
  "styles/base.css",
  "styles/primitives.css",
  "styles/features.css",
  "dev/styleguide.css"
];

function readSheet(path: string): string {
  return readFileSync(join(SRC_DIR, path), "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
}

describe("legacy custom properties", () => {
  // Tailwind and shadcn own --color-*, --radius-*, --shadow-*, --text-*,
  // --font-* and more. Declared in the legacy layer, an unprefixed name would
  // override theirs on :root and shadcn components would render with old values.
  it.each(LEGACY_SHEETS)("%s declares and reads only --legacy-* properties", (path) => {
    const foreign = new Set(
      [...readSheet(path).matchAll(/(?<![\w-])--(?!legacy-)[a-zA-Z][\w-]*/g)].map((m) => m[0])
    );
    expect([...foreign], `prefix with --legacy-: ${[...foreign].join(", ")}`).toEqual([]);
  });
});

function componentSources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return componentSources(path);
    return entry.name.endsWith(".tsx") && !entry.name.includes(".test.") ? [readFileSync(path, "utf8")] : [];
  });
}

describe("legacy class usage", () => {
  const source = componentSources(SRC_DIR).join("\n");
  // `status-tone--${tone}` builds class names at runtime; its literal prefix
  // vouches for every class it can produce.
  const prefixes = [...source.matchAll(/([A-Za-z0-9_-]+)\$\{/g)].map((m) => m[1]);
  const isUsed = (name: string) =>
    new RegExp(`(?<![\\w-])${name}(?![\\w-])`).test(source) || prefixes.some((p) => name.startsWith(p));

  // Each screen PR deletes the rules it stops using. Without this guard, dead
  // CSS would pile up in the legacy layer until PR 9.
  it.each(LEGACY_SHEETS)("%s styles only classes a component uses", (path) => {
    const classes = new Set<string>();
    for (const [, selector] of readSheet(path).matchAll(/([^{}]+)\{/g)) {
      if (selector.trim().startsWith("@")) continue;
      for (const [, name] of selector.matchAll(/\.([A-Za-z_][\w-]*)/g)) classes.add(name);
    }
    const dead = [...classes].filter((name) => !isUsed(name));
    expect(dead, `delete the rules for: ${dead.join(", ")}`).toEqual([]);
  });
});

describe("legacy field rules", () => {
  // shadcn's Label, Input and Textarea carry data-slot. A legacy rule on the
  // element would beat what they leave to inheritance or to their height: the
  // label would turn muted, and Input's h-8 would grow to the legacy 36px
  // min-height. :where() adds no specificity, so legacy fields still match
  // exactly as they did.
  //
  // Only rules that start at the element are checked. A descendant rule such
  // as `.credential-form input` names a legacy class, and when a screen PR
  // migrates that markup the class goes with it and the dead-CSS guard above
  // makes the rule go too.
  it.each(LEGACY_SHEETS)("%s skips elements that carry data-slot", (path) => {
    const unscoped = [...readSheet(path).matchAll(/([^{}]+)\{/g)]
      .flatMap(([, selector]) => selector.split(","))
      .map((selector) => selector.trim())
      .filter((selector) => /^(label|input|textarea|select)(?![\w-])/.test(selector))
      .filter((selector) => !/^(label|input|textarea|select):where\(:not\(\[data-slot\]\)\)/.test(selector));
    expect(unscoped, `scope with :where(:not([data-slot])): ${unscoped.join(", ")}`).toEqual([]);
  });
});

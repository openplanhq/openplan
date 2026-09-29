import { readFileSync } from "node:fs";
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

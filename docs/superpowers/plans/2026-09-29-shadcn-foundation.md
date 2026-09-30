# shadcn Foundation (PR 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Put Tailwind v4, the shadcn theme and one shadcn component (Button) into `web/`, next to the existing stylesheets, without changing how any screen looks.

**Architecture:** Tailwind and the shadcn theme load in the normal CSS cascade layers (`theme`, `base`, `components`, `utilities`). The existing stylesheets go into a new `legacy` layer placed between `base` and `components`, so they override Tailwind's reset (Preflight) but not its utilities. Every custom property in the existing stylesheets gets a `--legacy-` prefix so it can't shadow a Tailwind or shadcn variable. New Vitest guards enforce contrast, the layer setup, class hygiene and legacy-CSS hygiene.

**Tech Stack:** React 19, Vite 8, Vitest 4, Tailwind CSS 4.3, shadcn CLI 4.21 (`base-nova` style on Base UI), `cn` 0.4, Fontsource Geist variable fonts.

**Spec:** `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md` (read the "Theme", "Running old and new styles side by side" and "Guards" sections before starting).

## Global Constraints

- Branch `feat/shadcn-foundation`, stacked on `chore/react-19`. The PR base is `chore/react-19`.
- All commands run from `web/` unless a step says otherwise.
- No screen's markup or behaviour changes. The only intended visual change is removing the page glow. `/styleguide` (dev-only) gains a section.
- shadcn: Base UI primitives; `components.json` has `"style": "base-nova"`, `"baseColor": "neutral"`, `"cssVariables": true`, `"iconLibrary": "lucide"`.
- Paths: shadcn components in `src/components/ui/`, `cn` re-exported from `src/lib/utils.ts`, alias `@/` → `src/`.
- Runtime dependencies: `@base-ui/react`, `class-variance-authority`, `cn`, `@fontsource-variable/geist`, `@fontsource-variable/geist-mono`. Build-only dependencies go in `devDependencies`: `tailwindcss`, `@tailwindcss/vite`, `tw-animate-css`, `shadcn`.
- Theme values, lowercase hex: `--background #ffffff`, `--foreground #0a0a0a`, `--card #ffffff`, `--card-foreground #0a0a0a`, `--popover #ffffff`, `--popover-foreground #0a0a0a`, `--primary #0052ff`, `--primary-foreground #ffffff`, `--secondary #f5f5f5`, `--secondary-foreground #171717`, `--muted #f5f5f5`, `--muted-foreground #525252`, `--accent #f5f5f5`, `--accent-foreground #171717`, `--destructive #b91c1c`, `--success #166534`, `--warning #92400e`, `--border #e5e5e5`, `--input #e5e5e5`, `--ring #0052ff`, `--radius 0.625rem`.
- Light only: no `.dark { … }` block. `@custom-variant dark (&:is(.dark *));` stays.
- The five legacy stylesheets are `src/styles/tokens.css`, `src/styles/base.css`, `src/styles/primitives.css`, `src/styles/features.css` and `src/dev/styleguide.css`. Every custom property they declare or read starts with `--legacy-`.
- New rules added to legacy files must target a legacy class, never a bare element. The only exceptions are `html` and `body`. Bare element rules in the `legacy` layer would also reach shadcn components.
- Commit subjects are lowercase-prefixed (`feat(web):`, `test(web):`, `refactor(web):`, `docs:`) and every commit message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **The user's OS is in dark mode.** shadcn components carry `dark:` classes, and Tailwind's default `dark:` follows `prefers-color-scheme`, which would half-darken them. A theme guard in Task 4 asserts the `.dark`-ancestor custom variant and the absence of a `.dark` block.
2. **A later `shadcn add` writes `oklch()` values or a `.dark` block into `theme.css`.** The contrast guard can't read `oklch()`, and a `.dark` block contradicts the spec. The Task 4 theme guard requires every `:root` colour to be 6-digit hex and forbids `.dark {`.
3. **Someone adds an unprefixed custom property to a legacy file** (say `--gap`), and it silently overrides a Tailwind variable of the same name. The Task 2 legacy-token guard catches this.
4. **An unmigrated screen relied on a browser default that Preflight removes** (paragraph margins, list bullets, inline icons, inherited text colour). Task 7 diffs the computed styles of every screen before and after and fixes each difference.
5. **The Docker image builds on Alpine (musl) from a lockfile generated on macOS.** Tailwind's native binaries (`@tailwindcss/oxide`, `lightningcss`) must resolve there. Task 8 runs `docker compose build web`.

---

### Task 1: `@/` alias and `cn`

**Files:**
- Modify: `web/package.json`, `web/package-lock.json` (via npm)
- Modify: `web/vite.config.ts`
- Modify: `web/tsconfig.json`
- Create: `web/src/lib/utils.ts`
- Test: `web/src/lib/utils.test.ts`

**Interfaces:**
- Produces: `import { cn } from "@/lib/utils"`, where `cn(...inputs: ClassValue[]): string` merges class names and later Tailwind classes win (the `clsx` + `tailwind-merge` semantics). Produces the `@/` → `src/` alias for Vite, Vitest and `tsc`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/utils.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { cn } from "@/lib/utils";

describe("cn", () => {
  it("resolves through the @/ alias and lets the later Tailwind class win", () => {
    expect(cn("px-2 text-sm", undefined, "px-4")).toBe("text-sm px-4");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/lib/utils.test.ts`
Expected: FAIL, because the import of `@/lib/utils` can't be resolved.

- [ ] **Step 3: Install `cn` and add the alias**

Run: `npm install cn@^0.4.0`

Create `web/src/lib/utils.ts` (this is exactly what `shadcn init` generates for this preset):

```ts
export { cn } from "cn"
```

Replace `web/vite.config.ts` with:

```ts
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) }
  },
  server: {
    port: 5173,
    proxy: {
      "/healthz": "http://localhost:8081",
      "/v1": "http://localhost:8081"
    }
  },
  test: {
    environment: "node"
  }
});
```

In `web/tsconfig.json`, add these two keys to `compilerOptions`. The shadcn CLI validates the alias through `baseUrl` + `paths`.

```json
    "baseUrl": ".",
    "paths": { "@/*": ["./src/*"] },
```

- [ ] **Step 4: Run the test and the type-check**

Run: `npx vitest run src/lib/utils.test.ts && npx tsc -b`
Expected: 1 test passes; `tsc` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add package.json package-lock.json vite.config.ts tsconfig.json src/lib
git commit -m "feat(web): add the @/ import alias and cn" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Prefix every legacy custom property with `--legacy-`

**Files:**
- Create: `web/src/styles/legacy.guard.test.ts`
- Modify: `web/src/styles/tokens.css`, `web/src/styles/base.css`, `web/src/styles/primitives.css`, `web/src/styles/features.css`, `web/src/dev/styleguide.css`, `web/src/dev/StyleGuide.tsx`, `web/src/styles/styles.guard.test.ts`

**Interfaces:**
- Produces: `LEGACY_SHEETS` and `readSheet()` in `legacy.guard.test.ts`, which Task 3 extends. After this task, every legacy token is named `--legacy-<old name>`; for example, `--color-accent` becomes `--legacy-color-accent`.

- [ ] **Step 1: Write the failing guard**

Create `web/src/styles/legacy.guard.test.ts`:

```ts
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL for all five sheets, listing names such as `--color-bg`, `--space-4`, `--data-table-min-width` and `--reveal-delay`.

- [ ] **Step 3: Record the built CSS before renaming**

Run: `npm run build && cp dist/assets/index-*.css /tmp/legacy-before.css`

This is used in Step 7 to prove the rename changes nothing but names.

- [ ] **Step 4: Rename with a one-off script (not committed)**

Save as `/tmp/rename_legacy.py` and run it from the repository root with `python3 /tmp/rename_legacy.py`:

```python
import pathlib, re

root = pathlib.Path("web/src")
sheets = [root / p for p in (
    "styles/tokens.css", "styles/base.css", "styles/primitives.css",
    "styles/features.css", "dev/styleguide.css")]
ident = re.compile(r"(?<![\w-])--([a-zA-Z][\w-]*)(?![\w-])")
strip = lambda css: re.sub(r"/\*.*?\*/", "", css, flags=re.S)

# Every custom property the legacy sheets declare OR read, outside comments.
# Comments quote Geist's own variables (--geist-radius), which keep their names;
# --reveal-delay is read with a fallback but never declared, and still counts.
names = set()
for sheet in sheets:
    names |= set(ident.findall(strip(sheet.read_text())))

# StyleGuide.tsx lists token names to read at runtime; the old guard lists
# them in REQUIRED_TOKENS and SCALES.
targets = sheets + [root / "dev/StyleGuide.tsx", root / "styles/styles.guard.test.ts"]
for path in targets:
    text = path.read_text()
    path.write_text(ident.sub(lambda m: f"--legacy-{m.group(1)}" if m.group(1) in names else m.group(0), text))
print(f"{len(names)} names prefixed")
```

Expected: the last line reads `77 names prefixed`.

- [ ] **Step 5: Fix the five fragments the script can't see**

These are regex and substring fragments in `web/src/styles/styles.guard.test.ts`, not whole identifiers, so the script leaves them. Make exactly these replacements:

| Find | Replace with |
|---|---|
| `var\(--radius-[a-z]+\)` | `var\(--legacy-radius-[a-z]+\)` |
| `var\(--shadow-[a-z-]+\)` | `var\(--legacy-shadow-[a-z-]+\)` |
| `size?.includes("--text-")` | `size?.includes("--legacy-text-")` |
| `size.match(/--text-[\w-]+/)` | `size.match(/--legacy-text-[\w-]+/)` |
| `spacing.match(/--tracking-[\w-]+/)` | `spacing.match(/--legacy-tracking-[\w-]+/)` |

In the same file, update the three error messages that say `use var(--color-*)`, `use var(--radius-*)` and `use var(--shadow-*)` to the `--legacy-` forms.

Replace the opening comment of `web/src/styles/tokens.css` with:

```css
/* Legacy tokens, prefixed --legacy- so none can shadow a Tailwind or shadcn
   variable of the same name. With theme.css this is one of only two files
   permitted raw colour literals. PR 9 of the shadcn migration deletes it. */
```

- [ ] **Step 6: Run the whole suite**

Run: `npm test`
Expected: all tests pass, including the five new `legacy custom properties` cases.

Then run: `grep -nE '(^|[^-])--(color|space|text|radius|shadow|font)-' src/dev/StyleGuide.tsx`
Expected: no output.

- [ ] **Step 7: Prove nothing but names changed**

Run:

```bash
npm run build
diff <(sed 's/--legacy-/--/g' dist/assets/index-*.css) /tmp/legacy-before.css && echo IDENTICAL
```

Expected: `IDENTICAL`.

- [ ] **Step 8: Commit**

```bash
git add src/styles src/dev
git commit -m "refactor(web): prefix legacy custom properties with --legacy-" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Guard against unused legacy CSS, and delete what's already unused

**Files:**
- Modify: `web/src/styles/legacy.guard.test.ts`
- Modify: `web/src/styles/primitives.css` (delete `.hint-text`), `web/src/styles/features.css` (delete `.stack-template-list`, `.stack-template-list h3`, and `.search-result-item.focused` from one selector list)

**Interfaces:**
- Consumes: `SRC_DIR`, `LEGACY_SHEETS` and `readSheet` from Task 2.

- [ ] **Step 1: Write the failing guard**

Change the first import line of `web/src/styles/legacy.guard.test.ts` to:

```ts
import { readdirSync, readFileSync } from "node:fs";
```

Then append:

```ts
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `styles/primitives.css` (`hint-text`) and on `styles/features.css` (`focused`, `stack-template-list`).

- [ ] **Step 3: Confirm each class is really unused**

Run: `grep -rnE "hint-text|[\"' ]focused[\"' ]|stack-template-list[\"' ]" src --include='*.tsx'`
Expected: no output. If there is any output, stop and report it; the guard's matching would then be wrong.

- [ ] **Step 4: Delete the dead rules**

In `web/src/styles/primitives.css`, delete this block together with the comment above it:

```css
/* A blocked action that is nobody's mistake — a stale plan reads as a warning,
   not a failure, so it stays distinct from .error-text. */
.hint-text {
  margin: var(--legacy-space-3) 0 0;
  color: var(--legacy-color-warning);
  font-size: var(--legacy-text-sm);
  font-weight: 400;
}
```

In `web/src/styles/features.css`, delete both of these blocks, and keep the `/* ---- Template lists ---- */` comment:

```css
.stack-template-list {
  display: grid;
  gap: var(--legacy-space-3);
  margin-top: var(--legacy-space-5);
  border-top: 1px solid var(--legacy-color-border);
  padding-top: var(--legacy-space-4);
}

.stack-template-list h3 {
  margin: 0;
  font-size: var(--legacy-text-sm);
  font-weight: 400;
}
```

In `web/src/styles/features.css`, change the selector list

```css
.search-result-item:hover,
.search-result-item.focused {
```

to

```css
.search-result-item:hover {
```

- [ ] **Step 5: Run the whole suite**

Run: `npm test`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add src/styles
git commit -m "test(web): fail on legacy CSS no component uses, and drop three dead rules" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Tailwind, the shadcn theme, the cascade layers and the fonts

**Files:**
- Create: `web/src/styles/theme.css`, `web/src/styles/theme.guard.test.ts`
- Modify: `web/src/styles.css`, `web/vite.config.ts`, `web/src/styles/tokens.css`, `web/src/styles/base.css`, `web/src/dev/styleguide.css`, `web/src/styles/styles.guard.test.ts`, `web/index.html`, `web/package.json`, `web/package-lock.json`
- Delete: `web/public/fonts/geist-400.woff2`, `web/public/fonts/geist-mono-400.woff2`, `scripts/vendor-fonts.sh`

**Interfaces:**
- Produces: Tailwind utility classes for `background`, `foreground`, `card`, `card-foreground`, `popover`, `popover-foreground`, `primary`, `primary-foreground`, `secondary`, `secondary-foreground`, `muted`, `muted-foreground`, `accent`, `accent-foreground`, `destructive`, `success`, `warning`, `border`, `input`, `ring`, `black` and `white` (e.g. `bg-primary`, `text-muted-foreground`, `bg-success/10`); `font-sans` and `font-mono`; `rounded-sm` through `rounded-4xl`. Task 6 uses these.

- [ ] **Step 1: Write the failing theme guard**

Create `web/src/styles/theme.guard.test.ts`:

```ts
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/styles/theme.guard.test.ts`
Expected: FAIL; `theme.css` doesn't exist (ENOENT) and `styles.css` has no `@layer` line.

- [ ] **Step 3: Install Tailwind and the fonts**

```bash
npm install @fontsource-variable/geist@^5.3.0 @fontsource-variable/geist-mono@^5.3.0
npm install -D tailwindcss@^4.3.3 @tailwindcss/vite@^4.3.3 tw-animate-css@^1.4.0 shadcn@^4.21.0
```

`shadcn` is a devDependency only because `shadcn/tailwind.css` (the custom variants Base UI components rely on, such as `data-open:`) is imported from it. It isn't shipped to the browser.

In `web/vite.config.ts`, add `import tailwindcss from "@tailwindcss/vite";` below the `react` import, and change `plugins: [react()],` to `plugins: [react(), tailwindcss()],`.

- [ ] **Step 4: Create `web/src/styles/theme.css`**

```css
/* The shadcn theme. With tokens.css (legacy, until PR 9) this is one of only
   two files permitted raw colour literals. Values are hex, converted from
   shadcn's neutral oklch() defaults, so theme.guard.test.ts can compute
   contrast. Light only: there is deliberately no .dark block. */

/* Test files hold deliberately bad class strings; keep them out of the build. */
@source not "../**/*.test.{ts,tsx}";

/* shadcn components carry dark: classes. Tailwind's default dark: variant
   follows prefers-color-scheme, which would half-darken them for anyone whose
   OS is dark. Scoped to a .dark ancestor, which the app never sets, they stay
   light. */
@custom-variant dark (&:is(.dark *));

/* Only theme colours exist. Black and white stay because vendored shadcn
   components use them (alert-dialog's overlay is bg-black/10); the class guard
   keeps app code off them. */
@theme {
  --color-*: initial;
  --color-black: #000000;
  --color-white: #ffffff;
}

@theme inline {
  --font-sans: "Geist Variable", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, monospace;
  --font-heading: var(--font-sans);
  --color-background: var(--background);
  --color-foreground: var(--foreground);
  --color-card: var(--card);
  --color-card-foreground: var(--card-foreground);
  --color-popover: var(--popover);
  --color-popover-foreground: var(--popover-foreground);
  --color-primary: var(--primary);
  --color-primary-foreground: var(--primary-foreground);
  --color-secondary: var(--secondary);
  --color-secondary-foreground: var(--secondary-foreground);
  --color-muted: var(--muted);
  --color-muted-foreground: var(--muted-foreground);
  --color-accent: var(--accent);
  --color-accent-foreground: var(--accent-foreground);
  --color-destructive: var(--destructive);
  --color-success: var(--success);
  --color-warning: var(--warning);
  --color-border: var(--border);
  --color-input: var(--input);
  --color-ring: var(--ring);
  --radius-sm: calc(var(--radius) * 0.6);
  --radius-md: calc(var(--radius) * 0.8);
  --radius-lg: var(--radius);
  --radius-xl: calc(var(--radius) * 1.4);
  --radius-2xl: calc(var(--radius) * 1.8);
  --radius-3xl: calc(var(--radius) * 2.2);
  --radius-4xl: calc(var(--radius) * 2.6);
}

:root {
  --background: #ffffff;
  --foreground: #0a0a0a;
  --card: #ffffff;
  --card-foreground: #0a0a0a;
  --popover: #ffffff;
  --popover-foreground: #0a0a0a;
  --primary: #0052ff;
  --primary-foreground: #ffffff;
  --secondary: #f5f5f5;
  --secondary-foreground: #171717;
  --muted: #f5f5f5;
  /* shadcn's #737373 is 4.35:1 on --muted, below AA. */
  --muted-foreground: #525252;
  --accent: #f5f5f5;
  --accent-foreground: #171717;
  --destructive: #b91c1c;
  --success: #166534;
  --warning: #92400e;
  --border: #e5e5e5;
  --input: #e5e5e5;
  --ring: #0052ff;
  --radius: 0.625rem;
}

@layer base {
  * {
    @apply border-border outline-ring/50;
  }
  body {
    @apply bg-background text-foreground;
  }
  html {
    @apply font-sans;
  }
}
```

- [ ] **Step 5: Rewrite `web/src/styles.css`**

```css
@layer theme, base, legacy, components, utilities;
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "@fontsource-variable/geist";
@import "@fontsource-variable/geist-mono";
@import "./styles/theme.css";
@import "./styles/tokens.css" layer(legacy);
@import "./styles/base.css" layer(legacy);
@import "./styles/primitives.css" layer(legacy);
@import "./styles/features.css" layer(legacy);
```

Wrap the whole of `web/src/dev/styleguide.css` in a legacy layer block: put `@layer legacy {` as the first line and `}` as the last. It's imported from JavaScript, not from `styles.css`. Unwrapped, it would be unlayered and would beat Tailwind's utilities.

- [ ] **Step 6: Point the legacy fonts at the Fontsource faces and drop the vendored files**

In `web/src/styles/tokens.css`, set:

```css
  --legacy-font-display: "Geist Variable", system-ui, -apple-system, "Segoe UI", sans-serif;
  --legacy-font-body:    "Geist Variable", system-ui, -apple-system, "Segoe UI", sans-serif;
  --legacy-font-mono:    "Geist Mono Variable", ui-monospace, SFMono-Regular, Menlo, Monaco,
                         Consolas, "Liberation Mono", monospace;
```

In `web/src/styles/base.css`, delete the `/* ---- Self-hosted faces … */` comment and both `@font-face` blocks that follow it.

In `web/index.html`, delete the line
`<link rel="preload" href="/fonts/geist-400.woff2" as="font" type="font/woff2" crossorigin />`.

Run from the repository root: `git rm web/public/fonts/geist-400.woff2 web/public/fonts/geist-mono-400.woff2 scripts/vendor-fonts.sh`

- [ ] **Step 7: Remove the page glow**

In `web/src/styles/base.css`, delete the `/* ---- Ambient accent glows … */` comment, the `body::before { … }` block, and the `body > * { position: relative; z-index: 1; }` block. The latter existed only to lift content above the glow, and it would also apply to the containers Base UI appends to `<body>` for popovers.

In `web/src/styles/tokens.css`, delete the `--legacy-color-accent-wash` line; only the glow used it.

- [ ] **Step 8: Scope the old style guard to the legacy sheets**

In `web/src/styles/styles.guard.test.ts`:

- Replace the `convertedStylesheets` function and its comment with:

  ```ts
  /** Legacy stylesheets subject to the palette rules. tokens.css defines the
      palette; theme.css is the shadcn theme and has its own guard. */
  function convertedStylesheets(): string[] {
    return ["base.css", "features.css", "primitives.css"];
  }
  ```

- Change the first import to `import { readFileSync } from "node:fs";`.
- Delete the whole `describe("styles.css index", …)` block; `theme.guard.test.ts` replaces it.

- [ ] **Step 9: Run the tests and the build**

Run: `npm test && npm run build`
Expected: all tests pass, including every `theme.css` and `styles.css entry` case; the build succeeds.

Then run:

```bash
grep -oE "@layer [a-z]+[ {;]" dist/assets/index-*.css | awk '!seen[$0]++'
```

Expected, in this order: `@layer properties{`, `@layer theme{`, `@layer base{`, `@layer legacy{`, `@layer components;`, `@layer utilities{`. Any other order means legacy is not between Preflight and the utilities: stop and report it.

- [ ] **Step 10: Commit**

```bash
git add -A .
git commit -m "feat(web): load tailwind and the shadcn theme beside a legacy cascade layer" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Class and raw-colour guards

**Files:**
- Create: `web/src/styles/tailwind.guard.test.ts`

**Interfaces:**
- Consumes: the cleared palette from Task 4 (the guard makes a silent no-op into a failure).

- [ ] **Step 1: Write the guard**

Create `web/src/styles/tailwind.guard.test.ts`:

```ts
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
  const EXEMPT = [join("styles", "theme.css"), join("styles", "tokens.css")];
  const guarded = files(SRC_DIR, (p) => (p.endsWith(".css") || p.endsWith(".tsx")) && !isTest(p))
    .filter((p) => !EXEMPT.includes(relative(SRC_DIR, p)));

  it.each(guarded.map((p) => [relative(SRC_DIR, p), p]))("%s has no colour literals", (_, path) => {
    const text = readFileSync(path, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
    const hits = text.match(RAW) ?? [];
    expect(hits, `colours belong in theme.css: ${hits.join(", ")}`).toEqual([]);
  });
});
```

- [ ] **Step 2: Run it**

Run: `npx vitest run src/styles/tailwind.guard.test.ts`
Expected: PASS. No app code uses Tailwind yet, and no file outside the two exempt ones has a colour literal.

- [ ] **Step 3: Prove each guard bites, then revert**

Temporarily add `<div className="w-[37px] bg-blue-500" style={{ color: "#abcdef" }} />` as the first child of the returned JSX in `web/src/app/NotFound.tsx`.

Run: `npx vitest run src/styles/tailwind.guard.test.ts`
Expected: 3 failures: `uses no arbitrary values` (`w-[37px]`), `uses no built-in palette colours` (`bg-blue-500`), and `app/NotFound.tsx has no colour literals` (`#abcdef`).

Run: `git checkout src/app/NotFound.tsx`, then re-run the guard. Expected: PASS.

- [ ] **Step 4: Confirm test-only class strings stay out of the build**

Run: `npm run build && grep -c 'w-\\\[37px\\\]' dist/assets/index-*.css`
Expected: `0`. The `@source not` line in `theme.css` keeps the guard's sample strings out.

- [ ] **Step 5: Commit**

```bash
git add src/styles/tailwind.guard.test.ts
git commit -m "test(web): fail on arbitrary values, built-in palette colours and stray colour literals" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `components.json`, the Button, and a theme section on `/styleguide`

**Files:**
- Create: `web/components.json`, `web/src/components/ui/button.tsx` (generated)
- Modify: `web/src/dev/StyleGuide.tsx`, `web/src/dev/StyleGuide.test.tsx`, `web/package.json`, `web/package-lock.json`

**Interfaces:**
- Consumes: `cn` from `@/lib/utils` (Task 1); theme utilities (Task 4).
- Produces: `import { Button } from "@/components/ui/button"`, a Base UI button with `variant: "default" | "outline" | "secondary" | "ghost" | "destructive" | "link"` and `size: "default" | "xs" | "sm" | "lg" | "icon" | "icon-xs" | "icon-sm" | "icon-lg"`.

- [ ] **Step 1: Write the failing style guide tests**

In `web/src/dev/StyleGuide.test.tsx`, change the Testing Library import to
`import { cleanup, render, screen, within } from "@testing-library/react";`
and add inside the `describe`:

```tsx
  it("shows every shadcn Button variant", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    for (const name of ["Default", "Outline", "Secondary", "Ghost", "Destructive", "Link"]) {
      expect(section.getByRole("button", { name })).toBeTruthy();
    }
  });

  it("swatches every theme colour", () => {
    const { container } = render(<StyleGuide />);
    for (const name of ["background", "foreground", "primary", "secondary", "muted", "accent", "destructive", "success", "warning", "border"]) {
      expect(container.querySelector(`[data-swatch="${name}"]`), `missing --${name}`).toBeTruthy();
    }
  });
```

- [ ] **Step 2: Run them to verify they fail**

Run: `npx vitest run src/dev/StyleGuide.test.tsx`
Expected: the two new tests FAIL (`Unable to find an element by: [data-testid="sg-theme"]`, then `missing --background`).

- [ ] **Step 3: Create `web/components.json`**

This is what `shadcn init --base base --preset nova` writes, except `tailwind.css`, which points at `theme.css` so later `shadcn add` runs put new variables there.

```json
{
  "$schema": "https://ui.shadcn.com/schema.json",
  "style": "base-nova",
  "rsc": false,
  "tsx": true,
  "tailwind": {
    "config": "",
    "css": "src/styles/theme.css",
    "baseColor": "neutral",
    "cssVariables": true,
    "prefix": ""
  },
  "iconLibrary": "lucide",
  "rtl": false,
  "aliases": {
    "components": "@/components",
    "utils": "@/lib/utils",
    "ui": "@/components/ui",
    "lib": "@/lib",
    "hooks": "@/hooks"
  },
  "menuColor": "default",
  "menuAccent": "subtle",
  "registries": {}
}
```

- [ ] **Step 4: Add the Button**

```bash
npm install @base-ui/react@^1.8.0 class-variance-authority@^0.7.1
npx shadcn add button -y
```

Expected: `Created 1 file: src/components/ui/button.tsx`. Run `git diff package.json`: if the CLI moved or added anything outside the Global Constraints' dependency lists, move it back. Run `git diff src/styles/theme.css`: it must be empty; revert it if not.

- [ ] **Step 5: Add the theme section to `web/src/dev/StyleGuide.tsx`**

Add these imports after the existing ones:

```tsx
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
```

Make `{ id: "theme", title: "shadcn theme" }` the first entry of `SECTIONS`.

Add below `SECTIONS`:

```tsx
// Literal class names: Tailwind only generates classes it can find in source.
const THEME_SWATCHES = [
  { name: "background", className: "bg-background" },
  { name: "foreground", className: "bg-foreground" },
  { name: "primary", className: "bg-primary" },
  { name: "secondary", className: "bg-secondary" },
  { name: "muted", className: "bg-muted" },
  { name: "accent", className: "bg-accent" },
  { name: "destructive", className: "bg-destructive" },
  { name: "success", className: "bg-success" },
  { name: "warning", className: "bg-warning" },
  { name: "border", className: "bg-border" }
];

const BUTTON_VARIANTS = ["default", "outline", "secondary", "ghost", "destructive", "link"] as const;
const BUTTON_SIZES = ["xs", "sm", "default", "lg"] as const;
const titleCase = (word: string) => word[0].toUpperCase() + word.slice(1);
```

Insert this as the first child of `<main className="sg__main">`, after the `sg__intro` div and before the Colour section:

```tsx
        <div data-testid="sg-theme">
          <Section
            id="theme"
            title="shadcn theme"
            note="Components from src/components/ui on theme.css. Everything below this section is the legacy system, retired screen by screen."
          >
            <Specimen label="Colours" hint="theme.css">
              <div className="grid w-full grid-cols-2 gap-3 sm:grid-cols-5">
                {THEME_SWATCHES.map(({ name, className }) => (
                  <div key={name} data-swatch={name} className="flex flex-col gap-1.5">
                    <div className={cn("h-10 rounded-md border", className)} />
                    <code className="font-mono text-xs text-muted-foreground">--{name}</code>
                  </div>
                ))}
              </div>
            </Specimen>
            <Specimen label="Button variants">
              {BUTTON_VARIANTS.map((variant) => (
                <Button key={variant} variant={variant}>
                  {titleCase(variant)}
                </Button>
              ))}
            </Specimen>
            <Specimen label="Button sizes">
              {BUTTON_SIZES.map((size) => (
                <Button key={size} size={size} variant="outline">
                  {size}
                </Button>
              ))}
              <Button size="icon" aria-label="Add">
                <Plus />
              </Button>
            </Specimen>
          </Section>
        </div>
```

- [ ] **Step 6: Run the tests, the type-check and the guards**

Run: `npm test && npx tsc -b`
Expected: all pass, including the two new style guide tests, `links to every section it documents` (now including `#theme`), and `tailwind.guard.test.ts`, which now scans real Tailwind classes.

- [ ] **Step 7: Look at it**

Run `npm run dev -- --port 5199 --strictPort` in the background, then:

```bash
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
  --user-data-dir=/tmp/sg-chrome --window-size=1400,1400 --virtual-time-budget=5000 \
  --screenshot=/tmp/sg-theme.png http://127.0.0.1:5199/styleguide
```

Headless Chrome may not exit on its own after writing the file; stop it with `pkill -f /tmp/sg-chrome`. Open `/tmp/sg-theme.png`. Expected: the default Button is solid `#0052ff` with white text; the Destructive button has red text on a light red tint; swatches show white, near-black, blue, greys, red, green, amber and a light grey; the legacy sections below look as they did before this branch. Stop the dev server.

- [ ] **Step 8: Commit**

```bash
git add components.json src/components src/dev package.json package-lock.json
git commit -m "feat(web): add the shadcn button and a theme section to the style guide" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Preflight audit of every legacy screen

Preflight zeroes margins and padding, removes list bullets, makes `svg` and `img` block-level, and resets button styles. The theme also paints `body` with shadcn's colours. This task finds every place an unmigrated screen relied on those defaults, by diffing computed styles before and after, and restores each one in the legacy file that owns it.

**Files:**
- Modify: `scripts/drive-web.mjs`
- Modify: `web/src/styles/base.css`, and whichever of `web/src/styles/primitives.css`, `web/src/styles/features.css`, `web/src/dev/styleguide.css` own the differing elements

- [ ] **Step 1: Teach the driver the app's own sign-in form, SPA navigation and a signed-out mode**

`scripts/drive-web.mjs` only knows Keycloak's server-rendered login page (`#username`). The app now shows its own React form (`#signin-username`, `#signin-password`), where `form.submit()` would bypass React's handler.

In the flag parsing, add two flags. Declare `let gotos = [], signedOut = false;` beside the others, and add these branches to the `for` loop:

```js
  else if (argv[i] === "--goto") gotos.push(next());
  else if (argv[i] === "--signed-out") signedOut = true;
```

Directly after `await settle(6000);` (the one following `Page.navigate`), insert:

```js
// The app's own sign-in form submits through React, so .submit() would skip
// its handler; requestSubmit() fires the submit event React listens for.
if (!signedOut && await evaluate("!!document.querySelector('#signin-username')")) {
  await evaluate(`(() => {
    const u = document.querySelector('#signin-username'), p = document.querySelector('#signin-password');
    const set = (el, v) => {
      Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el), 'value').set.call(el, v);
      el.dispatchEvent(new Event('input', { bubbles: true }));
    };
    set(u, ${JSON.stringify(USER)});
    set(p, ${JSON.stringify(PASS)});
    u.form.requestSubmit();
  })()`);
  await settle(8000);
}
```

Change the existing Keycloak block's condition from
`if (await evaluate("!!document.querySelector('#username')")) {`
to
`if (!signedOut && await evaluate("!!document.querySelector('#username')")) {`.

Before the loop that performs `--click`s, add:

```js
// Client-side navigation: react-router follows popstate, and a full load
// would drop the in-memory session state the SPA holds.
for (const path of gotos) {
  await evaluate(`history.pushState({}, "", ${JSON.stringify(path)}); dispatchEvent(new PopStateEvent("popstate"))`);
  await settle(3500);
}
```

In the header comment, replace the "Why this exists" paragraph's claim about `OidcAuthProvider` with: "Every screen except /styleguide needs a session. The driver signs in through the app's own form (or Keycloak's page, if it lands there), then moves around with client-side navigation (--goto) or clicks (--click)." Add `--goto <path>` and `--signed-out` to the flag list.

Verify, with the local stack running and `OPENPLAN_USER`/`OPENPLAN_PASS` exported as the header describes:
`node scripts/drive-web.mjs --goto /templates --probe 'location.pathname'` prints `/templates`.

- [ ] **Step 2: Save the style dump and diff scripts (not committed)**

Save as `/tmp/audit/dump.js`:

```js
(() => {
  const PROPS = ["display", "position", "vertical-align", "list-style-type",
    "margin-top", "margin-right", "margin-bottom", "margin-left",
    "padding-top", "padding-right", "padding-bottom", "padding-left",
    "border-top-width", "border-top-style", "border-top-color",
    "border-bottom-width", "border-bottom-style", "border-bottom-color",
    "font-size", "font-weight", "line-height", "color", "background-color",
    "text-decoration-line", "border-collapse", "cursor"];
  const key = (el) => {
    const parts = [];
    for (let e = el; e && e.id !== "root"; e = e.parentElement) {
      const same = [...e.parentElement.children].filter((c) => c.tagName === e.tagName);
      const cls = typeof e.className === "string" && e.className.trim() ? "." + e.className.trim().split(/\s+/).join(".") : "";
      parts.unshift(`${e.tagName.toLowerCase()}[${same.indexOf(e)}]${cls}`);
    }
    return parts.join(" > ");
  };
  const out = {};
  for (const el of document.querySelectorAll("#root *")) {
    const cs = getComputedStyle(el);
    out[key(el)] = Object.fromEntries(PROPS.map((p) => [p, cs.getPropertyValue(p)]));
  }
  return JSON.stringify(out);
})()
```

Save as `/tmp/audit/diff.py`:

```python
import json, pathlib, sys

before_dir, after_dir = map(pathlib.Path, sys.argv[1:3])
clean = True
for before_file in sorted(before_dir.glob("*.json")):
    after_file = after_dir / before_file.name
    before, after = json.loads(before_file.read_text()), json.loads(after_file.read_text())
    for path in sorted(set(before) | set(after)):
        if path not in before or path not in after:
            print(f"{before_file.stem}: DOM differs at {path}"); clean = False; continue
        b, a = before[path], after[path]
        for prop in b:
            if b[prop] == a[prop]:
                continue
            side = prop.split("-")[1] if prop.startswith("border-") else None
            # The theme sets border-color on every element; it only shows where a border is drawn.
            if prop.endswith("-color") and side and b[f"border-{side}-width"] == "0px" and a[f"border-{side}-width"] == "0px":
                continue
            print(f"{before_file.stem}: {path}\n    {prop}: {b[prop]} -> {a[prop]}"); clean = False
print("CLEAN" if clean else "DIFFERENCES FOUND")
```

- [ ] **Step 3: List the screens**

Sign in once and collect real IDs. Click into one stack, one of its templates, one run of that template, and one registry template. After each click, run `--probe 'location.pathname'` and record `{stackId}`, `{stackTemplateId}`, `{runNumber}` and `{sourceTemplateId}`. If the local database has no stack with a template and a run, create them through the UI first.

Write `/tmp/audit/screens.txt`, one `name path` pair per line:

```
stacks /stacks
stacks-new /stacks/new
stack /stacks/{stackId}
stack-templates /stacks/{stackId}/templates
stack-templates-new /stacks/{stackId}/templates/new
template-runs /stacks/{stackId}/templates/{stackTemplateId}/runs
template-run /stacks/{stackId}/templates/{stackTemplateId}/runs/{runNumber}
template-variables /stacks/{stackId}/templates/{stackTemplateId}/variables
template-credentials /stacks/{stackId}/templates/{stackTemplateId}/credentials
template-settings /stacks/{stackId}/templates/{stackTemplateId}/settings
template-upgrade /stacks/{stackId}/templates/{stackTemplateId}/upgrade
environment /stacks/{stackId}/environment
access /stacks/{stackId}/access
registry /templates
registry-new /templates/new
registry-template /templates/{sourceTemplateId}
not-found /no-such-page
styleguide /styleguide
```

- [ ] **Step 4: Capture "before" from `chore/react-19`**

From the repository root:

```bash
git worktree add /tmp/audit/before-tree chore/react-19
(cd /tmp/audit/before-tree/web && npm ci)
docker compose stop web        # frees port 5173, the only redirect URI Keycloak accepts
(cd /tmp/audit/before-tree/web && npm run dev) &   # port 5173
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
  --remote-debugging-port=9222 --user-data-dir=/tmp/audit/chrome --window-size=1512,950 about:blank &
mkdir -p /tmp/audit/before
while read -r name path; do
  node scripts/drive-web.mjs --goto "$path" --probe "$(cat /tmp/audit/dump.js)" > "/tmp/audit/before/$name.json"
done < /tmp/audit/screens.txt
node scripts/drive-web.mjs --signed-out --goto /signin --probe "$(cat /tmp/audit/dump.js)" > /tmp/audit/before/signin.json
node scripts/drive-web.mjs --goto /stacks --shot /tmp/audit/before-stacks.png
```

Use the patched `scripts/drive-web.mjs` from the feature branch (the command runs from the main checkout), and stop the dev server afterwards. The Chrome profile holds the session, so for `signin` quit Chrome and restart it with a fresh `--user-data-dir=/tmp/audit/chrome-out`.

- [ ] **Step 5: Capture "after" from this branch**

Run the same loop with `npm run dev` started from `web/` on `feat/shadcn-foundation`, writing to `/tmp/audit/after/` and `/tmp/audit/after-stacks.png`.

- [ ] **Step 6: Apply the known `body` fix, then diff**

`theme.css` paints `body` with `bg-background text-foreground`, so every legacy screen's inherited text colour moves from `#0F172A` to `#0a0a0a`. In `web/src/styles/base.css`, add to the existing `body { … }` rule:

```css
  /* Preflight audit: theme.css paints body with shadcn's colours; legacy
     screens inherit theirs from :root until PR 9 deletes this file. */
  color: inherit;
  background: none;
```

Re-capture "after" (Step 5), then run: `python3 /tmp/audit/diff.py /tmp/audit/before /tmp/audit/after`

- [ ] **Step 7: Restore each remaining difference**

For each reported element, find the legacy class that owns it (the last class in its path, or the nearest ancestor class for an unclassed element), and add a rule to the legacy file that defines that class. Each rule must:
- target that class, never a bare element (`.stack-template-state p`, not `p`);
- restore with `revert`, which means "what the browser did" and is exactly what Preflight took away;
- start its comment with `Preflight audit:`.

For example, if `stack-template-state > p` shows `margin-top: 16px -> 0px`, add to `features.css`:

```css
/* Preflight audit: this copy relied on the browser's paragraph margins. */
.stack-template-state p {
  margin-block: revert;
}
```

Re-capture "after" and re-run the diff until it prints `CLEAN`. The one expected exception is a change on `/styleguide` inside `[data-testid="sg-theme"]`, which is new in Task 6. Ignore paths under that element.

- [ ] **Step 8: Run the suite, look, and clean up**

Run: `npm test && npm run build`
Expected: all pass. The dead-CSS guard confirms each new selector's class exists, and the old style guard confirms no new raw sizes.

Compare `/tmp/audit/before-stacks.png` with `/tmp/audit/after-stacks.png`. Expected: identical, apart from the page glow being gone.

Then, from the repository root: stop Chrome and the dev server, run `docker compose start web` and `git worktree remove --force /tmp/audit/before-tree`.

- [ ] **Step 9: Commit**

```bash
git add ../scripts/drive-web.mjs src/styles src/dev
git commit -m "fix(web): restore browser defaults legacy screens relied on under preflight" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Spec amendments, full verification and the PR

**Files:**
- Modify: `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`

- [ ] **Step 1: Bring the spec in line with what was built**

Make these edits to the spec:
- **Decisions table, Fonts row:** "Geist and Geist Mono from `@fontsource-variable/geist` and `@fontsource-variable/geist-mono`. Variable fonts carry every weight shadcn uses and are bundled at build time; the vendored 400-weight files and `scripts/vendor-fonts.sh` are removed."
- **Decisions table, shadcn preset row:** "`shadcn init --base base --preset nova`, which `components.json` records as `"style": "base-nova"`. `cn` comes from the `cn` package, re-exported by `src/lib/utils.ts`."
- **"Rename the old tokens":** there are five legacy files, not four; add `src/dev/styleguide.css`, which `StyleGuide.tsx` imports directly and which is wrapped in `@layer legacy { … }`. The rename covers 77 names: every custom property the legacy sheets declare or read, including `--data-table-min-width` (declared in `features.css`) and `--reveal-delay` (read in `base.css`, never set).
- **Theme rules:** add "`@custom-variant dark (&:is(.dark *));` stays, so shadcn's `dark:` classes never follow the OS setting" and "`--color-black` and `--color-white` stay defined, because vendored components use them; the class guard bars app code from them."
- **Migration table, PR 1 row:** add "removes `body > *`, which only lifted content above the glow" and "adds the Button to `/styleguide`".
- **Status line:** "Guard and testing sections reviewed with PR 1."

- [ ] **Step 2: Full verification**

Run from `web/`:

```bash
npx tsc -b
npm test
npm run build
```

Expected: `tsc` silent; every test passes; the build succeeds. Note the test count and the CSS and JS sizes the build prints, for the PR description.

Run from the repository root: `docker compose build web`
Expected: the image builds. This proves Tailwind's native binaries install on Alpine from this lockfile. If `npm ci` fails on a missing `@tailwindcss/oxide-linux-*-musl` or `lightningcss-linux-*-musl`, run `npm install` once more on macOS with npm 10+ so the lockfile records every platform's optional binary, and rebuild.

- [ ] **Step 3: Commit**

```bash
git add ../docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md
git commit -m "docs: record what PR 1 of the shadcn migration settled" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 4: Push and open the PR against `chore/react-19`**

```bash
gh auth status
git push -u origin feat/shadcn-foundation
gh pr create --base chore/react-19 --head feat/shadcn-foundation \
  --title "feat(web): tailwind and shadcn foundation beside a legacy layer"
```

The body must cover:
- that this PR is stacked on #273 and implements PR 1 of the spec;
- the behaviour change: the page glow is removed;
- configuration impact: new devDependencies and the removed vendored fonts;
- validation: the commands above, the layer-order check from Task 4 Step 9, the Preflight audit's `CLEAN` result, and the `/styleguide` and `/stacks` before/after screenshots;
- a final line: `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

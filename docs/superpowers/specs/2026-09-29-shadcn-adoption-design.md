# Adopt shadcn/ui in `web/`

**Date:** 2026-09-29
**Status:** Approved. Guard and testing sections reviewed with PR 1, which
also settled the details recorded below.

## Problem

`web/` has no component library. Buttons, panels and tables are plain elements
with classes from `primitives.css`, and every screen adds its own rules to
`features.css` (1,420 lines). Three costs follow:

- **Interactive widgets are hand-built and miss accessibility behaviour.** The
  user search in `StackAccessScreen.tsx` (lines 185-215) is a dropdown that
  handles only Escape: no arrow-key navigation, and no `combobox` / `listbox`
  roles. Every future dialog, menu or popover would be hand-built the same way.
- **New screens need new CSS.** There is no set of ready-made components to
  assemble, so each screen invents class names and rules.
- **The component vocabulary is private.** Contributors and agents have to learn
  this repo's classes instead of using components they already know.

## Goals

1. Focus, keyboard, ARIA and positioning behaviour come from a maintained
   library, not hand-written code.
2. New screens are built from shadcn components and Tailwind utilities, with no
   new CSS files.
3. The app uses shadcn's stock neutral look, with `#0052FF` as the primary
   colour.
4. The guarantees the current style system enforces mechanically survive the
   switch: raw colours live in one file, text meets WCAG AA contrast, off-scale
   values fail a test, and tables use fixed column layout.

## Non-goals

- **Dark mode.** Light theme only; there is no `.dark` block. shadcn's variable
  setup makes adding it later a matter of defining a second set of colours.
- **Redesigning flows or layouts.** Each screen keeps its structure, content and
  behaviour; only the component layer and the look change. The one exception is
  the stack-access user search, which gains keyboard and ARIA behaviour.
- **Introducing ESLint.** The new guards are Vitest tests, like the existing
  `styles.guard.test.ts` and `tables.guard.test.ts`.
- **Merging `feat/web-layout-primitives`.** That branch implements the
  2026-08-15 layout-primitives spec but was never merged. Tailwind's flex and
  grid utilities, together with the arbitrary-value guard below, take over its
  goals, so the branch should be closed.

## Reversing the 2026-08-15 decision

`2026-08-15-web-layout-primitives-design.md` rejected Tailwind for two reasons.
This design keeps Tailwind but answers both:

| 2026-08-15 objection | Answer in this design |
|---|---|
| Arbitrary values such as `grid-cols-[minmax(280px,0.85fr)]` are idiomatic Tailwind, so hand-tuned layout gets terser instead of prevented. | The class guard fails on any arbitrary value in app code. Hand-tuned values stay a failing test, not a review comment. |
| Tailwind throws away the AA-contrast reasoning in `tokens.css`. | The theme guard computes contrast for every foreground/background pair in the theme. Today's test only pins four hex values; the new one checks the actual ratios. |

## Decisions

| Topic | Choice | Reason |
|---|---|---|
| Primitives library | Base UI (`@base-ui/react`) | shadcn's default since July 2026. New docs, blocks and `shadcn add` output lead with it. |
| shadcn preset | `shadcn init --base base --preset nova`, which `components.json` records as `"style": "base-nova"`; base colour `neutral`, CSS variables on. `cn` comes from the `cn` package, re-exported by `src/lib/utils.ts`. | The CLI's stock setup. |
| React | 19 | shadcn's components no longer use `forwardRef`; under React 18, refs would not reach the DOM through them. |
| File locations | `src/components/ui/` for shadcn components, `src/lib/utils.ts` for `cn()`, `@/` alias to `src/` | shadcn's defaults, which the CLI, its docs and agents all expect. App-specific shared components stay in `src/shared/`. |
| Icons | `lucide-react` | Already a dependency, and shadcn's default. |
| Fonts | Geist and Geist Mono from `@fontsource-variable/geist` and `@fontsource-variable/geist-mono`, bundled at build time; the vendored 400-weight files and `scripts/vendor-fonts.sh` are removed | shadcn uses `font-medium` and `font-semibold`. Variable fonts carry every weight; with only the 400 face shipped, browsers render 500 as 400 and fake 600 by smearing the glyphs. |
| Radius | shadcn's `0.625rem` | Stock look. |
| Dark mode | None | Non-goal. |

## Theme: `src/styles/theme.css`

`theme.css` replaces `tokens.css` as the only file allowed to contain raw colour
values. It holds shadcn's variables in hex, not the CLI's `oklch()`, so the
contrast guard can compute ratios directly and the file follows the existing
hex convention.

| Variable | Value | Note |
|---|---|---|
| `--background`, `--card`, `--popover` | `#ffffff` | stock |
| `--foreground`, `--card-foreground`, `--popover-foreground` | `#0a0a0a` | stock |
| `--primary` | `#0052ff` | brand blue; stock is `#171717` |
| `--primary-foreground` | `#ffffff` | 5.75:1 on primary |
| `--secondary`, `--muted`, `--accent` | `#f5f5f5` | stock |
| `--secondary-foreground`, `--accent-foreground` | `#171717` | stock |
| `--muted-foreground` | `#525252` | stock `#737373` is 4.35:1 on `--muted`, below AA; `#525252` is 7.17:1 |
| `--destructive` | `#b91c1c` | carried over from `--color-danger` |
| `--success` | `#166534` | carried over; new variable |
| `--warning` | `#92400e` | carried over; new variable |
| `--border`, `--input` | `#e5e5e5` | stock |
| `--ring` | `#0052ff` | brand blue; stock is `#a1a1a1` |
| `--radius` | `0.625rem` | stock |

Other rules:

- **Clear Tailwind's built-in palette.** The `@theme` block sets
  `--color-*: initial`, then maps only the variables above. Classes like
  `bg-blue-500` generate no CSS. `--color-black` and `--color-white` stay
  defined, because vendored components use them (`alert-dialog`'s overlay is
  `bg-black/10`); the class guard bars app code from them.
- **Keep `@custom-variant dark (&:is(.dark *));`.** shadcn components carry
  `dark:` classes, and Tailwind's default `dark:` follows the OS setting. Scoped
  to a `.dark` ancestor, which the app never sets, they stay light.
- **Leave out the chart and sidebar variables** until a component needs them.
  The CLI adds them when it installs such a component.
- **`--accent` is not the brand colour.** In shadcn it is the neutral hover
  surface. The old `--color-accent` (blue) corresponds to `--primary`.
- **No separate tint variables.** Status tints use opacity modifiers on theme
  colours (`bg-success/10 text-success`), not the old `--color-*-soft`
  variables. The brighter `--color-*-dot` variants go away too; status dots use
  the text colours, which all clear the 3:1 minimum for UI elements.
- **`components.json` sets `tailwind.css` to `src/styles/theme.css`**, so later
  `shadcn add` runs write any new variables there.

The five status tones become Badge variants:

| Tone | Variant | Colour |
|---|---|---|
| settled | `success` | `--success` |
| failed | `destructive` | `--destructive` |
| progress | `progress` | `--primary` |
| waiting | `warning` | `--warning` |
| canceled | `muted` | `--muted-foreground` on `--muted` |

The four role badges (owner, operator, approver, viewer) also become Badge
variants.

## Running old and new styles side by side

Until PR 9, unmigrated screens keep their old styles. `src/styles.css` becomes:

```css
@layer theme, base, legacy, components, utilities;
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "./styles/theme.css";
@import "./styles/tokens.css" layer(legacy);
@import "./styles/base.css" layer(legacy);
@import "./styles/primitives.css" layer(legacy);
@import "./styles/features.css" layer(legacy);
```

The first line fixes the order of the CSS cascade layers, because a layer's
position is set where it is first declared. That gives:

- the old CSS beats Tailwind's reset (Preflight, in `base`), so unmigrated
  screens look as they do today;
- Tailwind utilities beat the old CSS, so it can't override shadcn components.

PR 1 checks this order in the compiled CSS instead of assuming it.

**Rename the old tokens.** 58 of the 76 custom properties in `tokens.css` share
names with Tailwind or shadcn variables, for example `--color-accent`,
`--color-muted`, `--radius-lg`, `--shadow-sm`, `--text-sm` and `--font-mono`.
Declared in the `legacy` layer, they would override Tailwind's theme layer on
`:root`, and shadcn components would render with the old values: `bg-accent`
would hover blue, and `rounded-lg` would be 8px. PR 1 prefixes every old token
with `--legacy-` in the five legacy files (`tokens.css`, `base.css`,
`primitives.css`, `features.css`, and `src/dev/styleguide.css`, which
`StyleGuide.tsx` imports directly and which wraps itself in `@layer legacy { … }`)
and in `dev/StyleGuide.tsx`, the only TSX file that reads them. The rename covers
77 names: every custom property the legacy sheets declare or read, including
`--data-table-min-width` (declared in `features.css`) and `--reveal-delay` (read
in `base.css`, never set).

**Restore what Preflight takes.** PR 1 diffed the computed styles of all 19
screens before and after adding Tailwind. Five restorations brought the diff to
zero: `body` keeps its inherited legacy colours instead of `theme.css`'s, and four
browser defaults come back where legacy markup relied on them. Each restoration
is written `:where(<legacy class> …) { <property>: revert; }`: `revert` is exactly
the browser default Preflight removed, and `:where()` gives it browser-default
precedence, so every existing legacy rule still wins over it.

**Known leak, accepted until PR 9.** `base.css` styles bare elements (`h1`–`h4`,
`a`, `button`, `input`, `select`, `textarea`, `code`). Those rules still apply on
migrated screens for any property the Tailwind classes don't set.

**Mixed look, accepted until PR 8.** Between PR 2 and PR 8, `main` shows
migrated and unmigrated screens side by side.

## Migration order

| PR | Scope | Main shadcn pieces |
|---|---|---|
| 0 | Upgrade React 18 to 19, nothing else | none |
| 1 | Tooling, theme, layer setup, token rename, Fontsource Geist, new guards, Preflight restorations; removes the page glows (`body::before`, and `body > *`, which only lifted content above them); adds the Button to `/styleguide`. No screen changes. | Button |
| 2 | App shell (`AppShell`) and `src/shared/` components | Breadcrumb; Badge (status tones, roles); Card (`StatBand`, `IdsPanel`); Collapsible (`LogSteps`) |
| 3 | Standalone screens: sign-in, access denied, not found, service unavailable, route placeholder | Card, Button, Alert |
| 4 | Stacks: list, create, detail shell, environment, credentials | Table, Input, Label, Button |
| 5 | Stack access | Combobox for the user search; Select for the role picker |
| 6 | Stack templates: list, detail shell and its four tabs, config panel, variable fields, add, upgrade | Tabs tied to the route, so each tab keeps its URL; Select; Textarea |
| 7 | Template registry: registry, registration, detail | Table, Card, Input |
| 8 | Runs: detail, logs, history, actions, destroy panel | AlertDialog for the destroy confirmation; ScrollArea for logs |
| 9 | Cleanup: delete `tokens.css`, `base.css`, `primitives.css`, `features.css`, the `legacy` layer, the old guards and the dead-CSS guard | none |

`HeroGraphic` (the decorative shapes on error and empty-state screens) has no
counterpart in the stock look. Each migrating screen drops it for a plain empty
state, and the component file goes when its last user migrates (PR 7:
`TemplateRegistryScreen`), along with its `/styleguide` section.

### What every screen PR must do

1. Leave no legacy class names in the files it touches; those files use only
   shadcn components and Tailwind classes.
2. Delete the legacy CSS rules it stopped using. The dead-CSS guard fails
   otherwise.
3. Rewrite tests that find elements by legacy class names (14 test files today,
   e.g. `.templates-list__name`, `.status-tone`) to query by role, text or test
   ID.
4. Add each newly installed shadcn component to the `/styleguide` dev page.
5. Include before/after screenshots in the PR description. The local stack
   (`docker compose --profile auth up` plus `npm run dev`) serves every screen,
   and `scripts/drive-web.mjs` can take them through a real Keycloak login.

## Guards

All guards are Vitest tests under `web/src/styles/`, scanning source files with
regexes, as the existing guards do.

**Theme guard** (`theme.guard.test.ts`, PR 1)
- Every variable in the theme table above is defined in `theme.css`.
- WCAG contrast, computed from the hex values: at least 4.5:1 for each text
  pair (foreground on background, card and popover; primary-foreground on
  primary; secondary- and accent-foreground on their surfaces; muted-foreground
  on muted and on background; primary, success, warning and destructive on
  background; each status colour on its own 10% tint composited over
  background). At least 3:1 for `--ring` on background, since it
  draws the focus indicator.
- Replaces the "uses the AA-safe status colours for text" test.

**Class guard** (`classes.guard.test.ts`, PR 1). Scans `src/**/*.tsx`, excluding
tests and `src/components/ui/`. The shadcn components are exempt because they
use arbitrary values themselves, such as `ring-[3px]`.
- **No arbitrary values:** no class ending in `-[...]` or `-(...)`, and no
  arbitrary property such as `[mask-type:luminance]`. Arbitrary *variants*
  (`data-[state=open]:`, `has-[>svg]:`) are selectors, not values, and are
  allowed.
- **No built-in palette colours:** no class naming a Tailwind colour scale
  (`slate`, `gray`, …, `rose` followed by a step) or `white`/`black`. These
  generate nothing once the palette is cleared, so the guard turns a silent
  no-op into a failure.

**Raw-colour guard** (PR 1). No hex, `rgb()`, `hsl()` or `oklch()` literals in
any `.css` or `.tsx` file except `theme.css`. `tokens.css` is also exempt until
PR 9.

**Table guard** (rewrite of `tables.guard.test.ts`, lands with PR 4, the first
Table migration)
- No raw `<table` outside `src/components/ui/`.
- Every `<Table>` has a `<colgroup>`.
- `src/components/ui/table.tsx` is edited to always apply `table-fixed`. The
  reason is unchanged: with automatic layout, a status changing from `queued`
  to `waiting_approval` shifts every column after it.

**Dead-CSS guard** (PR 1, deleted in PR 9). Every class selector in the five
legacy files appears in some `.tsx` file, either literally or as the literal
prefix of a template string (`status-tone--${tone}` counts for every
`status-tone--*`).

**Legacy-token guard** (PR 1, deleted in PR 9). The five legacy files reference
only `--legacy-*` custom properties, so an unrenamed token can't slip through.

**Existing guards.** `styles.guard.test.ts` is scoped by name to `base.css`,
`primitives.css` and `features.css`. It used to scan every CSS file in
`styles/`, which would flag `theme.css`. Its "`styles.css` contains nothing but
imports" test moves to `theme.guard.test.ts`, which also requires the leading
`@layer` statement. `styles.guard.test.ts` is deleted in PR 9.

## Testing

**jsdom stand-ins.** The default Vitest environment is `node`, and 29 test files
opt into jsdom with a pragma. A new `src/test/setup.ts`, registered as
`test.setupFiles`, adds the browser APIs Base UI and its positioning code need
but jsdom lacks, and does nothing when `window` is absent:

- `ResizeObserver`
- `Element.prototype.hasPointerCapture`, `setPointerCapture`,
  `releasePointerCapture`
- `Element.prototype.scrollIntoView`

Add others as component tests turn them up; each one gets a comment naming the
component that needed it.

**Interaction.** Tests that drive Base UI components use
`@testing-library/user-event` (already installed), not `fireEvent`, because the
components respond to pointer events that `fireEvent.click` doesn't send.

**New behaviour tests:**
- PR 5, Combobox: the input has `role="combobox"` and `aria-expanded`; typing
  filters options with `role="option"`; ArrowDown and ArrowUp move the
  highlight; Enter selects; Escape closes and clears.
- PR 5 and PR 6, Select: opens from the keyboard, arrows move, Enter commits.
- PR 8, AlertDialog: focus moves into the dialog, Tab stays inside, Cancel
  returns focus to the destroy button, and confirm fires the destroy mutation
  once.

**Every PR** runs `npm test` and `npm run build` in `web/`. PR 1 also runs
`docker compose build web`: Tailwind v4 depends on native binaries
(`@tailwindcss/oxide`, `lightningcss`), the lockfile is generated on macOS, and
`Dockerfile.web` runs `npm ci` on Alpine (musl).

## Risks

| Risk | Mitigation |
|---|---|
| The layer order doesn't behave as described once Tailwind processes the imports. | PR 1 checks the compiled CSS for the order before any screen depends on it. |
| An old token is missed by the rename and silently overrides a Tailwind variable. | Legacy-token guard. |
| `base.css` element rules leak onto migrated screens. | Accepted until PR 9; each PR's screenshots catch visible effects. |
| Base UI needs more jsdom stand-ins than listed. | Added as found, one comment each. |
| The React 19 upgrade breaks something unrelated. | It lands alone as PR 0, so its cause is unambiguous. |

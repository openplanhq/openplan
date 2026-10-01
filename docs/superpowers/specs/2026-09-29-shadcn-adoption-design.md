# Adopt shadcn/ui in `web/`

**Date:** 2026-09-29
**Status:** Approved. Guard and testing sections reviewed with PR 1. PRs 1 to
6 settled the details recorded below.

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

The four role badges map onto the same variants: owner `progress`, operator
`success`, approver `warning`, viewer `muted`. A role the UI doesn't know
renders `muted`.

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
screens before and after adding Tailwind, in the states the local database could
show (one stack, one template, two completed runs). The branch review then
rendered the states that audit could not reach: loading lines, both session-error
screens, the selected-user card and the undo banner. Restorations bring both
checks to zero difference: `body` keeps its inherited legacy colours instead of
`theme.css`'s, and browser defaults come back where legacy markup relied on them
(inline icons in loading lines and non-flex buttons, panel and swatch margins,
the icon button's padding, and the class-less session-error screens, which gain
a `session-error` class to hang them on). Each restoration is written
`:where(<legacy class> …) { <property>: revert; }`: `revert` is exactly the
browser default Preflight removed, and `:where()` gives it browser-default
precedence, so every existing legacy rule still wins over it. Screen PRs should
audit the same way, and also reach loading, error and transient states, for
example by stalling or failing `/v1/*` requests over the DevTools protocol
(`scripts/drive-web.mjs --fail`).

**Known leak, accepted until PR 9.** `base.css` styles bare elements (`h1`–`h4`,
`a`, `button`, `input`, `select`, `textarea`, `code`). Those rules still apply on
migrated screens for any property the Tailwind classes don't set. Migrated
components therefore set colour, type and decoration on their own `a` and `h1`
elements: a rule on the element beats a colour it would inherit.

**Mixed look, accepted until PR 8.** Between PR 2 and PR 8, `main` shows
migrated and unmigrated screens side by side.

## Migration order

| PR | Scope | Main shadcn pieces |
|---|---|---|
| 0 | Upgrade React 18 to 19, nothing else | none |
| 1 | Tooling, theme, layer setup, token rename, Fontsource Geist, new guards, Preflight restorations; removes the page glows (`body::before`, and `body > *`, which only lifted content above them); adds the Button to `/styleguide`. No screen changes. | Button |
| 2 | App shell (`AppShell`) and `src/shared/` components; deletes the unused `StatBand` and `IdsPanel` | Breadcrumb; Badge (status tones, roles); Collapsible (`LogSteps`) |
| 3 | Standalone screens: sign-in, access denied, not found, service unavailable, route placeholder, and the two session-error screens | Card, Button, Alert, Input, Label; Empty for the route messages |
| 4 | Stacks: list, create, detail shell, environment, credentials | Table; Tabs for the stack's tab row (`RouteTabs`); reuses Card, Input, Label, Button, Alert and Empty |
| 5 | Stack access | Combobox for the user search; Select for the role picker; reuses Card, Alert, Button, Label and `RoleBadge` |
| 6 | Stack templates: list, detail shell and its four tabs, config panel, variable fields, add, upgrade | `RouteTabs` from PR 4 for the template's tabs, so each tab keeps its URL; Select; Textarea (vendored in PR 5) |
| 7 | Template registry: registry, registration, detail | Table, Card, Input |
| 8 | Runs: detail, logs, history, actions, destroy panel | AlertDialog for the destroy confirmation; ScrollArea for logs |
| 9 | Cleanup: delete `tokens.css`, `base.css`, `primitives.css`, `features.css`, the `legacy` layer, the old guards and the dead-CSS guard | none |

**What PR 2 settled.**

- `StatBand` and `IdsPanel` had no screen left rendering them, so PR 2
  deleted them instead of moving them to Card. Card arrives in PR 3.
- Status pills and role badges are `src/shared/StatusBadge.tsx` and
  `src/shared/RoleBadge.tsx`. PR 2 moved only `StatusRow` and `/styleguide`
  onto them. The legacy `status-tone` and `role-badge` CSS goes with the last
  screen that stops using it.
- `Breadcrumb` carries the page spacing below it (`mb-6`). Header rows that
  set it beside a button pass `className="mb-0"`, because a legacy rule can't
  outrank a utility.
- The app header is `z-5`, below the legacy `.search-dropdown` (10) and
  `.undo-banner` (20). `AppShell.test.tsx` checks that order until those
  overlays migrate.

**What PR 3 settled.**

- The sign-in form needed Input and Label, so they arrived in PR 3 instead
  of PR 4. The route messages use shadcn's Empty, which PR 4 and PR 7 can
  reuse for the stacks and registry empty states.
- SessionProvider's two session-error screens migrated with sign-in. No PR
  row named them, and they relied on Preflight restorations PR 9 would
  delete.
- The four route messages (not found, not permitted, service unavailable,
  the placeholder) are `src/app/RouteMessage.tsx`: Empty with a real `h1`.
  Screens outside the shell frame themselves with `src/auth/AuthCard.tsx`.
- The legacy `label`, `input`, `textarea` and `select` rules skip elements
  with a `data-slot`, written `input:where(:not([data-slot]))` so legacy
  fields keep their specificity. Without that, the legacy 36px `min-height`
  outgrew Input's `h-8`, and labels turned muted. `legacy.guard.test.ts`
  checks every such selector.
- `scripts/drive-web.mjs --fail <glob>=<status>` answers matching requests
  with an error, and `--eval` and `--reload` set up state before a fresh
  load. That is how PR 3 reached the 403, 503 and session-error screens.
  `--width <px>` emulates a phone, touch included: headless Chrome won't
  size a window below 500px, so `--window-size` can't measure a 375px
  screen.

**What PR 4 settled.**

- Tabs arrived in PR 4, not PR 6: the stack shell has the same tab row as
  the template shell. `src/shared/RouteTabs.tsx` renders shadcn's line
  Tabs with each tab a `NavLink`, Base UI's "tabs as links". The caller
  derives the active value from the pathname, the same match each
  `NavLink` makes, so the selected tab is always the one marked
  `aria-current`. PR 6 reuses it for the template's tabs, passing
  `className="mb-0"` where the row sits beside the template's state.
- On a coarse pointer each tab is `h-11` and the list `h-auto`. Both
  override vendored heights because Tailwind 4.3 compiles `group-data-*`
  inside `:where()`, so source order decides, and the `pointer-coarse`
  media block comes later.
- `table.tsx` applies `table-fixed` after the caller's `className`, since
  `cn` would let a caller's `table-auto` replace it. Tables sit in
  shadcn's bordered frame (`overflow-hidden rounded-lg border`). Cells with
  user-chosen text use `truncate` and a `title`. A row's link fills its
  cell (`block`, `pointer-coarse:py-3`), so the cell is the 44px target.
- The table guard allows a raw `<table>` only in the files
  `LEGACY_TABLES` lists (`TemplateRunHistory`, and the legacy specimen on
  `/styleguide`), held to the old `.data-table` rules. The list can only
  shrink; PR 8 empties it.
- `CredentialsPanel` is shared with a template's Credentials tab, so that
  tab's panel moved in PR 4. Its legacy `.stack-template-tab` wrapper moves
  in PR 6.
- An empty state inside a page is Empty with a `border` and a real `h2` in
  EmptyTitle's look. PR 7's registry empty state can follow it and then
  share one component.
- The dead-CSS guard can't see a class whose name survives as a test ID
  (`stacks-list`). Screen PRs check with `rg` after deleting.
- `scripts/drive-web.mjs --respond <glob>=<json>` answers matching
  requests with 200 and that body, and `--stall <glob>` never answers.
  That is how PR 4 reached empty and loading states and long names. A
  usage example in the script's header comment can't contain `*/`.

**What PR 5 settled.**

- Combobox brought InputGroup and Textarea with it, so PR 6 starts with
  Textarea vendored. `shadcn add` offers to overwrite `button.tsx`, which
  carries PR 1's `/90` hover; answer no (`yes n | npx shadcn add …`).
- The user search is a Combobox over the server's answer, with
  `filter={null}`: the server matches email as well as name. The list
  opens on the user's intent, a debounced query of two or more
  characters, and its empty row reads "Searching..." until that query
  answers, so "No users found" never shows before one has. Only typing
  sets the query; a pick filling the input doesn't search for the picked
  name.
- The pick is the Combobox's value: the input shows the picked name, and
  the selected-user card is gone. Base UI's `ComboboxClear` inside the
  input, still named "Clear selected user", replaces the card's X and
  returns focus to the input; its reason is `clear-press`. Base UI takes
  it out of the tab order, so the screen passes `tabIndex={0}`.
  `ComboboxInput` renders its own inline-end addon only when it shows the
  trigger or the clear button, so the input carries no empty padding. Typing keeps the pick, but Assign holds while the input shows
  anything other than the picked name: who gets the role is who the input
  shows. Another pick, the clear button or Escape changes the pick, and
  leaving the field puts its name back. Users who already hold a role are
  disabled options that show it.
- Base UI also resets its input after every close of the list: to the
  pick, or to empty, including closes a controlled `open` makes on its
  own, such as once the query is too short, and it wiped what the user
  had typed. An `open` that waited for the answer made more of them. So the screen owns the input text
  (`inputValue`), ignores Base UI's resets, and does its own in
  `onOpenChange` when the reason is `focus-out`, `outside-press` or
  `escape-key`: Enter with nothing highlighted closes the list with reason
  `none` and keeps the query. Leaving a list that is already shut puts the
  pick back on blur, and closes the search so a late answer can't open it.
  The search query keeps its previous answer while the next one loads, but
  only for a query that narrows it (`placeholderData` as a function), so
  refining doesn't blink back to "Searching..." and an unrelated query
  never shows the wrong people. PR 6's comboboxes over async data start from this pattern.
  Tests reach it with a delayed search answer (`serve({ searchDelay })`).
- The stack roles live in `src/shared/roles.ts` (`STACK_ROLES`,
  `StackRole`). The role picker and the style guide list them from it, and
  RoleBadge's tints are a `Record<StackRole, …>`, so a new role won't
  type-check without one.
- The user-search tests run on fake timers
  (`vi.useFakeTimers({ shouldAdvanceTime: true })`, `elapse(ms)`), so the
  debounce and slow answers cost no wall-clock time.
- Undo restores a role through its own mutation, disabled while it is out,
  so Assign never reads "Assigning..." during an undo. The undo banner's
  `role="status"` region stays in the page, empty, so screen readers
  announce the message when it arrives.
- While a Combobox popup is open, Base UI hides the rest of the page from
  assistive technology, so tests query outside the popup only once it has
  closed. `ComboboxEmpty` is `role="status"`; tests find the undo banner,
  also a status, by its text.
- Select's trigger height is `data-[size=default]:h-8`, which a plain
  `pointer-coarse:h-11` can't outrank; `pointer-coarse:data-[size=default]:h-11`
  does. The search input fills its taller group on a coarse pointer
  through `pointer-coarse:*:data-[slot=input-group-control]:h-full`.
  Audits measure the InputGroup, not the input inside its border, and skip
  Base UI's hidden form inputs: they have no `data-slot`, so the legacy
  input `min-height` stretches them to 36px, but they are `aria-hidden` and
  clipped to nothing.
- The undo banner is a Tailwind toast (fixed, `role="status"`, the
  popover look), not Sonner. `AppShell.test.tsx` now holds the header
  (`z-5`) below Base UI's popups (`z-50`) and the banner (`z-20`), the
  overlays that replaced `.search-dropdown` and `.undo-banner`.
- `role-badge` CSS went with this screen, its last user.
- A grant row's name has `grow basis-32`, not `flex-1`: a zero basis never
  forces a wrap, so at 1,080 to 1,280px the confirm row squeezed the name
  to 8 to 94px. With a 128px basis the actions wrap under it, and a
  resting row still fits on one line on a 375px phone.
- Neither Combobox nor Select needed a jsdom stand-in.
- Rebasing onto the Dex change had dropped `scripts/drive-web.mjs`'s phone
  emulation and its `--signed-out` check: `--width` emulated nothing and
  `--signed-out` signed in anyway. PR 5 restored both. To type into a
  Base UI input from `--eval`, use `document.execCommand("insertText", …)`;
  setting `value` and dispatching `input` doesn't open the list. Phone
  probes compare `scrollWidth` with `document.documentElement.clientWidth`:
  on a mobile viewport `innerWidth` grows with any overflow, and hides it.
  Screenshots of an open Base UI popup are taken viewport-only: the full-page
  `--shot` (`captureBeyondViewport`) drew the Combobox popup 140px off.

**What PR 6 settled.**

- A template's tabs are `RouteTabs` (`Template sections`). Runs stays
  selected for everything under `runs/`, run detail included. The
  template's state is a `StatusBadge` at the far end of the same row, and
  on a phone it wraps under the tabs. Tab links have `role="tab"`, so tests
  query `getByRole("tab")` and check `aria-current`, not a link's class.
- The shell and tab grids that hold the legacy run components use
  `grid-cols-1`, which is `minmax(0, 1fr)`. A bare `grid` has an `auto`
  column, which grew to the run table's 824px minimum and widened the page
  by 468px at 375px. With `minmax(0, 1fr)` the table scrolls inside its
  `.data-table-frame`.
- `toneTextClass(tone)` in `src/shared/statusTone.ts` gives a tone's text
  colour outside a pill, as its Badge variant uses it. The template list's
  state glyph and the "will be dropped" notes use it.
- The install picker's rows are their own Tailwind-styled buttons: the
  chosen row takes the primary border and ring, and a row with no active
  revision is disabled with its `StatusBadge`. The registry still uses
  `.templates-list*` and `.templates-group*`, so that CSS stays until
  PR 7, which can share a row component with the picker.
- Variable inputs take their ids from `useId`, two columns from `md`.
- Revision Selects take `items` as `{ value, label }`, so `SelectValue`
  shows the label. Tests read the options from the listbox and the shown
  value from `[data-slot="select-value"]`.
- Rules whose class names survive as test IDs (`stack-template-items`,
  `stack-template-state`, `stack-template-detail`,
  `stack-template-list-content`, `stack-template-revision-action`) were
  deleted by hand, out of the dead-CSS guard's sight.
- The run components on the Runs and Settings tabs (`TemplateRunActions`,
  `TemplateRunHistory`, `TemplateDestroyPanel`) stay legacy until PR 8.
  Until then their Plan, Apply and Destroy buttons are under 44px on a
  phone.

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
   (`docker compose up`) serves every screen; its `web` service is a built
   image, so rebuild it (`docker compose build web`) before an audit.
   `scripts/drive-web.mjs` takes screenshots through a real sign-in as Dex's
   static user, `admin@openplan.local` (docs/authentication.md).
6. Give each control a 44px target on coarse pointers, as
   `--legacy-touch-target` did: `pointer-coarse:h-11` on fixed-height
   controls, `pointer-coarse:min-h-11` on rows whose height comes from their
   content.
7. Replace its inline `status-tone` and `role-badge` markup with
   `StatusBadge` and `RoleBadge` from `src/shared/`.

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
- No raw `<table` outside `src/components/ui/`, except in the files
  `LEGACY_TABLES` lists, which keep the legacy `.data-table` rules until PR 8
  moves them.
- Every `<Table>` opens with a `<colgroup>`.
- `src/components/ui/table.tsx` is edited to always apply `table-fixed`, after
  the caller's `className`, so `cn` can't let a `table-auto` replace it. The
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
- PR 4, RouteTabs: Tab reaches only the active tab; ArrowLeft and ArrowRight
  move focus without navigating; Enter follows the focused tab; on every stack
  route the selected tab is the one its link marks `aria-current`.
- PR 5, Combobox: the input has `role="combobox"` and `aria-expanded`; typing
  searches, and the options (`role="option"`) are exactly the server's answer;
  ArrowDown and ArrowUp move the highlight; Enter selects; Escape closes and
  clears.
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

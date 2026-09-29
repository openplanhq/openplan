# App Shell and Shared Components on shadcn (PR 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the app shell (`AppShell`) and the `src/shared/` components every screen renders onto shadcn components and Tailwind utilities, and delete the legacy CSS they stop using.

**Architecture:** The shadcn CLI vendors Badge, Breadcrumb and Collapsible into `src/components/ui/`. Badge gets four tinted variants, which carry the status tones and the stack roles. The `src/shared/` wrappers (`Breadcrumb`, `LogSteps`, `StatusRow`, plus the new `StatusBadge` and `RoleBadge`) keep their current props, so screens don't change. Only their markup moves to shadcn components and Tailwind classes. `AppShell` is rebuilt with Tailwind and the shadcn Button. `StatBand` and `IdsPanel` are deleted: no screen renders either one. Each task deletes the legacy CSS rules its component stops using.

**Tech Stack:** React 19, Tailwind CSS 4.3, shadcn CLI 4.21 (`base-nova` style on Base UI 1.8), `class-variance-authority`, `cn`, lucide-react, Vitest 4 with Testing Library and `@testing-library/user-event`.

**Spec:** `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`. Read "Theme" (the tone-to-variant table), "Running old and new styles side by side" (the layer order and the known `base.css` leak), "What every screen PR must do" and "Guards" before starting.

## Global Constraints

- Branch `feat/shadcn-shell-shared`, created from `feat/shadcn-foundation` (PR #274, itself stacked on #273). The PR base is `feat/shadcn-foundation`.
- All commands run from `web/` unless a step says otherwise.
- Screens keep their structure, content and behaviour. PR 2 edits screen files for one reason only: the three header rows pass `className="mb-0"` to `Breadcrumb` (Task 4). Screen tests change only where they matched the old breadcrumb markup.
- Components are added only with `npx shadcn add`. Vendored files in `src/components/ui/` are edited only where a step says so (Task 2 adds Badge variants).
- App code uses theme colours and Tailwind's scale steps only: no arbitrary values (`w-[37px]`) and no built-in palette colours (`bg-blue-500`). `src/styles/tailwind.guard.test.ts` fails otherwise.
- Status tones map to Badge variants exactly as the spec's table says: settled → `success`, failed → `destructive`, progress → `progress` (`--primary`), waiting → `warning`, canceled → `muted`.
- Roles reuse those variants: owner → `progress`, operator → `success`, approver → `warning`, viewer → `muted`. A role the UI doesn't know renders `muted`.
- `base.css` still styles bare `a`, `a:hover`, `h1`, `button`, `code` and `pre` until PR 9, from the `legacy` layer. A rule on an element beats a colour it would inherit. So every migrated `a` sets its own text colour and `hover:no-underline`, and every migrated `h1` sets its own font family, size, weight and tracking.
- Migrated controls keep the 44px target on coarse pointers that `--legacy-touch-target` gave them. Fixed-height controls use `pointer-coarse:h-11`; rows whose height comes from their content use `pointer-coarse:min-h-11`.
- Each task deletes the legacy rules it stopped using. The dead-CSS guard (`src/styles/legacy.guard.test.ts`) catches most of them. It can't catch a class whose name also appears in some `.tsx` file as a word: `breadcrumb` appears in comments, and `identity-menu` and `debug-panel` are test IDs. Delete those rules by hand as the tasks list.
- Tests find elements by role, text, test ID or `data-*` attribute, never by legacy class name. Tests that click Base UI components use `@testing-library/user-event`.
- No `src/test/setup.ts` is needed. A probe of Base UI's Collapsible under jsdom (open, close, reopen) touched no missing browser API. Add the file, as the spec's Testing section describes, only if a test fails on one.
- Commit subjects are lowercase-prefixed (`feat(web):`, `refactor(web):`, `test(web):`, `docs:`). Every commit message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **The API returns a role the UI doesn't know,** say a new `auditor`, or `constructor`, which a plain-object lookup would resolve to `Object.prototype.constructor`. Expected: a muted badge that names the role, with no crash and no stray variant. Task 3 tests both.
2. **The page is scrolled while a legacy overlay is open:** the stack-access search dropdown (`z-index: 10`) or the undo banner (`20`). Expected: the overlay paints above the sticky header. Task 6 adds a test that reads both z-index values and the header's.
3. **A touch-screen user taps the nav, Log out, or a log row.** Expected: a 44px target, as today. Tasks 5 and 6 pin the `pointer-coarse:` classes.
4. **A long tenant ID (config allows 128 characters) or a long stack or template name on a 375px screen.** Expected: the text wraps and the page never scrolls sideways. Tasks 4 and 6 pin `wrap-anywhere`; Task 7 measures `scrollWidth` at phone width.
5. **A keyboard user's first Tab.** Expected: "Skip to content" appears above the sticky header, and following it moves focus into `<main>`. Task 6 tests the link and its target; Task 7 checks it becomes visible when focused.

---

### Setup: branch and plan commit

- [ ] **Step 1: Create the branch and commit this plan**

From the repository root:

```bash
git switch feat/shadcn-foundation
git pull --ff-only
git switch -c feat/shadcn-shell-shared
git add docs/superpowers/plans/2026-09-29-shadcn-shell-shared.md
git commit -m "docs: implementation plan for the shell and shared components (PR 2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 1: Delete the unused `StatBand` and `IdsPanel`

`StatBand` was taken off the stacks screen in `3bb3127` and now renders only on `/styleguide`. `IdsPanel` has had no importer since the dev-only debug panel in `AppShell` replaced it. The spec's PR 2 row planned to move both onto Card. Deleting them is what the spec amendment in Task 7 records.

**Files:**
- Delete: `web/src/shared/StatBand.tsx`, `web/src/shared/StatBand.test.tsx`, `web/src/shared/IdsPanel.tsx`
- Modify: `web/src/dev/StyleGuide.tsx`
- Modify: `web/src/styles/primitives.css`

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. After this task, no file imports `StatBand` or `IdsPanel`.

- [ ] **Step 1: Confirm nothing else renders them**

Run: `rg -n "StatBand|IdsPanel" src -g '!src/shared/StatBand*' -g '!src/shared/IdsPanel.tsx'`
Expected: only `src/dev/StyleGuide.tsx` (its import and the specimen).

- [ ] **Step 2: Delete the components and their styleguide specimens**

```bash
git rm src/shared/StatBand.tsx src/shared/StatBand.test.tsx src/shared/IdsPanel.tsx
```

In `web/src/dev/StyleGuide.tsx`:
- delete the line `import StatBand from "../shared/StatBand";`
- delete the `SECTIONS` entry `{ id: "stats", title: "Stat band" },`
- in the "Panels" section, delete the inverted panel specimen. `StatBand` was the only thing in the app that used it:

```tsx
              <section className="panel panel--inverted">
                <h2>Inverted</h2>
                <p>Dark surface with a dot texture, used for emphasis bands.</p>
              </section>
```

- delete the whole "Stat band" section:

```tsx
        <Section id="stats" title="Stat band">
          <Specimen label="StatBand" hint="real component" stack>
            <StatBand
              items={[
                { label: "Stacks", value: 4 },
                { label: "You can operate", value: 3 }
              ]}
            />
          </Specimen>
        </Section>
```

- [ ] **Step 3: Run the dead-CSS guard to see what is now unused**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `styles/primitives.css styles only classes a component uses`, with `delete the rules for: panel--inverted, stat-band, stat-band__grid, stat-band__item, stat-band__value, stat-band__label`.

- [ ] **Step 4: Delete those rules**

In `web/src/styles/primitives.css`, delete the three `.panel--inverted` rules (`.panel--inverted { … }`, `.panel--inverted::before { … }`, `.panel--inverted > * { … }`). Also delete the whole stat band block: the `/* ---- Stat band ---- */` comment and the rules `.stat-band`, `.stat-band__grid`, the `@media (min-width: 768px) { .stat-band__grid { … } }` block, `.stat-band__item`, `.stat-band__value` and `.stat-band__label`. It ends right before `/* ---- Hero graphic ---- */`.

- [ ] **Step 5: Run the guards, the styleguide test and the type-check**

Run: `npx vitest run src/styles src/dev && npx tsc -b`
Expected: all pass; `tsc` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add -A src/shared src/dev/StyleGuide.tsx src/styles/primitives.css
git commit -m "refactor(web): delete the unused StatBand and IdsPanel" -m "Neither renders on any screen: the stacks stat band was dropped in 3bb3127 and the debug panel replaced IdsPanel. Their CSS, and the inverted panel only StatBand used, go with them." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Vendor Badge, Breadcrumb and Collapsible; add the tone variants

**Files:**
- Create (by the CLI): `web/src/components/ui/badge.tsx`, `web/src/components/ui/breadcrumb.tsx`, `web/src/components/ui/collapsible.tsx`
- Modify: `web/src/components/ui/badge.tsx` (four variants)
- Test: `web/src/styles/theme.guard.test.ts`

**Interfaces:**
- Consumes: the theme colours in `src/styles/theme.css`.
- Produces:
  - `import { Badge, badgeVariants } from "@/components/ui/badge"`. `Badge` renders a `<span data-slot="badge" data-variant="<variant>">`. Its variants are the stock `default`, `secondary`, `destructive`, `outline`, `ghost` and `link`, plus the new `success`, `progress`, `warning` and `muted`.
  - `import { Breadcrumb, BreadcrumbList, BreadcrumbItem, BreadcrumbLink, BreadcrumbPage, BreadcrumbSeparator, BreadcrumbEllipsis } from "@/components/ui/breadcrumb"`. `BreadcrumbLink` takes a Base UI `render` prop, for example `render={<Link to="/x" />}`. `BreadcrumbSeparator` is an `<li role="presentation" aria-hidden="true" data-slot="breadcrumb-separator">` holding a chevron.
  - `import { Collapsible, CollapsibleTrigger, CollapsibleContent } from "@/components/ui/collapsible"`. These wrap Base UI's `Collapsible.Root`, `.Trigger` and `.Panel`. `open` and `onOpenChange` control the state. The trigger is a `<button>` carrying `aria-expanded`, and `aria-controls` while open. The panel unmounts while closed. All three take `render`.

Card is not added: with `StatBand` and `IdsPanel` gone, nothing in PR 2 needs it. PR 3 adds it.

- [ ] **Step 1: Write the failing guard**

In `web/src/styles/theme.guard.test.ts`, replace the whole `describe("Button variants on theme.css", …)` block, along with the comment above it, with:

```ts
// The theme's values pass on their own, but components paint them translucent:
// hover:bg-primary/80 under white text is 4.11:1 with the brand blue. Read the
// alphas out of the components so a regenerated one is re-checked.
const uiSource = (file: string) => readFileSync(join(STYLES_DIR, "..", "components", "ui", file), "utf8");
const variantIn = (source: string) => (name: string) =>
  source.match(new RegExp(`\\b${name}:\\s*"([^"]*)"`))?.[1] ?? "";

function alpha(classes: string, utility: string): number {
  const match = classes.match(new RegExp(`(?:^|\\s)${utility}/(\\d+)(?:\\s|$)`));
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

  // TEXT_PAIRS above already checks muted-foreground on muted.
  it("paints the muted badge with a checked pair", () => {
    expect(variant("muted").split(/\s+/)).toEqual(expect.arrayContaining(["bg-muted", "text-muted-foreground"]));
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `npx vitest run src/styles/theme.guard.test.ts`
Expected: FAIL with `ENOENT … components/ui/badge.tsx`. The file is read while the tests are being collected, so the whole file fails, Button tests included, until Step 3 creates it.

- [ ] **Step 3: Vendor the three components**

Run: `npx shadcn add badge breadcrumb collapsible --yes`

Then run: `git status --short`
Expected: exactly three new files, `src/components/ui/badge.tsx`, `src/components/ui/breadcrumb.tsx` and `src/components/ui/collapsible.tsx`. `cn` is already a dependency, so `package.json` and `package-lock.json` should be unchanged. If the CLI changed any other file, restore it with `git checkout -- <file>`.

- [ ] **Step 4: Run the guard again**

Run: `npx vitest run src/styles/theme.guard.test.ts`
Expected: FAIL for `success`, `progress`, `warning` and `muted`, because stock Badge has none of them. `destructive` passes.

- [ ] **Step 5: Add the tinted variants**

In `web/src/components/ui/badge.tsx`, add these four entries to `variants.variant`, directly after the `link:` entry:

```ts
        // Status tones and roles: src/shared/StatusBadge.tsx and RoleBadge.tsx.
        // theme.guard.test.ts checks each text colour on its tint.
        success: "bg-success/10 text-success",
        progress: "bg-primary/10 text-primary",
        warning: "bg-warning/10 text-warning",
        muted: "bg-muted text-muted-foreground",
```

- [ ] **Step 6: Run the guards and the type-check**

Run: `npx vitest run src/styles && npx tsc -b`
Expected: all pass. The class guard skips `src/components/ui/`, so the vendored arbitrary values (`ring-[3px]`) don't trip it.

- [ ] **Step 7: Commit**

```bash
git add src/components/ui src/styles/theme.guard.test.ts
git commit -m "feat(web): add the shadcn Badge, Breadcrumb and Collapsible" -m "Badge gains success, progress, warning and muted: the status tones and roles as tints of theme colours. The theme guard reads their alphas and checks each text colour on its tint." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `StatusBadge`, `RoleBadge`, and `StatusRow` on Badge

Seven screens and `/styleguide` repeat the status-pill markup by hand (`status-tone status-tone--${tone}` plus a glyph span). This task adds the shared component that replaces it. It moves `StatusRow` and `/styleguide` onto the new component, and nothing else. Each screen swaps its own copy in its screen PR.

**Files:**
- Create: `web/src/shared/StatusBadge.tsx`, `web/src/shared/StatusBadge.test.tsx`
- Create: `web/src/shared/RoleBadge.tsx`, `web/src/shared/RoleBadge.test.tsx`
- Create: `web/src/shared/StatusRow.test.tsx`
- Modify: `web/src/shared/StatusRow.tsx`
- Modify: `web/src/dev/StyleGuide.tsx`, `web/src/dev/StyleGuide.test.tsx`
- Modify: `web/src/styles/primitives.css`

**Interfaces:**
- Consumes: `Badge` and its `success`, `progress`, `warning`, `muted` and `destructive` variants (Task 2). `statusTone(value: string): StatusTone`, `statusGlyph(tone: StatusTone): string` and `type StatusTone` from `src/shared/statusTone.ts`.
- Produces:
  - `export default function StatusBadge(props: Omit<ComponentProps<typeof Badge>, "variant"> & { tone: StatusTone })`. It renders `<span data-slot="badge" data-variant=<variant> data-tone=<tone>>`, containing an `aria-hidden` glyph followed by `children`. Other props (`title`, `data-testid`) pass through.
  - `export default function RoleBadge(props: Omit<ComponentProps<typeof Badge>, "variant" | "children"> & { role: string })`. It renders `<span data-slot="badge" data-variant=<variant> data-role=<role>>{role}</span>`.
  - `StatusRow({ label, value })`: unchanged props. It now renders a `StatusBadge`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/shared/StatusBadge.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import StatusBadge from "./StatusBadge";
import { statusGlyph } from "./statusTone";
import type { StatusTone } from "./statusTone";

afterEach(cleanup);

describe("StatusBadge", () => {
  // The tone table in docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md.
  it.each([
    ["settled", "success"],
    ["failed", "destructive"],
    ["progress", "progress"],
    ["waiting", "warning"],
    ["canceled", "muted"]
  ] as [StatusTone, string][])("paints the %s tone as the %s variant", (tone, variant) => {
    render(<StatusBadge tone={tone}>some status</StatusBadge>);
    const badge = screen.getByText("some status");
    expect(badge.getAttribute("data-slot")).toBe("badge");
    expect(badge.getAttribute("data-variant")).toBe(variant);
    expect(badge.getAttribute("data-tone")).toBe(tone);
  });

  it("pairs the words with the tone's glyph, hidden from assistive tech", () => {
    render(<StatusBadge tone="failed">failed</StatusBadge>);
    const glyph = screen.getByText(statusGlyph("failed"));
    expect(glyph.getAttribute("aria-hidden")).toBe("true");
    expect(glyph.parentElement).toBe(screen.getByText("failed"));
  });

  it("passes a title and test ID through to the badge", () => {
    render(
      <StatusBadge tone="waiting" title="waiting_approval" data-testid="run-status">
        Awaiting approval
      </StatusBadge>
    );
    const badge = screen.getByTestId("run-status");
    expect(badge.getAttribute("title")).toBe("waiting_approval");
    expect(badge.textContent).toContain("Awaiting approval");
  });
});
```

Create `web/src/shared/RoleBadge.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import RoleBadge from "./RoleBadge";

afterEach(cleanup);

describe("RoleBadge", () => {
  it.each([
    ["owner", "progress"],
    ["operator", "success"],
    ["approver", "warning"],
    ["viewer", "muted"]
  ])("paints %s as the %s variant", (role, variant) => {
    render(<RoleBadge role={role} />);
    const badge = screen.getByText(role);
    expect(badge.getAttribute("data-variant")).toBe(variant);
    expect(badge.getAttribute("data-role")).toBe(role);
  });

  // The API types a role as a string, so a role the UI doesn't know yet still
  // renders, named and neutral. A plain lookup object would resolve
  // "constructor" to Object.prototype.constructor.
  it.each(["auditor", "constructor"])("renders the unknown role %s as muted", (role) => {
    render(<RoleBadge role={role} />);
    expect(screen.getByText(role).getAttribute("data-variant")).toBe("muted");
  });
});
```

Create `web/src/shared/StatusRow.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import StatusRow from "./StatusRow";

afterEach(cleanup);

describe("StatusRow", () => {
  it("labels a status and shows it as a badge of its tone", () => {
    render(<StatusRow label="Registration" value="pending_validation" />);
    const row = screen.getByText("Registration").parentElement!;
    expect(row.getAttribute("data-status")).toBe("pending_validation");
    expect(within(row).getByText("pending_validation").getAttribute("data-tone")).toBe("waiting");
  });
});
```

In `web/src/dev/StyleGuide.test.tsx`, replace the two tests `"renders the real status tones rather than copies of their markup"` and `"shows every role badge variant"` with:

```tsx
  it("renders the real status tones rather than copies of their markup", () => {
    render(<StyleGuide />);
    // StatusBadge marks its tone. If the gallery drew a copy of the markup
    // instead, this would break, which is the point: the gallery must not
    // drift from the components.
    const section = screen.getByTestId("sg-theme");
    for (const tone of ["settled", "progress", "waiting", "failed", "canceled"]) {
      expect(section.querySelector(`[data-tone="${tone}"]`), `missing tone ${tone}`).toBeTruthy();
    }
  });

  it("shows every role badge", () => {
    render(<StyleGuide />);
    const section = screen.getByTestId("sg-theme");
    for (const role of ["owner", "operator", "approver", "viewer"]) {
      expect(section.querySelector(`[data-role="${role}"]`), `missing role ${role}`).toBeTruthy();
    }
  });

  it("shows every Badge variant", () => {
    render(<StyleGuide />);
    const section = screen.getByTestId("sg-theme");
    for (const variant of ["default", "secondary", "outline", "destructive", "success", "progress", "warning", "muted"]) {
      expect(section.querySelector(`[data-slot="badge"][data-variant="${variant}"]`), `missing ${variant}`).toBeTruthy();
    }
  });
```

- [ ] **Step 2: Run them to verify they fail**

Run: `npx vitest run src/shared/StatusBadge.test.tsx src/shared/RoleBadge.test.tsx src/shared/StatusRow.test.tsx src/dev/StyleGuide.test.tsx`
Expected: FAIL. `StatusBadge` and `RoleBadge` can't be resolved. `StatusRow`'s badge has no `data-tone`. The styleguide has no `[data-tone]`, `[data-role]` or Badge variants.

- [ ] **Step 3: Write the components**

Create `web/src/shared/StatusBadge.tsx`:

```tsx
import type { ComponentProps } from "react";
import { Badge } from "@/components/ui/badge";
import { statusGlyph } from "./statusTone";
import type { StatusTone } from "./statusTone";

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>["variant"]>;

// The tone table in docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md.
const VARIANTS: Record<StatusTone, BadgeVariant> = {
  settled: "success",
  progress: "progress",
  waiting: "warning",
  failed: "destructive",
  canceled: "muted"
};

type StatusBadgeProps = Omit<ComponentProps<typeof Badge>, "variant"> & { tone: StatusTone };

// A status pill. The glyph and the words say what the colour says, so colour
// is never the only signal. Callers choose the tone, usually
// statusTone(value), and the words.
export default function StatusBadge({ tone, children, ...props }: StatusBadgeProps) {
  return (
    <Badge variant={VARIANTS[tone]} data-tone={tone} {...props}>
      <span aria-hidden="true" className={tone === "progress" ? "motion-safe:animate-pulse" : undefined}>
        {statusGlyph(tone)}
      </span>
      {children}
    </Badge>
  );
}
```

Create `web/src/shared/RoleBadge.tsx`:

```tsx
import type { ComponentProps } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>["variant"]>;

// Roles share the status tints; owner takes progress's primary blue. This is a
// Map, not an object, so a role named like an Object.prototype property
// ("constructor") can't find one.
const VARIANTS = new Map<string, BadgeVariant>([
  ["owner", "progress"],
  ["operator", "success"],
  ["approver", "warning"],
  ["viewer", "muted"]
]);

type RoleBadgeProps = Omit<ComponentProps<typeof Badge>, "variant" | "children"> & { role: string };

// A stack role. The API types a role as a string, so one the UI doesn't know
// yet still renders, named and muted. min-w-20 fits the longest role, so a
// column of badges lines up.
export default function RoleBadge({ role, className, ...props }: RoleBadgeProps) {
  return (
    <Badge variant={VARIANTS.get(role) ?? "muted"} data-role={role} className={cn("min-w-20", className)} {...props}>
      {role}
    </Badge>
  );
}
```

Replace `web/src/shared/StatusRow.tsx` with:

```tsx
import StatusBadge from "./StatusBadge";
import { statusTone } from "./statusTone";

// A labelled status under a form or panel: the label at one edge, its status
// at the other, set off from what is above by a rule.
export default function StatusRow({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="mt-4 flex items-center justify-between gap-3 border-t pt-3 text-sm text-muted-foreground"
      data-status={value}
    >
      <span>{label}</span>
      <StatusBadge tone={statusTone(value)}>{value}</StatusBadge>
    </div>
  );
}
```

- [ ] **Step 4: Move the styleguide's status and role specimens into the shadcn section**

In `web/src/dev/StyleGuide.tsx`:

Change the imports: replace `import { statusGlyph, statusTone } from "../shared/statusTone";` with `import { statusTone } from "../shared/statusTone";`, and add:

```tsx
import RoleBadge from "../shared/RoleBadge";
import StatusBadge from "../shared/StatusBadge";
import { Badge } from "@/components/ui/badge";
```

Next to `BUTTON_SIZES`, add:

```tsx
const BADGE_VARIANTS = ["default", "secondary", "outline", "destructive", "success", "progress", "warning", "muted"] as const;
// One status per tone: settled, progress, waiting, failed, canceled.
const STATUS_SAMPLES = ["completed", "running", "waiting_approval", "failed", "canceled"];
const ROLES = ["owner", "operator", "approver", "viewer"];
```

Delete the `SECTIONS` entries `{ id: "status", title: "Status tones" },` and `{ id: "roles", title: "Role badges" },`.

Inside the `shadcn theme` `<Section>`, after the "Button sizes" `<Specimen>`, add:

```tsx
            <Specimen label="Badge variants">
              {BADGE_VARIANTS.map((variant) => (
                <Badge key={variant} variant={variant}>
                  {variant}
                </Badge>
              ))}
            </Specimen>
            <Specimen label="StatusBadge" hint="real component, tone from statusTone()">
              {STATUS_SAMPLES.map((status) => (
                <StatusBadge key={status} tone={statusTone(status)}>
                  {status}
                </StatusBadge>
              ))}
            </Specimen>
            <Specimen label="StatusRow" hint="real component" stack>
              <div className="w-full max-w-md">
                <StatusRow label="Template" value="completed" />
                <StatusRow label="Plan" value="running" />
                <StatusRow label="Approval" value="waiting_approval" />
                <StatusRow label="Apply" value="failed" />
                <StatusRow label="Cancelled" value="canceled" />
                <StatusRow label="Approved" value="approved" />
              </div>
            </Specimen>
            <Specimen label="RoleBadge" hint="real component">
              {ROLES.map((role) => (
                <RoleBadge key={role} role={role} />
              ))}
            </Specimen>
```

Delete the two legacy sections that showed the same things: `<Section id="status" title="Status tones" …>…</Section>` and `<Section id="roles" title="Role badges" …>…</Section>`.

In the "Tables" section's `data-table` specimen, replace the status cell's hand-copied pill:

```tsx
                        <span className={`status-tone status-tone--${statusTone(row.status)}`}>
                          <span className="status-tone__glyph" aria-hidden="true">
                            {statusGlyph(statusTone(row.status))}
                          </span>
                          {row.status}
                        </span>
```

with:

```tsx
                        <StatusBadge tone={statusTone(row.status)}>{row.status}</StatusBadge>
```

- [ ] **Step 5: Run the tests**

Run: `npx vitest run src/shared src/dev`
Expected: PASS.

- [ ] **Step 6: Delete the `StatusRow` rules**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `primitives.css` with `delete the rules for: status-row`.

In `web/src/styles/primitives.css`, delete `.status-row { … }` and `.status-row strong { … }`. They sit directly under the `/* ---- Status tones ---- */` comment. Keep the comment and every `.status-tone*` rule: six screens still use them. Keep every `.role-badge*` rule too: `StackAccessScreen` still uses them until PR 5.

- [ ] **Step 7: Run the guards and the type-check**

Run: `npx vitest run src/styles src/shared src/dev && npx tsc -b`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add src/shared src/dev src/styles/primitives.css
git commit -m "feat(web): status and role badges on the shadcn Badge" -m "StatusBadge and RoleBadge replace the hand-copied status-tone and role-badge markup. StatusRow and the styleguide use them now; each screen switches in its own PR. An unknown role renders muted." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `Breadcrumb` on the shadcn Breadcrumb

**Files:**
- Modify: `web/src/shared/Breadcrumb.tsx`, `web/src/shared/Breadcrumb.test.tsx`
- Modify: `web/src/features/stacks/StackDetailShell.test.tsx` (four breadcrumb assertions)
- Modify: `web/src/features/stacks/StacksListScreen.tsx`, `web/src/features/templates/TemplateRegistryScreen.tsx`, `web/src/features/templates/TemplateDetailScreen.tsx` (one prop each)
- Modify: `web/src/dev/StyleGuide.tsx`, `web/src/dev/StyleGuide.test.tsx`
- Modify: `web/src/styles/primitives.css`, `web/src/styles/features.css`

**Interfaces:**
- Consumes: `Breadcrumb`, `BreadcrumbList`, `BreadcrumbItem`, `BreadcrumbLink` and `BreadcrumbSeparator` from `@/components/ui/breadcrumb` (Task 2).
- Produces: `export default function Breadcrumb({ items, detail, className }: { items: Crumb[]; detail?: ReactNode; className?: string })`. `export type Crumb` is unchanged. The nav is still named "Breadcrumb", and the last crumb is still an `<h1 aria-current="page">`. New: `className` replaces the default page spacing (`mb-6`). The detail renders in a `<span data-slot="breadcrumb-detail">`.

- [ ] **Step 1: Make the StackDetailShell breadcrumb tests independent of markup details**

Four tests in `web/src/features/stacks/StackDetailShell.test.tsx` regex-match the old markup exactly (`<nav class="breadcrumb" aria-label="Breadcrumb">`, `<h1 aria-current="page">`), which any class change breaks. Add this helper below `renderStackRoute`:

```ts
// The breadcrumb's links and title, read from server-rendered markup without
// depending on classes or attribute order.
function breadcrumbOf(markup: string) {
  const nav = markup.match(/<nav [^>]*aria-label="Breadcrumb"[^>]*>[\s\S]*?<\/nav>/)?.[0] ?? "";
  return {
    links: [...nav.matchAll(/<a [^>]*href="([^"]*)"[^>]*>([^<]*)<\/a>/g)].map(([, href, text]) => `${text} ${href}`),
    title: nav.match(/<h1 [^>]*aria-current="page"[^>]*>([^<]*)<\/h1>/)?.[1],
    detail: nav.includes('data-slot="breadcrumb-detail"')
  };
}
```

Then rewrite the assertions:

In `"titles the page with a Stacks / stack breadcrumb"`, replace both `expect`s with:

```ts
    expect(breadcrumbOf(markup)).toEqual({ links: ["Stacks /stacks"], title: "Payments", detail: false });
```

In `"swaps the stack tabs for the template's on a template's page"`, replace `expect(markup).not.toContain("breadcrumb__detail");` with:

```ts
    expect(breadcrumbOf(markup).detail).toBe(false);
```

In `"names the template on its own page"`, replace the `toMatch` with:

```ts
    expect(breadcrumbOf(markup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates"],
      title: "Network"
    });
```

In `"extends the breadcrumb through the template on a page below it"`, replace both `toMatch`es with:

```ts
    expect(breadcrumbOf(runMarkup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates", "Network /stacks/stack_1/templates/st_1"],
      title: "Run #4"
    });
```

and

```ts
    expect(breadcrumbOf(upgradeMarkup)).toMatchObject({
      links: ["Stacks /stacks", "Payments /stacks/stack_1", "Templates /stacks/stack_1/templates", "Network /stacks/stack_1/templates/st_1"],
      title: "Change revision"
    });
```

Run: `npx vitest run src/features/stacks/StackDetailShell.test.tsx`
Expected: PASS against the current, legacy Breadcrumb. The helper reads both markups.

- [ ] **Step 2: Write the failing Breadcrumb tests**

Replace `web/src/shared/Breadcrumb.test.tsx` with:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import Breadcrumb from "./Breadcrumb";
import type { Crumb } from "./Breadcrumb";

afterEach(cleanup);

function renderCrumbs(items: Crumb[], props: { detail?: string; className?: string } = {}) {
  render(
    <MemoryRouter>
      <Breadcrumb items={items} {...props} />
    </MemoryRouter>
  );
  return screen.getByRole("navigation", { name: "Breadcrumb" });
}

describe("Breadcrumb", () => {
  it("links each ancestor and makes the current page the h1", () => {
    const nav = renderCrumbs([{ label: "Templates", to: "/templates" }, { label: "vpc" }], { detail: "acme/vpc · main" });

    expect(within(nav).getByRole("link", { name: "Templates" }).getAttribute("href")).toBe("/templates");
    const heading = within(nav).getByRole("heading", { level: 1 });
    expect(heading.textContent).toBe("vpc");
    expect(heading.getAttribute("aria-current")).toBe("page");
    expect(within(nav).getByText("acme/vpc · main")).toBeTruthy();
  });

  // Screen readers announce the list's length; the chevrons between crumbs
  // must not count toward it.
  it("hides the separators from assistive tech", () => {
    const nav = renderCrumbs([{ label: "Stacks", to: "/stacks" }, { label: "payments", to: "/stacks/s1" }, { label: "Run #4" }]);

    const separators = nav.querySelectorAll('[data-slot="breadcrumb-separator"]');
    expect(separators).toHaveLength(2);
    separators.forEach((separator) => expect(separator.getAttribute("aria-hidden")).toBe("true"));
    expect(within(nav).getAllByRole("listitem")).toHaveLength(3);
  });

  it("shows an ancestor without a path as text", () => {
    const nav = renderCrumbs([{ label: "Archive" }, { label: "vpc" }]);

    expect(within(nav).queryByRole("link")).toBeNull();
    expect(within(nav).getByText("Archive")).toBeTruthy();
  });

  // A header row sets the breadcrumb beside a button and spaces the row itself.
  it("spaces the page below it unless the caller replaces the spacing", () => {
    expect(renderCrumbs([{ label: "Stacks" }]).classList).toContain("mb-6");
    cleanup();
    const nav = renderCrumbs([{ label: "Stacks" }], { className: "mb-0" });
    expect(nav.classList).toContain("mb-0");
    expect(nav.classList).not.toContain("mb-6");
  });

  it("renders the detail slot only when there is a detail", () => {
    expect(renderCrumbs([{ label: "vpc" }]).querySelector('[data-slot="breadcrumb-detail"]')).toBeNull();
    cleanup();
    const nav = renderCrumbs([{ label: "vpc" }], { detail: "acme/vpc · main" });
    expect(nav.querySelector('[data-slot="breadcrumb-detail"]')?.textContent).toBe("acme/vpc · main");
  });

  // Stack and template names are user-chosen and can be long.
  it("lets a long page name wrap rather than widen the page", () => {
    const nav = renderCrumbs([{ label: "a".repeat(120) }]);
    expect(within(nav).getByRole("heading", { level: 1 }).classList).toContain("wrap-anywhere");
  });
});
```

- [ ] **Step 3: Run them to verify they fail**

Run: `npx vitest run src/shared/Breadcrumb.test.tsx`
Expected: "links each ancestor…" and "shows an ancestor without a path as text" pass against the legacy component; they pin its behaviour for the rewrite. The other four fail: there are no separator elements, `className` is ignored, there's no `data-slot="breadcrumb-detail"`, and the h1 has no `wrap-anywhere`.

- [ ] **Step 4: Rewrite the component**

Replace `web/src/shared/Breadcrumb.tsx` with:

```tsx
import { Fragment } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import {
  Breadcrumb as BreadcrumbNav,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";
import { cn } from "@/lib/utils";

export type Crumb = {
  label: ReactNode;
  /** Omitted on the last crumb, which is the current page. */
  to?: string;
  testId?: string;
};

// The page's title bar. The last crumb is the page's h1, so every screen keeps
// exactly one top-level heading even though nothing on screen looks like one.
// The separators are aria-hidden, so assistive tech counts only real crumbs.
//
// Until PR 9, base.css styles bare a, a:hover and h1 from the legacy layer, and
// a rule on the element beats a colour inherited from the list. So the links
// and the h1 set their own colour, type and decoration.
export default function Breadcrumb({
  items,
  detail,
  className
}: {
  items: Crumb[];
  detail?: ReactNode;
  /** Replaces the page spacing below it; header rows pass "mb-0". */
  className?: string;
}) {
  const current = items[items.length - 1];
  const trail = items.slice(0, -1);

  return (
    <BreadcrumbNav
      aria-label="Breadcrumb"
      className={cn("mb-6 flex min-h-9 min-w-0 flex-wrap items-center gap-x-3 gap-y-1", className)}
    >
      <BreadcrumbList>
        {trail.map((crumb, index) => (
          <Fragment key={index}>
            <BreadcrumbItem>
              {crumb.to ? (
                <BreadcrumbLink
                  render={<Link to={crumb.to} />}
                  className="text-muted-foreground hover:no-underline"
                  data-testid={crumb.testId}
                >
                  {crumb.label}
                </BreadcrumbLink>
              ) : (
                <span data-testid={crumb.testId}>{crumb.label}</span>
              )}
            </BreadcrumbItem>
            <BreadcrumbSeparator />
          </Fragment>
        ))}
        <BreadcrumbItem>
          <h1
            aria-current="page"
            data-testid={current.testId}
            className="font-sans text-sm font-normal tracking-normal text-foreground wrap-anywhere"
          >
            {current.label}
          </h1>
        </BreadcrumbItem>
      </BreadcrumbList>
      {detail && (
        <span data-slot="breadcrumb-detail" className="font-mono text-xs text-muted-foreground wrap-anywhere">
          {detail}
        </span>
      )}
    </BreadcrumbNav>
  );
}
```

- [ ] **Step 5: Run the component and screen tests**

Run: `npx vitest run src/shared/Breadcrumb.test.tsx src/features/stacks src/features/templates`
Expected: PASS. That includes `StackDetailShell.test.tsx` (Step 1's helper) and `CreateStackScreen.test.tsx`, which finds the breadcrumb by role.

- [ ] **Step 6: Zero the spacing in the three header rows**

`.stacks-list-header`, `.templates-list-header` and `.template-detail-header` are flex rows that set the breadcrumb beside a button and carry the spacing below the row themselves. A legacy rule used to zero the breadcrumb's margin in these rows. Utilities outrank the legacy layer, so that rule can no longer reach it, and each row passes `className` instead:

- `web/src/features/stacks/StacksListScreen.tsx`: in the `<header className="stacks-list-header">`, change `<Breadcrumb items={[{ label: "Stacks" }]} />` to `<Breadcrumb items={[{ label: "Stacks" }]} className="mb-0" />`. Leave the error branch's breadcrumb, which isn't in a header row, unchanged.
- `web/src/features/templates/TemplateRegistryScreen.tsx`: in the `<header className="templates-list-header">`, change `<Breadcrumb items={[{ label: "Templates" }]} />` to `<Breadcrumb items={[{ label: "Templates" }]} className="mb-0" />`. Leave the error branch unchanged.
- `web/src/features/templates/TemplateDetailScreen.tsx`: in the `<header className="template-detail-header">`, add `className="mb-0"` to the `<Breadcrumb` element, after its `detail={…}` prop. Leave the error branch unchanged.

- [ ] **Step 7: Show the Breadcrumb on `/styleguide`**

The styleguide renders standalone in its test and inside the app's data router at `/styleguide`. `Breadcrumb`'s links need a router in both places, and a second router inside the app's would throw.

In `web/src/dev/StyleGuide.tsx`, add `import { MemoryRouter, useInRouterContext } from "react-router-dom";` and `import Breadcrumb from "../shared/Breadcrumb";`. Then add, after `Specimen`:

```tsx
// Breadcrumb links need a router. The app mounts this page inside its own
// router, and a second one there would throw; the standalone test has none.
function WithRouter({ children }: { children: ReactNode }) {
  return useInRouterContext() ? <>{children}</> : <MemoryRouter>{children}</MemoryRouter>;
}
```

Inside the `shadcn theme` `<Section>`, after the "RoleBadge" specimen, add:

```tsx
            <Specimen label="Breadcrumb" hint="real component" stack>
              <WithRouter>
                <Breadcrumb
                  className="mb-0"
                  items={[{ label: "Stacks", to: "#theme" }, { label: "payments", to: "#theme" }, { label: "Run #4" }]}
                  detail="acme/vpc · main"
                />
              </WithRouter>
            </Specimen>
```

In `web/src/dev/StyleGuide.test.tsx`, add:

```tsx
  it("renders the real Breadcrumb without a router around the page", () => {
    render(<StyleGuide />);
    const nav = within(screen.getByTestId("sg-theme")).getByRole("navigation", { name: "Breadcrumb" });
    expect(within(nav).getByRole("heading", { level: 1 }).textContent).toBe("Run #4");
  });
```

Run: `npx vitest run src/dev`
Expected: PASS.

- [ ] **Step 8: Delete the breadcrumb rules**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `primitives.css` with `delete the rules for: breadcrumb__detail`. The guard can't see that `.breadcrumb` is dead too, because the word appears in `.tsx` comments. Delete it by hand:

- `web/src/styles/primitives.css`: delete from the `/* ---- Breadcrumb ----` comment through `.breadcrumb__detail { … }`: `.breadcrumb`, `.breadcrumb ol`, `.breadcrumb li`, `.breadcrumb li + li::before`, `.breadcrumb a`, `.breadcrumb a:hover`, `.breadcrumb h1` and `.breadcrumb__detail`. Stop before `/* A heading inside a page whose title the breadcrumb already carries. */`, which belongs to `.section-title`.
- `web/src/styles/features.css`: delete the rule for `.stacks-list-header .breadcrumb, .templates-list-header .breadcrumb, .template-detail-header .breadcrumb { margin-bottom: 0; }`, along with its comment `/* The breadcrumb's bottom margin is page-level spacing; inside these header rows the row itself carries it. */`.
- `web/src/styles/features.css`: in the `@media (max-width: 760px)` block, the comment above `h1, .showcase__title` ends with `The breadcrumb's h1 is unaffected; .breadcrumb h1 outranks this selector.` Change that sentence to `The breadcrumb's h1 is unaffected: its text-sm utility outranks the legacy layer.`

Run: `rg -n "breadcrumb" src/styles`
Expected: only that comment in `features.css`, the `.section-title` comment in `primitives.css`, and the "stated once, beside the breadcrumb" comment in `features.css`.

- [ ] **Step 9: Run everything touched, and the type-check**

Run: `npx vitest run src/styles src/shared src/dev src/features && npx tsc -b`
Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add src/shared src/dev src/features src/styles
git commit -m "feat(web): breadcrumb on the shadcn Breadcrumb" -m "Same props, still named Breadcrumb, and the current page is still the h1. Chevron separators are aria-hidden. Page spacing is mb-6 by default, and the three header rows pass mb-0: a legacy rule used to zero it, and legacy rules can no longer outrank utilities." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `LogSteps` on Collapsible

**Files:**
- Modify: `web/src/shared/LogSteps.tsx`
- Create: `web/src/shared/LogSteps.test.tsx`
- Modify: `web/src/dev/StyleGuide.tsx`, `web/src/dev/StyleGuide.test.tsx`
- Modify: `web/src/styles/features.css`

**Interfaces:**
- Consumes: `Collapsible`, `CollapsibleTrigger`, `CollapsibleContent` (Task 2).
- Produces: `LogSteps({ children })` and `LogStep({ name, open, onToggle, children })`, with unchanged props. Every row is an `<li>` holding a `<button aria-expanded>` named by `name`. While a row is open, the button's `aria-controls` points at the log. A closed row mounts no children. `RunLogsPanel.tsx` does not change.

- [ ] **Step 1: Write the tests**

Create `web/src/shared/LogSteps.test.tsx`:

```tsx
// @vitest-environment jsdom
import { useEffect, useState } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LogStep, LogSteps } from "./LogSteps";

afterEach(cleanup);

function Steps({ initiallyOpen = {} }: { initiallyOpen?: Record<string, boolean> }) {
  const [open, setOpen] = useState(initiallyOpen);
  return (
    <LogSteps>
      {["init", "plan"].map((name) => (
        <LogStep
          key={name}
          name={name}
          open={open[name] ?? false}
          onToggle={() => setOpen((current) => ({ ...current, [name]: !current[name] }))}
        >
          {`${name} log body`}
        </LogStep>
      ))}
    </LogSteps>
  );
}

describe("LogSteps", () => {
  it("lists one row per step, each a button that says whether its log is open", () => {
    render(<Steps initiallyOpen={{ plan: true }} />);

    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "init" }).getAttribute("aria-expanded")).toBe("false");
    const plan = screen.getByRole("button", { name: "plan" });
    expect(plan.getAttribute("aria-expanded")).toBe("true");
    expect(document.getElementById(plan.getAttribute("aria-controls") ?? "")?.textContent).toBe("plan log body");
  });

  // RunLogsPanel passes a component that fetches the log, so a closed row
  // must not mount it.
  it("mounts a log only while its row is open", async () => {
    const mounted = vi.fn();
    function Log() {
      useEffect(() => {
        mounted();
      }, []);
      return <>apply log body</>;
    }
    function OneStep() {
      const [open, setOpen] = useState(false);
      return (
        <LogSteps>
          <LogStep name="apply" open={open} onToggle={() => setOpen(!open)}>
            <Log />
          </LogStep>
        </LogSteps>
      );
    }
    render(<OneStep />);

    expect(mounted).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "apply" }));
    expect(mounted).toHaveBeenCalledTimes(1);
    expect(screen.getByText("apply log body")).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "apply" }));
    expect(screen.queryByText("apply log body")).toBeNull();
  });

  it("opens one row without touching the others", async () => {
    render(<Steps initiallyOpen={{ plan: true }} />);

    await userEvent.click(screen.getByRole("button", { name: "init" }));
    expect(screen.getByText("init log body")).toBeTruthy();
    expect(screen.getByText("plan log body")).toBeTruthy();
  });

  // --legacy-touch-target gave these rows 44px on touch screens.
  it("gives each row a 44px target on coarse pointers", () => {
    render(<Steps />);
    for (const button of screen.getAllByRole("button")) {
      expect(button.classList).toContain("pointer-coarse:min-h-11");
    }
  });
});
```

In `web/src/dev/StyleGuide.test.tsx`, add:

```tsx
  it("renders the real LogSteps in the shadcn section", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    expect(section.getByRole("button", { name: "plan" }).getAttribute("aria-expanded")).toBe("true");
  });
```

- [ ] **Step 2: Run them to verify which fail**

Run: `npx vitest run src/shared/LogSteps.test.tsx src/dev/StyleGuide.test.tsx`
Expected: the first three `LogSteps` tests pass against the legacy component; they pin its behaviour for the rewrite. The touch-target test and the styleguide test fail.

- [ ] **Step 3: Rewrite the component**

Replace `web/src/shared/LogSteps.tsx` with:

```tsx
import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

// Logs laid out as CI systems lay out a job's steps: stacked in the order they
// ran, each a row that opens onto its log. The caller decides which rows are
// open and supplies each log.
export function LogSteps({ children }: { children: ReactNode }) {
  return <ol className="divide-y overflow-hidden rounded-lg border">{children}</ol>;
}

interface LogStepProps {
  name: string;
  open: boolean;
  onToggle: () => void;
  // Mounted only while the row is open, so a log fetched by a component
  // passed here is fetched only once someone opens it. Base UI unmounts a
  // closed panel.
  children: ReactNode;
}

// The list clips to its rounded corners, which would cut off a focus ring drawn
// outside these full-width rows, so the rings are drawn inside them.
export function LogStep({ name, open, onToggle, children }: LogStepProps) {
  return (
    <Collapsible open={open} onOpenChange={onToggle} render={<li />}>
      <CollapsibleTrigger className="group flex min-h-9 w-full items-center gap-2 px-4 text-left text-sm outline-none transition-colors hover:bg-muted focus-visible:inset-ring-2 focus-visible:inset-ring-ring pointer-coarse:min-h-11">
        <ChevronRight
          className="size-4 shrink-0 text-muted-foreground transition-transform group-aria-expanded:rotate-90"
          aria-hidden="true"
        />
        {name}
      </CollapsibleTrigger>
      <CollapsibleContent
        render={<pre />}
        className="max-h-115 overflow-auto bg-foreground px-5 py-4 font-mono text-sm whitespace-pre-wrap text-background outline-none focus-visible:inset-ring-2 focus-visible:inset-ring-background"
      >
        {children}
      </CollapsibleContent>
    </Collapsible>
  );
}
```

The log stays dark, as today, because terminal output reads as terminal output. `max-h-115` is 460px, the old height, and is a scale step, not an arbitrary value. The log's focus ring is white (`inset-ring-background`), because the brand-blue ring measures under 3:1 on the dark log.

- [ ] **Step 4: Move the styleguide specimen into the shadcn section**

In `web/src/dev/StyleGuide.tsx`, delete the `SECTIONS` entry `{ id: "log", title: "Log panel" },` and the whole `<Section id="log" title="Log panel" …>…</Section>`. Inside the `shadcn theme` `<Section>`, after the "Breadcrumb" specimen, add:

```tsx
            <Specimen label="LogSteps" hint="real component, on Collapsible" stack>
              <LogStepsSpecimen />
            </Specimen>
```

- [ ] **Step 5: Run the tests, including the run screen's**

Run: `npx vitest run src/shared/LogSteps.test.tsx src/dev src/features/runs`
Expected: PASS. `RunDetailScreen.test.tsx` clicks the toggles with `fireEvent.click` and expects a closed log to be gone synchronously. Base UI's trigger handles `onClick`, and it unmounts the panel at once when no CSS animation runs, which is always the case in jsdom.

- [ ] **Step 6: Delete the log panel rules**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `features.css` with `delete the rules for: log-steps, log-step, log-step__toggle, log-step__chevron, log-step__log`.

In `web/src/styles/features.css`, delete from the comment that starts `/* ---- Log panel. A run's log phases stack in the order they ran` through the `.log-step__log { … }` rule: the comment, `.log-steps`, `.log-step + .log-step`, `.log-step__toggle`, `.log-step__toggle:hover`, `.log-step__chevron`, `.log-step__toggle[aria-expanded="true"] .log-step__chevron`, the `:focus-visible` rule with its comment, and `.log-step__log`. Keep the `/* ---- Metadata grids ---- */` line above it and the `.spin` rule below it.

- [ ] **Step 7: Run the guards and the type-check**

Run: `npx vitest run src/styles src/shared src/dev src/features/runs && npx tsc -b`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add src/shared src/dev src/styles/features.css
git commit -m "feat(web): log steps on the shadcn Collapsible" -m "Same props. Base UI supplies aria-expanded and aria-controls, and unmounts a closed log, so a log is still fetched only once its row opens. Rows keep a 44px target on coarse pointers." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `AppShell` on Tailwind and the shadcn Button

**Files:**
- Modify: `web/src/app/AppShell.tsx`, `web/src/app/AppShell.test.tsx`
- Modify: `web/src/styles/features.css`, `web/src/styles/base.css`

**Interfaces:**
- Consumes: `Button` and `buttonVariants` from `@/components/ui/button` (PR 1). `useAuth()` returns `{ me, logout, status }`. `tenantID` comes from `../config`.
- Produces: `export default function AppShell()`, still the layout route that renders `<Outlet />` inside `<main id="main-content" tabIndex={-1}>`. Every existing `data-testid` stays: `identity-menu`, `identity-loading`, `identity-display-name`, `logout-button`, `shell-tenant-context`, `debug-panel` and the `debug-*` values. The header carries `z-5`.

- [ ] **Step 1: Write the failing tests**

Replace `web/src/app/AppShell.test.tsx` with the file below. The first three tests are today's, unchanged apart from the shared helpers.

```tsx
// @vitest-environment jsdom
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderToStaticMarkup } from "react-dom/server";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { AuthContext } from "../auth/AuthContext";
import type { AuthContextValue } from "../auth/AuthContext";

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: {
      sub: "user_1",
      tenantID: "tenant_123",
      displayName: "Otto Operator",
      globalCapabilities: { isPlatformAdmin: false, canCreateStack: true, canPublishTemplate: true },
    },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides,
  };
}

function TestAuthWrapper({ value = authValue(), children }: { value?: AuthContextValue; children: ReactNode }) {
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

async function shellRouter() {
  vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
  const { default: AppShell } = await import("./AppShell");
  return createMemoryRouter(
    [
      {
        path: "/",
        element: <AppShell />,
        children: [{ index: true, element: <div data-testid="outlet-content">child</div> }]
      }
    ],
    { initialEntries: ["/"] }
  );
}

async function shellMarkup() {
  const router = await shellRouter();
  return renderToStaticMarkup(
    <TestAuthWrapper>
      <RouterProvider router={router} />
    </TestAuthWrapper>
  );
}

async function renderShell(value = authValue()) {
  const router = await shellRouter();
  return render(
    <TestAuthWrapper value={value}>
      <RouterProvider router={router} />
    </TestAuthWrapper>
  );
}

describe("AppShell", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllEnvs();
  });

  it("renders nav, an identity slot, a static tenant indicator, and routed content", async () => {
    const markup = await shellMarkup();

    expect(markup).toContain('href="/stacks"');
    expect(markup).toContain('href="/templates"');
    expect(markup).toContain('data-testid="identity-menu"');
    expect(markup).toContain('data-testid="shell-tenant-context"');
    expect(markup).toContain(">tenant_123<");
    expect(markup).not.toContain("<input");
    expect(markup).toContain('data-testid="outlet-content"');
  });

  it("displays the user's display name and a logout control", async () => {
    const markup = await shellMarkup();

    expect(markup).toContain("Otto Operator");
    expect(markup).toContain('data-testid="logout-button"');
  });

  it("renders the debug IDs panel when in dev mode", async () => {
    const markup = await shellMarkup();

    expect(markup).toContain('data-testid="debug-panel"');
    expect(markup).toContain("IDs (debug)");
    expect(markup).toContain('data-testid="debug-user-sub"');
    expect(markup).toContain("user_1");
    expect(markup).toContain('data-testid="debug-tenant"');
    expect(markup).toContain("tenant_123");
  });

  it("logs out from the identity menu", async () => {
    const logout = vi.fn();
    await renderShell(authValue({ logout }));

    await userEvent.click(screen.getByRole("button", { name: "Log out" }));
    expect(logout).toHaveBeenCalledTimes(1);
  });

  it("shows a loading line, and no logout control, while the session resolves", async () => {
    await renderShell(authValue({ me: null, status: "loading" }));

    expect(screen.getByTestId("identity-loading").textContent).toBe("Loading...");
    expect(screen.queryByRole("button", { name: "Log out" })).toBeNull();
  });

  it("skips straight to the page content", async () => {
    await renderShell();

    expect(screen.getByRole("link", { name: "Skip to content" }).getAttribute("href")).toBe("#main-content");
    const main = screen.getByRole("main");
    expect(main.id).toBe("main-content");
    expect(main.getAttribute("tabindex")).toBe("-1");
  });

  // --legacy-touch-target gave these controls 44px on touch screens.
  it("gives its controls a 44px target on coarse pointers", async () => {
    await renderShell();

    const controls = [
      screen.getByRole("button", { name: "Log out" }),
      ...within(screen.getByRole("navigation", { name: "Primary" })).getAllByRole("link")
    ];
    for (const control of controls) {
      expect(control.classList).toContain("pointer-coarse:h-11");
    }
  });

  // config.ts allows a 128-character tenant ID. It must wrap inside the header
  // instead of pushing the page sideways on a phone.
  it("lets a long tenant ID wrap", async () => {
    await renderShell();

    expect(screen.getByTestId("shell-tenant-context").classList).toContain("wrap-anywhere");
  });
});

// The header is sticky and opaque. Legacy overlays that open over scrolled
// content must paint above it until they migrate: .search-dropdown on stack
// access (PR 5) and .undo-banner. Update this test when either leaves
// features.css.
describe("AppShell layering", () => {
  const read = (path: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), path), "utf8");
  const zIndexOf = (selector: string) =>
    Number(read("../styles/features.css").match(new RegExp(`\\${selector} \\{[^}]*z-index: (\\d+);`))?.[1]);

  it("keeps the sticky header below the legacy overlays", () => {
    const header = Number(read("AppShell.tsx").match(/<header className="[^"]*\bz-(\d+)\b/)?.[1]);

    expect(header).toBeGreaterThan(0);
    expect(header).toBeLessThan(zIndexOf(".search-dropdown"));
    expect(header).toBeLessThan(zIndexOf(".undo-banner"));
  });
});
```

- [ ] **Step 2: Run them to verify they fail**

Run: `npx vitest run src/app/AppShell.test.tsx`
Expected: the first three tests pass, along with the logout, loading and skip-link tests. The touch-target, tenant-wrap and layering tests fail: the legacy header has no `z-` class, so the layering test reads `NaN`.

- [ ] **Step 3: Rewrite the component**

Replace `web/src/app/AppShell.tsx` with:

```tsx
import { Link, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { tenantID } from "../config";
import { Button, buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const navItems: { to: string; label: string }[] = [
  { to: "/stacks", label: "Stacks" },
  { to: "/templates", label: "Templates" }
];

const isDebug = import.meta.env.DEV || import.meta.env.VITE_DEBUG === "true";

// Until PR 9, base.css colours and underlines every bare <a> from the legacy
// layer, and a rule on the element beats an inherited colour, so each link
// sets its own.
const navLinkClass = cn(
  buttonVariants({ variant: "ghost", size: "sm" }),
  "text-muted-foreground hover:no-underline pointer-coarse:h-11"
);

export default function AppShell() {
  const { me, logout, status } = useAuth();
  const debugRows: [label: string, testId: string, value: string][] = [
    ["Auth status", "debug-auth-status", status],
    ["User sub", "debug-user-sub", me?.sub ?? "-"],
    ["Display name", "debug-display-name", me?.displayName ?? "-"],
    ["Tenant", "debug-tenant", tenantID],
    ["isPlatformAdmin", "debug-is-platform-admin", me?.globalCapabilities.isPlatformAdmin.toString() ?? "-"],
    ["canCreateStack", "debug-can-create-stack", me?.globalCapabilities.canCreateStack.toString() ?? "-"],
    ["canPublishTemplate", "debug-can-publish-template", me?.globalCapabilities.canPublishTemplate.toString() ?? "-"]
  ];

  return (
    <div className="min-h-screen">
      <a
        href="#main-content"
        className="fixed top-2 left-2 z-50 -translate-y-16 rounded-lg bg-primary px-3 py-2 text-sm font-medium text-primary-foreground transition-transform hover:no-underline focus:translate-y-0"
      >
        Skip to content
      </a>
      {/* z-5 keeps the legacy overlays that must cover this bar above it:
          .search-dropdown (10) and .undo-banner (20) in features.css.
          AppShell.test.tsx checks the order. */}
      <header className="sticky top-0 z-5 flex flex-wrap items-center justify-between gap-x-6 gap-y-3 border-b bg-background px-4 py-3 md:px-6">
        <div className="flex items-center gap-4 md:gap-8">
          <span className="text-lg leading-none font-semibold tracking-tight">openplan</span>
          <nav className="flex items-center gap-1" aria-label="Primary">
            {navItems.map((item) => (
              <Link key={item.to} to={item.to} className={navLinkClass}>
                {item.label}
              </Link>
            ))}
          </nav>
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-3 text-sm">
          <div className="flex items-center gap-3 text-muted-foreground" data-testid="identity-menu">
            {status === "loading" && <span data-testid="identity-loading">Loading...</span>}
            {me && (
              <>
                <span data-testid="identity-display-name">{me.displayName}</span>
                <Button
                  variant="outline"
                  size="sm"
                  className="pointer-coarse:h-11"
                  data-testid="logout-button"
                  onClick={logout}
                >
                  Log out
                </Button>
              </>
            )}
          </div>
          <div className="flex min-w-0 items-center gap-2 text-muted-foreground">
            <span>Tenant</span>
            <span
              className="min-w-0 rounded-md bg-muted px-2 py-0.5 font-mono text-xs text-foreground wrap-anywhere"
              data-testid="shell-tenant-context"
            >
              {tenantID}
            </span>
          </div>
        </div>
      </header>
      <main className="mx-auto w-full max-w-7xl px-4 py-6" id="main-content" tabIndex={-1}>
        <Outlet />
      </main>
      {isDebug && (
        <details className="mx-4 my-8 border-t pt-4 font-mono text-xs md:mx-6" data-testid="debug-panel">
          <summary className="cursor-pointer tracking-wide text-muted-foreground uppercase">IDs (debug)</summary>
          <dl className="mt-4 grid gap-2">
            {debugRows.map(([label, testId, value]) => (
              <div key={testId} className="flex gap-4">
                <dt className="w-36 shrink-0 text-muted-foreground">{label}</dt>
                <dd className="min-w-0 wrap-anywhere" data-testid={testId}>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
        </details>
      )}
    </div>
  );
}
```

Notes:
- `max-w-7xl` is 1280px and `px-4 py-6` is 16px by 24px: the old content frame, `width: min(1280px, 100%)` with `padding: 24px 16px`.
- `md:` is 768px; the old header's breakpoint was 760px.
- The tenant value is a plain span, not a Badge. Badge is `whitespace-nowrap` and fixed at `h-5`, and a long tenant ID has to wrap.
- The skip link sits 64px above the viewport until focused. The old one used `translateY(-200%)`.

- [ ] **Step 4: Run the tests**

Run: `npx vitest run src/app`
Expected: PASS. That includes `router.test.tsx` and the other `src/app` tests, which render the shell through the route tree.

- [ ] **Step 5: Delete the shell's rules**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `features.css` and `base.css`, listing the `app-frame*`, `app-nav`, `app-wordmark`, `runtime-*`, `id-grid` and `skip-link` classes. The guard can't see that `.identity-menu` and `.debug-panel` are dead too, because both names survive as test IDs. Delete all of these by hand:

- `web/src/styles/features.css`: delete everything from `/* ---- Application frame ---- */` down to, but not including, `/* ---- Full-viewport showcase screens ---- */`. That is `.app-frame`, `.app-frame-header`, `.app-frame-brand`, `.app-wordmark`, `.app-nav`, `.app-nav a`, `.app-nav a:hover`, `.app-frame-identity`, `.identity-menu`, `.identity-menu button`, `.identity-menu button:hover`, `.app-frame-identity .runtime-field`, `.app-frame-identity .runtime-value`, `.app-frame-content`, `.runtime-field`, `.runtime-value`, `.debug-panel`, `.debug-panel summary`, `.id-grid`, `.id-grid dt` and `.id-grid dd`.
- `web/src/styles/features.css`, `@media (max-width: 760px)` block: delete the `.app-frame-header { … }`, `.app-frame-brand { … }` and `.app-frame-content { … }` rules. Under `/* 44px minimum touch targets where density and accessibility conflict. */`, delete `.app-nav a,` from the selector list, leaving `.icon-button,` and `.selected-user-card button`.
- `web/src/styles/features.css`, `.search-dropdown`: change the comment `/* Must stay ABOVE .app-frame-header (5) — the sticky header is opaque and would otherwise clip this dropdown once the page scrolls. */` to `/* Must stay ABOVE the app header (z-5 in AppShell.tsx): the sticky header is opaque and would otherwise clip this dropdown once the page scrolls. */`.
- `web/src/styles/features.css`, `.undo-banner`: change `/* Must stay ABOVE .app-frame-header (5) and .search-dropdown (10). */` to `/* Must stay ABOVE the app header (z-5 in AppShell.tsx) and .search-dropdown (10). */`.
- `web/src/styles/base.css`: delete the `/* ---- Skip link ---- */` comment and the `.skip-link` and `.skip-link:focus` rules.

Run: `rg -n "app-frame|app-nav|app-wordmark|identity-menu|runtime-(field|value)|debug-panel|id-grid|skip-link" src/styles`
Expected: only the comment in `styles.guard.test.ts` that recounts which controls the height-token check has caught.

- [ ] **Step 6: Run the full suite and the type-check**

Run: `npx tsc -b && npm test`
Expected: `tsc` prints nothing; every test passes.

- [ ] **Step 7: Commit**

```bash
git add src/app src/styles
git commit -m "feat(web): app shell on tailwind and the shadcn Button" -m "Same structure, test IDs and routes. The header stays z-5, below the legacy search dropdown and undo banner; a test now checks that order. Nav links and Log out keep 44px on coarse pointers, and a long tenant ID wraps." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Browser audit, spec amendments, and the PR

**Files:**
- Modify: `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`
- Scratch, not committed: `/tmp/audit2/…`

**Interfaces:**
- Consumes: everything above.
- Produces: the PR.

This audit checks two things. The legacy content inside `<main>` must not move: its computed styles must match before and after. And the migrated pieces must look right: screenshots, plus the checks in Review Focus.

- [ ] **Step 1: Save the dump and diff scripts**

Save as `/tmp/audit2/dump.js`:

```js
(() => {
  const PROPS = ["display", "position", "vertical-align", "list-style-type",
    "margin-top", "margin-right", "margin-bottom", "margin-left",
    "padding-top", "padding-right", "padding-bottom", "padding-left",
    "border-top-width", "border-top-style", "border-bottom-width", "border-bottom-style",
    "font-size", "font-weight", "line-height", "color", "background-color",
    "text-decoration-line", "cursor"];
  // What PR 2 migrated. Their insides change on purpose, so only their outer
  // box is compared. Each selector matches the old markup and the new.
  const MIGRATED = 'nav[aria-label="Breadcrumb"], [data-status], ol:has(> li > button[aria-expanded])';
  const OUTER = ["display", "margin-top", "margin-right", "margin-bottom", "margin-left", "min-height"];
  const main = document.getElementById("main-content");
  const pick = (el, props) => {
    const cs = getComputedStyle(el);
    return Object.fromEntries(props.map((p) => [p, cs.getPropertyValue(p)]));
  };
  const out = { main: pick(main, PROPS) };
  const walk = (el, path) => {
    [...el.children].forEach((child, i) => {
      const key = `${path} > ${child.tagName.toLowerCase()}[${i}]`;
      const migrated = child.matches(MIGRATED);
      out[key] = pick(child, migrated ? OUTER : PROPS);
      if (!migrated) walk(child, key);
    });
  };
  walk(main, "main");
  return JSON.stringify(out);
})()
```

Save as `/tmp/audit2/diff.py`:

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
        for prop, value in before[path].items():
            if after[path].get(prop) != value:
                print(f"{before_file.stem}: {path}\n    {prop}: {value} -> {after[path].get(prop)}"); clean = False
print("CLEAN" if clean else "DIFFERENCES FOUND")
```

- [ ] **Step 2: List the screens**

The local stack must be up (`docker compose --profile auth up -d` from the repository root). Export `OPENPLAN_USER` and `OPENPLAN_PASS` from `.env`'s `KEYCLOAK_PLATFORM_ADMIN_USERNAME` and `KEYCLOAK_PLATFORM_ADMIN_PASSWORD`, as `scripts/drive-web.mjs` describes. Sign in once and collect real IDs: click into one stack, one of its templates, one run of that template, and one registry template. After each click, run `node scripts/drive-web.mjs --probe 'location.pathname'`. If the local database has no stack with a template and a run, create them through the UI first.

Write `/tmp/audit2/screens.txt`, one `name path` pair per line:

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
```

- [ ] **Step 3: Capture "before" from `feat/shadcn-foundation`**

From the repository root:

```bash
git worktree add /tmp/audit2/before-tree feat/shadcn-foundation
(cd /tmp/audit2/before-tree/web && npm ci)
docker compose stop web        # frees port 5173, the only redirect URI Keycloak accepts
(cd /tmp/audit2/before-tree/web && npm run dev) &
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
  --remote-debugging-port=9222 --user-data-dir=/tmp/audit2/chrome --window-size=1512,950 about:blank &
mkdir -p /tmp/audit2/before /tmp/audit2/shots
while read -r name path; do
  node scripts/drive-web.mjs --goto "$path" --probe "$(cat /tmp/audit2/dump.js)" > "/tmp/audit2/before/$name.json"
done < /tmp/audit2/screens.txt
for name in stacks template-run registry-new registry-template; do
  node scripts/drive-web.mjs --goto "$(grep "^$name " /tmp/audit2/screens.txt | cut -d' ' -f2)" --shot "/tmp/audit2/shots/before-$name.png"
done
node scripts/drive-web.mjs --signed-out --goto /styleguide --shot /tmp/audit2/shots/before-styleguide.png
```

Stop that dev server (`kill %1`, or find it with `lsof -i :5173`) and keep Chrome running.

- [ ] **Step 4: Capture "after" from this branch**

Run the same loop and screenshots with `npm run dev` started from `web/` on `feat/shadcn-shell-shared`. Write to `/tmp/audit2/after/` and `/tmp/audit2/shots/after-*.png`.

- [ ] **Step 5: Diff**

Run: `python3 /tmp/audit2/diff.py /tmp/audit2/before /tmp/audit2/after`
Expected: `CLEAN`. Legacy content in `<main>` is unchanged, and so is each migrated piece's outer box: breadcrumb margin `24px` (`0px` in the three header rows), `min-height: 36px`, and `StatusRow`'s `16px` top margin.

For each difference, find the task that owns the element and fix it there: a margin the old rule gave and the new classes don't, or a legacy rule deleted too broadly. Re-capture "after" and diff again.

- [ ] **Step 6: Check what the diff can't**

Still on the "after" server:

1. **Role badges line up.** Run `node scripts/drive-web.mjs --signed-out --goto /styleguide --probe 'JSON.stringify([...document.querySelectorAll("[data-role]")].map((b) => b.getBoundingClientRect().width))'`
   Expected: four equal widths. If they differ, raise `min-w-20` in `RoleBadge.tsx` one step at a time (`min-w-22`, `min-w-24`) until they match, then amend Task 3's commit (`git commit --fixup`).
2. **The skip link appears when focused.** Run `node scripts/drive-web.mjs --goto /stacks --probe '(async () => { const a = document.querySelector("a[href=\"#main-content\"]"); a.focus(); await new Promise((r) => setTimeout(r, 400)); const r = a.getBoundingClientRect(); return JSON.stringify({ top: r.top, bottom: r.bottom }); })()'`
   Expected: `top` is at least 0 and `bottom` is greater than `top`.
3. **Nothing scrolls sideways at phone width.** Quit Chrome and restart it with `--window-size=375,812` and the same `--user-data-dir`, which keeps the session. For `stacks`, `template-run` and `registry-template`, run `node scripts/drive-web.mjs --goto <path> --probe 'JSON.stringify({ scroll: document.documentElement.scrollWidth, width: innerWidth })'`
   Expected: `scroll` is at most `width` on each.

Then compare the before and after screenshots side by side. Expected differences: the header (shadcn buttons, plain wordmark), chevron separators in the breadcrumb, the status badge on `registry-new`, and the log rows on `template-run`. The page content around them should not move.

Clean up from the repository root: stop Chrome and the dev server, run `docker compose start web`, then `git worktree remove --force /tmp/audit2/before-tree`.

- [ ] **Step 7: Bring the spec in line with what PR 2 built**

Edit `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`:

- **Status line:** `Approved. Guard and testing sections reviewed with PR 1. PR 1 and PR 2 settled the details recorded below.`
- **Theme section,** after the tone-to-variant table: replace `The four role badges (owner, operator, approver, viewer) also become Badge variants.` with `The four role badges map onto the same variants: owner \`progress\`, operator \`success\`, approver \`warning\`, viewer \`muted\`. A role the UI doesn't know renders \`muted\`.`
- **Known leak paragraph:** add `Migrated components therefore set colour, type and decoration on their own \`a\` and \`h1\` elements: a rule on the element beats a colour it would inherit.`
- **Migration table, PR 2 row:** Scope becomes `App shell (\`AppShell\`) and \`src/shared/\` components; deletes the unused \`StatBand\` and \`IdsPanel\``. Main shadcn pieces becomes `Breadcrumb; Badge (status tones, roles); Collapsible (\`LogSteps\`)`.
- **After the migration table,** add:

  ```markdown
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
  ```

- **What every screen PR must do,** add two items:

  ```markdown
  6. Give each control a 44px target on coarse pointers, as
     `--legacy-touch-target` did: `pointer-coarse:h-11` on fixed-height
     controls, `pointer-coarse:min-h-11` on rows whose height comes from their
     content.
  7. Replace its inline `status-tone` and `role-badge` markup with
     `StatusBadge` and `RoleBadge` from `src/shared/`.
  ```

- [ ] **Step 8: Full verification**

Run from `web/`:

```bash
npx tsc -b
npm test
npm run build
```

Expected: `tsc` silent; every test passes; the build succeeds. Note the test count and the CSS and JS sizes the build prints, for the PR description. No native dependency changed, so `docker compose build web` isn't needed.

- [ ] **Step 9: Commit**

```bash
git add ../docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md
git commit -m "docs: record what PR 2 of the shadcn migration settled" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 10: Push and open the PR against `feat/shadcn-foundation`**

```bash
gh auth status
git push -u origin feat/shadcn-shell-shared
gh pr create --base feat/shadcn-foundation --head feat/shadcn-shell-shared \
  --title "feat(web): app shell and shared components on shadcn" --body-file /tmp/audit2/pr-body.md
```

Write `/tmp/audit2/pr-body.md` first. It must cover:
- that this PR is stacked on #274 and implements PR 2 of the spec, with links to the spec and this plan;
- what moved: the shell, Breadcrumb, LogSteps, StatusRow, and the new StatusBadge and RoleBadge;
- what was deleted: `StatBand`, `IdsPanel`, and every legacy rule they and the migrated components used;
- the behaviour and look changes: shadcn header and nav, chevron breadcrumb separators, Badge-style status pills where `StatusRow` is used, and log rows on Collapsible. Also that screens still show legacy status pills until their own PRs;
- configuration impact: none. No new dependencies, no migrations;
- validation: the commands above with the test count, the audit's `CLEAN` result, the role-badge widths, the skip-link probe and the phone-width probe;
- a `## Screenshots` heading listing the before/after pairs in `/tmp/audit2/shots/`. `gh` can't upload images, so tell the user to drag them into the PR description;
- a final line: `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

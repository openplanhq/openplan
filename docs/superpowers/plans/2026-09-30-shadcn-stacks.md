# Stacks on shadcn (PR 4) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the stacks screens (the list, create stack, the stack detail shell, the Environment tab, and the credentials panel) onto shadcn components and Tailwind utilities. Delete the legacy CSS they stop using.

**Architecture:** The shadcn CLI vendors Table and Tabs. `table.tsx` gets one edit, so its layout is always fixed, and the table guard is rewritten around `<Table>`. Tabs arrives two PRs early: the stack detail shell has the same tab row as the template shell, which the spec gives "Tabs tied to the route" in PR 6. `src/shared/RouteTabs.tsx` wraps shadcn's line Tabs and renders each tab as a `NavLink`, which Base UI documents as "tabs as links". The route picks the active tab, and Base UI supplies the tab-list keyboard behaviour. The stacks list and the credentials list become Tables in shadcn's bordered frame. Create stack and the credentials panel become Cards with Label, Input, Button and Alert. The stacks empty state becomes an Empty. `scripts/drive-web.mjs` learns `--respond` and `--stall`, so the audit can reach the empty and loading states.

**Tech Stack:** React 19, React Router 6, Tailwind CSS 4.3, shadcn CLI 4.21 (`base-nova` style on Base UI 1.8), `class-variance-authority`, `cn`, lucide-react, Vitest 4 with Testing Library and `@testing-library/user-event`.

**Spec:** `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`. Read these sections before starting: "Theme", "Running old and new styles side by side" (the layer order, the Preflight audit, and the known `base.css` leak), "What PR 2 settled", "What PR 3 settled", "What every screen PR must do", "Guards" (the table guard in particular) and "Testing".

## Global Constraints

- Branch `feat/shadcn-stacks`, created from `feat/shadcn-standalone-screens` (PR #277, stacked on #276, #274 and #273). The PR base is `feat/shadcn-standalone-screens`.
- All commands run from `web/` unless a step says otherwise.
- Screens keep their copy and behaviour, word for word. The look changes, and so does the component each piece is built from, as the spec's migration table says:
  - the stacks list becomes a Table (Name, Slug);
  - the credentials list becomes a Table (Name, Value, and an unlabelled actions column);
  - the stack tabs become a tab list;
  - errors become Alerts;
  - the stacks empty state becomes an Empty, which loses `HeroGraphic` and the entrance reveal.
- Test IDs don't change, because other test files depend on them: `stacks-list-loading`, `stacks-list-error`, `stacks-list-retry`, `stacks-list`, `stacks-list-empty`, `create-stack-link`, `create-stack-success`, `create-stack-error`, `stack-detail-shell`, `environment-loading`, `environment-error`, `environment-retry`, `environment-screen`.
- These accessible names don't change either:
  - the tab row's label `Stack sections`, which moves from the `nav` to the tab list; the template shell's tests check it is absent there;
  - the credential fields' `<Scope> credential name` and `<Scope> credential value`;
  - each delete button's `Delete <name>`.
- `data-unsaved` stays on the create-stack section and on the credentials panel. SessionProvider's proactive re-auth reads it.
- Components are added only with `npx shadcn add`. PR 4 edits one vendored file: `src/components/ui/table.tsx`, to always apply `table-fixed`, as the spec's table guard section requires. Nothing else in `src/components/ui/` changes.
- App code uses theme colours and Tailwind's scale steps only: no arbitrary values (`w-[37px]`) and no built-in palette colours (`bg-blue-500`). `src/styles/tailwind.guard.test.ts` fails otherwise.
- `base.css` styles bare elements from the `legacy` layer until PR 9. A rule on an element beats anything it would inherit:
  - Every migrated `h2` sets its own font family, size, weight and tracking. `base.css` gives `h2` the legacy 32px display type.
  - Every migrated link sets its own colour and `no-underline`, because of the legacy `a` and `a:hover` rules. AppShell's `navLinkClass` comment says why not `hover:no-underline`.
  - `th { font-weight: 400 }` does not leak: TableHead puts `font-medium` on the element itself.
- `body` keeps the legacy text colour (`#0F172A`, where `--foreground` is `#0a0a0a`), so each fully migrated screen's outermost element sets `text-foreground`. The stack shell's `<section>` must not: it wraps unmigrated screens (the templates list, the template pages, access), and they would change colour.
- Migrated controls keep the 44px target on coarse pointers that `--legacy-touch-target` gave them:
  - `pointer-coarse:h-11` on every Button, Input and tab;
  - `pointer-coarse:size-11` on icon buttons;
  - `pointer-coarse:py-3` on a table row's link, which fills its cell.
- Each task deletes the legacy rules it stopped using. The dead-CSS guard (`src/styles/legacy.guard.test.ts`) can't see a class whose name survives elsewhere in a `.tsx` file, such as the `stacks-list` test ID. Task 3 deletes those rules by hand and checks with `rg`.
- Tests find elements by role, text, label, test ID or `data-slot`, never by legacy class name. Tests that click or type into Base UI components use `@testing-library/user-event`.
- No `src/test/setup.ts`. Tabs, Table and every test below run in jsdom without stand-ins; the plan's prototype confirmed it.
- Commit subjects are lowercase-prefixed (`feat(web):`, `refactor(web):`, `test(web):`, `chore:`, `docs:`). Every commit message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A route where the selected tab and the current-page link could disagree.** `stackSection()` derives the tab list's `value` from the pathname, and each tab's `NavLink` sets `aria-current` on its own. Examples are `/stacks/:id/templates/new`, which belongs to Templates, and a path no tab owns. Expected: on every stack route exactly one tab is selected, and it is the one marked `aria-current="page"`. On a path no tab owns, none is selected. Task 2 tests all five stack routes, and the no-tab case.
2. **A keyboard user moving through the stack tabs.** Expected: one Tab stop for the whole row, at the active tab. ArrowLeft and ArrowRight move focus without navigating, and Enter follows the focused tab. Task 2 tests it in jsdom.
3. **A stack or credential with a very long name, on a 375px phone.** Expected: the column keeps its width, the name ends in an ellipsis and shows whole on hover, and the page doesn't scroll sideways. Tasks 3 and 5 test `truncate` and `title`. Task 7 measures `scrollWidth` at 375px with long names served by `--respond`.
4. **Someone types a secret into the credential form.** Expected:
   - it is masked (`type="password"`);
   - both fields clear once the server has it;
   - a failed add keeps what was typed, so they can try again;
   - the page counts as unsaved while anything is typed.

   Task 5 tests all four.
5. **Someone on a slow network presses Enter twice on Create stack.** Expected: one stack is created. The button disables while the request is out, and a disabled default button blocks implicit submission. Task 4 tests it.

---

### Setup: branch and plan commit

- [ ] **Step 1: Create the branch and commit this plan**

From the repository root:

```bash
git switch feat/shadcn-standalone-screens
git pull --ff-only
git switch -c feat/shadcn-stacks
git add docs/superpowers/plans/2026-09-30-shadcn-stacks.md
git commit -m "docs: implementation plan for the stacks screens (PR 4)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 1: Vendor Table and Tabs; keep every Table's layout fixed

The spec's table guard lands with the first Table migration, which is this PR. It has three rules:

- no raw `<table>` outside `src/components/ui/`;
- every `<Table>` has a `<colgroup>`;
- `table.tsx` always applies `table-fixed`.

Two things the spec doesn't spell out:

1. **Two files still have raw tables until PR 8.** `TemplateRunHistory` and the legacy `data-table` specimen on `/styleguide` keep them. The guard lists both in `LEGACY_TABLES` and holds them to the old `.data-table` rules. A second test fails once a listed file no longer has a raw table, so the list can only shrink.
2. **Where `table-fixed` goes decides whether a caller can undo it.** `cn` merges classes the way tailwind-merge does: `cn("table-fixed", "table-auto")` returns `table-auto`. With the CLI's `cn("w-full caption-bottom text-sm", className)`, a caller could turn fixed layout off. So `table-fixed` goes after `className`, and the guard renders `<Table className="table-auto">` to prove it stays.

The guard also checks that each `<Table>`'s first child is a `<colgroup>`. Counting `<colgroup>`s per file isn't enough, because `StyleGuide.tsx` also holds the legacy table's.

**Files:**
- Create (by the CLI): `web/src/components/ui/table.tsx`, `web/src/components/ui/tabs.tsx`
- Modify: `web/src/components/ui/table.tsx` (the `<table>` element's `className`)
- Rewrite: `web/src/styles/tables.guard.test.ts`
- Modify: `web/src/dev/StyleGuide.tsx`
- Test: `web/src/dev/StyleGuide.test.tsx`

**Interfaces:**
- Consumes: the theme colours in `src/styles/theme.css`; `cn` from `@/lib/utils`.
- Produces (from the CLI; only `Table` edited):
  - `import { Table, TableHeader, TableBody, TableFooter, TableHead, TableRow, TableCell, TableCaption } from "@/components/ui/table"`.
    - `Table` renders `<div data-slot="table-container" class="relative w-full overflow-x-auto">` around `<table data-slot="table">`. The table's classes are `w-full caption-bottom text-sm`, then the caller's, then `table-fixed`.
    - `TableHead` is `<th>` with `h-10 px-2 text-left font-medium whitespace-nowrap text-foreground`.
    - `TableCell` is `<td>` with `p-2 align-middle whitespace-nowrap`.
    - `TableRow` is `<tr>` with `border-b hover:bg-muted/50`.
    - `TableBody` removes the last row's border.
  - `import { Tabs, TabsList, TabsTrigger, TabsContent, tabsListVariants } from "@/components/ui/tabs"`, on Base UI Tabs.
    - `Tabs` takes `value`, or `defaultValue`; `null` means no tab is active.
    - `TabsList` takes `variant?: "default" | "line"`. It is `role="tablist"` and `h-8`, set through `group-data-horizontal/tabs:h-8`.
    - `TabsTrigger` takes `value`, `nativeButton` and `render`. It is `role="tab"`, carries `aria-selected`, and has `data-active` when active. In the `line` variant the active tab is underlined by an `::after` bar.

- [ ] **Step 1: Vendor the components**

Run: `npx shadcn add table tabs -y`
Then run: `git status --short`
Expected: exactly two new files, `src/components/ui/table.tsx` and `src/components/ui/tabs.tsx`. `package.json`, the lockfile, `theme.css` and `components.json` are unchanged. If anything else changed, inspect it with `git diff` before going on, and revert what the CLI rewrote without need. `table.tsx` starts with `"use client"`; leave it as the CLI wrote it.

- [ ] **Step 2: Rewrite the table guard**

Replace the whole of `web/src/styles/tables.guard.test.ts` with:

```ts
import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Table } from "@/components/ui/table";

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), "..");

function componentFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return componentFiles(path);
    return entry.name.endsWith(".tsx") && !entry.name.includes(".test.") ? [path] : [];
  });
}

// Files still on the legacy .data-table. Each leaves this list when its screen
// PR moves it to <Table>, and the legacy rules below go with the last one.
const LEGACY_TABLES = [join("features", "runs", "TemplateRunHistory.tsx"), join("dev", "StyleGuide.tsx")];

// Every table has fixed layout, with its column widths declared in a
// <colgroup>. Left to automatic layout, a table re-sizes its columns whenever
// a cell's content changes, so a status that moves from queued to
// waiting_approval shifts every column after it.
describe("tables", () => {
  const files = componentFiles(SRC_DIR)
    .map((path) => ({ name: relative(SRC_DIR, path), source: readFileSync(path, "utf8") }))
    .filter(({ name }) => !name.startsWith(`components${sep}ui${sep}`));

  it("finds the tables it guards", () => {
    expect(files.some(({ source }) => /<Table\b/.test(source))).toBe(true);
  });

  it("builds every table on the shadcn Table", () => {
    const raw = files
      .filter(({ name, source }) => /<table\b/.test(source) && !LEGACY_TABLES.includes(name))
      .map(({ name }) => name);
    expect(raw, `use <Table> from @/components/ui/table in: ${raw.join(", ")}`).toEqual([]);
  });

  it("opens every Table with a <colgroup>", () => {
    const violations = files.flatMap(({ name, source }) =>
      [...source.matchAll(/<Table\b[^>]*>/g)]
        .filter((match) => !source.slice(match.index + match[0].length).trimStart().startsWith("<colgroup>"))
        .map((match) => `${name}: ${match[0]} is not followed by <colgroup>`)
    );
    expect(violations, violations.join("\n")).toEqual([]);
  });

  // cn() merges classes like tailwind-merge, so a caller's table-auto would
  // silently replace table-fixed if the Table let it.
  it("keeps the Table's layout fixed whatever class the caller passes", () => {
    const markup = renderToStaticMarkup(createElement(Table, { className: "table-auto" }));
    const table = markup.match(/<table [^>]*class="([^"]*)"/)?.[1] ?? "";
    expect(table.split(" ")).toContain("table-fixed");
    expect(table.split(" ")).not.toContain("table-auto");
  });

  describe("legacy tables", () => {
    const legacy = files.filter(({ name }) => LEGACY_TABLES.includes(name));

    // The list only shrinks: a file that no longer has a raw table leaves it.
    it("lists only files that still have a raw table", () => {
      expect(legacy.map(({ name }) => name).sort()).toEqual([...LEGACY_TABLES].sort());
      const migrated = legacy.filter(({ source }) => !/<table\b/.test(source)).map(({ name }) => name);
      expect(migrated, `remove from LEGACY_TABLES: ${migrated.join(", ")}`).toEqual([]);
    });

    // Every legacy <table> is a .data-table (primitives.css): fixed layout
    // with its widths in a <colgroup>, inside a .data-table-frame.
    it("builds every legacy table on .data-table", () => {
      const violations: string[] = [];
      for (const { name, source } of legacy) {
        const tables = [...source.matchAll(/<table\b([^>]*)>/g)];
        for (const [tag, attributes] of tables) {
          if (!/className=[{"][^>]*\bdata-table\b/.test(attributes)) violations.push(`${name}: ${tag} lacks the data-table class`);
        }
        const colgroups = source.match(/<colgroup>/g)?.length ?? 0;
        if (colgroups < tables.length) violations.push(`${name}: ${tables.length} table(s) but ${colgroups} <colgroup>`);
        const frames = source.match(/\bdata-table-frame\b/g)?.length ?? 0;
        if (frames < tables.length) violations.push(`${name}: ${tables.length} table(s) but ${frames} data-table-frame`);
      }
      expect(violations, violations.join("\n")).toEqual([]);
    });
  });
});
```

- [ ] **Step 3: Run it to see it fail**

Run: `npx vitest run src/styles/tables.guard.test.ts`
Expected: FAIL in exactly two tests.
- `finds the tables it guards` fails because no app code uses `<Table>` yet (`expected false to be true`).
- `keeps the Table's layout fixed whatever class the caller passes` fails because the stock Table lets `table-auto` through.

The other four pass.

- [ ] **Step 4: Make the Table's layout fixed**

In `web/src/components/ui/table.tsx`, in `function Table`, replace:

```tsx
        className={cn("w-full caption-bottom text-sm", className)}
```

with:

```tsx
        // Edited from the CLI's output: table-fixed comes after className so
        // no caller can undo it. With automatic layout a cell's content sizes
        // its column, and a status going from queued to waiting_approval
        // shifts every column after it. tables.guard.test.ts checks this, and
        // that every <Table> declares its widths in a <colgroup>.
        className={cn("w-full caption-bottom text-sm", className, "table-fixed")}
```

Run: `npx vitest run src/styles/tables.guard.test.ts`
Expected: only `finds the tables it guards` still fails. The `/styleguide` specimen below is the first `<Table>`.

- [ ] **Step 5: Write the failing styleguide test**

In `web/src/dev/StyleGuide.test.tsx`, add inside `describe("StyleGuide", …)`, before `it("renders the real RouteMessage", …)`:

```tsx
  it("shows the components PR 4 added", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    const table = section.getByRole("table");
    expect(table.getAttribute("data-slot")).toBe("table");
    expect(table.classList).toContain("table-fixed");
    expect(table.querySelector("colgroup")).not.toBeNull();
    expect(section.getByRole("tablist").getAttribute("data-variant")).toBe("line");
    expect(section.getByRole("tab", { name: "Templates" }).getAttribute("aria-selected")).toBe("true");
  });
```

Run: `npx vitest run src/dev/StyleGuide.test.tsx`
Expected: FAIL on `shows the components PR 4 added`, `Unable to find an accessible element with the role "table"`. The legacy `data-table` specimen sits outside `sg-theme`, so it doesn't count.

- [ ] **Step 6: Add the specimens**

In `web/src/dev/StyleGuide.tsx`, add next to the other `@/components/ui` imports:

```tsx
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
```

Inside the `theme` Section, after the `RouteMessage` specimen and before `</Section>`, add:

```tsx
            <Specimen label="Table" hint="fixed layout: widths in a <colgroup>, one column takes the slack" stack>
              <div className="w-full overflow-hidden rounded-lg border">
                <Table>
                  <colgroup>
                    <col />
                    <col className="w-48" />
                  </colgroup>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead>Slug</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {[
                      ["Payments", "payments"],
                      ["A stack whose name is too long for its column", "a-stack-whose-name-is-too-long-for-its-column"]
                    ].map(([name, slug]) => (
                      <TableRow key={slug}>
                        <TableCell className="truncate font-medium" title={name}>
                          {name}
                        </TableCell>
                        <TableCell className="truncate font-mono text-xs text-muted-foreground" title={slug}>
                          {slug}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </Specimen>
            <Specimen label="Tabs" hint="line variant; RouteTabs renders each tab as a link" stack>
              <Tabs defaultValue="templates">
                <TabsList variant="line" aria-label="Tabs specimen">
                  <TabsTrigger value="overview">Overview</TabsTrigger>
                  <TabsTrigger value="templates">Templates</TabsTrigger>
                  <TabsTrigger value="environment">Environment</TabsTrigger>
                </TabsList>
                <TabsContent value="overview">The stack at a glance.</TabsContent>
                <TabsContent value="templates">The templates installed on this stack.</TabsContent>
                <TabsContent value="environment">Credentials every template on this stack receives.</TabsContent>
              </Tabs>
            </Specimen>
```

The Tabs specimen uses plain triggers, not `RouteTabs`. The page mounts inside the app's router, and a `RouteTab` there would navigate the app away from `/styleguide`.

- [ ] **Step 7: Run the styleguide test, the guards and the type-check**

Run: `npx vitest run src/dev src/styles && npx tsc -b`
Expected: all pass; `tsc` reports no errors. The class guard skips `src/components/ui/`, so the vendored arbitrary values (`p-[3px]`, `bottom-[-5px]`) don't trip it.

- [ ] **Step 8: Commit**

```bash
git add src/components/ui/table.tsx src/components/ui/tabs.tsx src/styles/tables.guard.test.ts src/dev/StyleGuide.tsx src/dev/StyleGuide.test.tsx
git commit -m "feat(web): add the shadcn Table and Tabs" -m "The Table applies table-fixed after the caller's className, since cn would let a caller's table-auto replace it. The table guard now requires <Table> with a leading <colgroup>, and holds the two files still on the legacy .data-table to the old rules until PR 8. Tabs arrives two PRs early for the stack shell's tab row. Both get a /styleguide specimen." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `RouteTabs`, and the stack shell's tabs on it

The stack shell's tab row is four `NavLink`s in a `nav`. It becomes shadcn's line Tabs with each tab rendered as a `NavLink`, which is Base UI's documented "tabs as links" pattern (`nativeButton={false}` and `render`). Several things follow:

- **The route drives the selection.** Tabs is controlled: `value` comes from the pathname, so a click or Enter follows the link and the selection moves with the route. No `onValueChange` is needed.
- **`NavLink` still sets `aria-current="page"`.** Base UI merges props onto the rendered element, so the active tab carries both `aria-selected="true"` and `aria-current="page"`. The prototype confirmed this, both server-rendered and in jsdom.
- **Keyboard.** Tab reaches only the active tab. ArrowLeft and ArrowRight move focus without navigating (`activateOnFocus` is off by default), and Enter follows the focused link.
- **No tab panel.** The routed `<Outlet>` is what each tab shows.
- **44px on coarse pointers.** A `pointer-coarse:h-11` on each tab wins over the trigger's `h-[calc(100%-1px)]`. The list's `group-data-horizontal/tabs:h-8` is overridden by `pointer-coarse:h-auto`, so the taller tabs fit inside it. Both overrides work because Tailwind 4.3 compiles `group-data-*` inside `:where()`, which adds no specificity. Source order then decides, and the `@media (pointer: coarse)` block comes after both vendored rules in the built CSS.

The template shell (`StackTemplateDetailShell.tsx`) keeps its legacy `stack-detail-tabs` row until PR 6. So the `.stack-detail-tabs` rules in `features.css` stay, and the dead-CSS guard stays green.

**Files:**
- Create: `web/src/shared/RouteTabs.tsx`
- Test: `web/src/shared/RouteTabs.test.tsx`
- Modify: `web/src/features/stacks/StackDetailShell.tsx`
- Test: `web/src/features/stacks/StackDetailShell.test.tsx`

**Interfaces:**
- Consumes: `Tabs`, `TabsList`, `TabsTrigger` (Task 1); `NavLink` from `react-router-dom`; `cn`.
- Produces, in `src/shared/RouteTabs.tsx`:
  - `export function RouteTabs(props: { value: string | null; label: string; children: ReactNode; className?: string })`. It renders `<Tabs value={value} className="mb-6 …">` around a `line` TabsList named by `label`. `className` replaces the `mb-6` below the row; PR 6 passes `"mb-0"` where the row sits beside the template's state.
  - `export function RouteTab(props: { value: string; to: string; end?: boolean; children: ReactNode })`. A TabsTrigger rendered as `<NavLink to={to} end={end}>`.

- [ ] **Step 1: Write the failing `RouteTabs` test**

Create `web/src/shared/RouteTabs.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import { RouteTab, RouteTabs } from "./RouteTabs";

afterEach(cleanup);

// The value the route selects, as a screen derives it from its pathname.
const SECTIONS: Record<string, string> = { "/s": "overview", "/s/templates": "templates", "/s/access": "access" };

function Screen() {
  const { pathname } = useLocation();
  return (
    <>
      <RouteTabs value={SECTIONS[pathname] ?? null} label="Stack sections">
        <RouteTab value="overview" to="/s" end>
          Overview
        </RouteTab>
        <RouteTab value="templates" to="/s/templates">
          Templates
        </RouteTab>
        <RouteTab value="access" to="/s/access">
          Access
        </RouteTab>
      </RouteTabs>
      <p data-testid="pathname">{pathname}</p>
    </>
  );
}

function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="*" element={<Screen />} />
      </Routes>
    </MemoryRouter>
  );
  return userEvent.setup();
}

const pathname = () => screen.getByTestId("pathname").textContent;

describe("RouteTabs", () => {
  it("is a tab list named by its label, with a link for each tab", () => {
    renderAt("/s/templates");
    const list = screen.getByRole("tablist", { name: "Stack sections" });
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((tab) => tab.textContent)).toEqual(["Overview", "Templates", "Access"]);
    expect(tabs.map((tab) => tab.getAttribute("href"))).toEqual(["/s", "/s/templates", "/s/access"]);
    expect(tabs.every((tab) => list.contains(tab))).toBe(true);
  });

  // The route picks the tab, and the link agrees with it.
  it("selects the route's tab and marks its link as the current page", () => {
    renderAt("/s/templates");
    const templates = screen.getByRole("tab", { name: "Templates" });
    expect(templates.getAttribute("aria-selected")).toBe("true");
    expect(templates.getAttribute("aria-current")).toBe("page");
    for (const name of ["Overview", "Access"]) {
      expect(screen.getByRole("tab", { name }).getAttribute("aria-selected")).toBe("false");
    }
  });

  it("follows a clicked tab to its route, and the selection goes with it", async () => {
    const user = renderAt("/s/templates");
    await user.click(screen.getByRole("tab", { name: "Access" }));
    expect(pathname()).toBe("/s/access");
    expect(screen.getByRole("tab", { name: "Access" }).getAttribute("aria-selected")).toBe("true");
  });

  // The tab pattern: one Tab stop for the row, arrows between tabs, Enter to open.
  it("takes one Tab stop, moves with the arrow keys, and opens the focused tab with Enter", async () => {
    const user = renderAt("/s/templates");
    await user.tab();
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Templates" }));

    await user.keyboard("{ArrowRight}");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: "Access" }));
    expect(pathname()).toBe("/s/templates");

    await user.keyboard("{Enter}");
    expect(pathname()).toBe("/s/access");
  });

  it("selects nothing on a route that belongs to no tab", () => {
    renderAt("/s/elsewhere");
    expect(screen.getAllByRole("tab").map((tab) => tab.getAttribute("aria-selected"))).toEqual(["false", "false", "false"]);
  });

  // --legacy-touch-target gave every tab 44px on a touch screen. The list
  // drops its fixed height there so the taller tabs fit inside it.
  it("gives every tab a 44px target on coarse pointers", () => {
    renderAt("/s");
    expect(screen.getByRole("tablist").classList).toContain("pointer-coarse:h-auto");
    for (const tab of screen.getAllByRole("tab")) {
      expect(tab.classList).toContain("pointer-coarse:h-11");
    }
  });

  // base.css underlines every bare <a> on hover until PR 9.
  it("keeps the legacy link underline off its tabs", () => {
    renderAt("/s");
    for (const tab of screen.getAllByRole("tab")) {
      expect(tab.classList).toContain("no-underline");
    }
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npx vitest run src/shared/RouteTabs.test.tsx`
Expected: FAIL, `Failed to resolve import "./RouteTabs"`.

- [ ] **Step 3: Write `RouteTabs`**

Create `web/src/shared/RouteTabs.tsx`:

```tsx
import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

/**
 * A row of tabs where each tab is a route: shadcn's Tabs, with every trigger
 * rendered as a NavLink (Base UI's "tabs as links"). The route decides which
 * tab is active, so the caller derives `value` from the pathname, and a click
 * or Enter follows the link instead of setting state. Base UI supplies the
 * tablist semantics and the keyboard: Tab reaches the active tab, the arrow
 * keys move between tabs, and Enter opens the focused one.
 *
 * There is no tab panel. The route's own content, rendered by an <Outlet>, is
 * what each tab shows.
 */
export function RouteTabs({
  value,
  label,
  children,
  className
}: {
  /** The active tab's value, or null when the route matches none. */
  value: string | null;
  /** Names the tab list for assistive technology. */
  label: string;
  children: ReactNode;
  /** Replaces the page spacing below the row; a row that sits beside other content passes "mb-0". */
  className?: string;
}) {
  return (
    <Tabs value={value} className={cn("mb-6", className)}>
      {/* On a coarse pointer each tab is 44px tall, so the list drops its
          fixed h-8 and grows to fit them. */}
      <TabsList variant="line" aria-label={label} className="pointer-coarse:h-auto">
        {children}
      </TabsList>
    </Tabs>
  );
}

/**
 * One tab of a RouteTabs row. `to` and `end` are NavLink's: `end` keeps a
 * parent route's tab from matching its children.
 *
 * Until PR 9, base.css underlines every bare <a> on hover from the legacy
 * layer, so the tab sets no-underline itself (AppShell's navLinkClass says
 * why not hover:no-underline).
 */
export function RouteTab({ value, to, end, children }: { value: string; to: string; end?: boolean; children: ReactNode }) {
  return (
    <TabsTrigger
      value={value}
      nativeButton={false}
      render={<NavLink to={to} end={end} />}
      className="no-underline pointer-coarse:h-11"
    >
      {children}
    </TabsTrigger>
  );
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `npx vitest run src/shared/RouteTabs.test.tsx`
Expected: PASS, 7 tests.

- [ ] **Step 5: Add the failing shell tests**

In `web/src/features/stacks/StackDetailShell.test.tsx`, add before `it("still renders NotFound (no shell chrome) when canView is denied", …)`:

```tsx
  // The tab's selected state comes from stackSection(), and its link's
  // aria-current from NavLink. They must name the same tab on every route,
  // or a screen reader hears one tab selected and another current.
  it.each([
    ["/stacks/stack_1", "Overview"],
    ["/stacks/stack_1/templates", "Templates"],
    ["/stacks/stack_1/templates/new", "Templates"],
    ["/stacks/stack_1/environment", "Environment"],
    ["/stacks/stack_1/access", "Access"]
  ])("selects exactly one tab at %s: %s, the one its link marks current", async (path, tab) => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute(path, allAllowed);

    const tabs = [...markup.matchAll(/<a [^>]*role="tab"[^>]*>([^<]*)<\/a>/g)].map(([tag, label]) => ({
      label,
      selected: tag.includes('aria-selected="true"'),
      current: tag.includes('aria-current="page"')
    }));
    expect(tabs.map(({ label }) => label)).toEqual(["Overview", "Templates", "Environment", "Access"]);
    expect(tabs.filter(({ selected }) => selected).map(({ label }) => label)).toEqual([tab]);
    expect(tabs.filter(({ current }) => current).map(({ label }) => label)).toEqual([tab]);
  });

  it("names the stack's tab list", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const markup = await renderStackRoute("/stacks/stack_1", allAllowed);

    expect(markup).toMatch(/<div [^>]*role="tablist"[^>]*aria-label="Stack sections"/);
  });
```

- [ ] **Step 6: Run them to see them fail**

Run: `npx vitest run src/features/stacks/StackDetailShell.test.tsx`
Expected: FAIL in the six new tests. The five route cases fail with `expected [] to deeply equal [ 'Overview', 'Templates', …(2) ]`, because the legacy links have no `role="tab"`. The tab-list test fails on the regex. The other twelve pass.

- [ ] **Step 7: Move the shell's tabs onto `RouteTabs`**

In `web/src/features/stacks/StackDetailShell.tsx`:

1. Replace the imports, from the first line down to `import { stackTemplateLabel } from "./stackWorkflow";`, with:

   ```tsx
   import { matchPath, Outlet, useLocation, useParams } from "react-router-dom";
   import { useStackQuery } from "../../api/queries";
   import RequireCapability from "../../auth/RequireCapability";
   import { tenantID } from "../../config";
   import Breadcrumb from "../../shared/Breadcrumb";
   import type { Crumb } from "../../shared/Breadcrumb";
   import { RouteTab, RouteTabs } from "../../shared/RouteTabs";
   import { stackTemplateLabel } from "./stackWorkflow";

   // The stack tab a path belongs to: the same match each tab's NavLink makes,
   // so the selected tab is always the one marked aria-current. Templates owns
   // everything below it; Overview is the stack's own path only.
   function stackSection(pathname: string): string | null {
     if (matchPath("/stacks/:stackId", pathname)) return "overview";
     if (matchPath({ path: "/stacks/:stackId/templates", end: false }, pathname)) return "templates";
     if (matchPath("/stacks/:stackId/environment", pathname)) return "environment";
     if (matchPath("/stacks/:stackId/access", pathname)) return "access";
     return null;
   }
   ```

   `stackSection` is not exported. A component file that exports anything else loses Vite's fast refresh.

2. Replace the `return ( … );` block of `StackDetailShell` with:

   ```tsx
     return (
       // No text colour here: the shell wraps screens still on the legacy
       // layer (templates, access), which inherit theirs from body until they
       // migrate. RouteTabs sets its own.
       <section data-testid="stack-detail-shell">
         <Breadcrumb items={crumbs} />
         {!currentTemplate && (
           <RouteTabs value={stackSection(pathname)} label="Stack sections">
             <RouteTab value="overview" to="." end>
               Overview
             </RouteTab>
             <RouteTab value="templates" to="templates">
               Templates
             </RouteTab>
             <RequireCapability capability="canManageAccess">
               <RouteTab value="environment" to="environment">
                 Environment
               </RouteTab>
               <RouteTab value="access" to="access">
                 Access
               </RouteTab>
             </RequireCapability>
           </RouteTabs>
         )}
         <Outlet />
       </section>
     );
   ```

   The `stack-detail-shell` class goes: no CSS rule uses it.

- [ ] **Step 8: Run every test that renders the shell**

Run: `npx vitest run src/shared src/features/stacks src/app`
Expected: all pass. `StackTemplatePages.test.tsx` still finds the template's tabs as links with an `active` class, because the template shell is untouched.

- [ ] **Step 9: Confirm no CSS went dead, and type-check**

Run: `npx vitest run src/styles && npx tsc -b`
Expected: all pass; `tsc` reports no errors. `.stack-detail-tabs` still has users (`StackTemplateDetailShell.tsx`, and the legacy Tabs specimen on `/styleguide`).

- [ ] **Step 10: Commit**

```bash
git add src/shared/RouteTabs.tsx src/shared/RouteTabs.test.tsx src/features/stacks/StackDetailShell.tsx src/features/stacks/StackDetailShell.test.tsx
git commit -m "feat(web): stack tabs on the shadcn Tabs" -m "RouteTabs renders shadcn's line Tabs with each tab a NavLink, Base UI's tabs-as-links pattern. The route picks the active tab, the link still marks itself aria-current, and Base UI adds the tab list's keyboard: one Tab stop, arrows between tabs, Enter to open. Tests pin that the selected and current tabs agree on every stack route. PR 6 reuses it for the template's tabs." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: The stacks list on Table and Empty

The list becomes a Table with two columns: Name, which takes the slack, and Slug (`w-48`). It sits in shadcn's bordered frame (`overflow-hidden rounded-lg border`). The frame's `overflow-hidden` clips the row hover fill to the rounded corners.

A row's name is its link. The link fills its cell (`block`, with the cell at `p-0` and the link at `p-2`), so the whole cell is the target, and `pointer-coarse:py-3` makes it 44px on a touch screen. A name or slug longer than its column is cut with `truncate` and shown whole in `title`.

The empty state becomes an Empty with a dashed `border` and a real `h2` in EmptyTitle's look, since EmptyTitle renders a `div`. This drops `HeroGraphic`, `useInView`, and the `showcase`, `reveal` and `gradient-text` classes from this screen. The registry's empty state still uses them until PR 7, so their CSS stays.

**Files:**
- Modify: `web/src/features/stacks/StacksListScreen.tsx` (whole file)
- Test: `web/src/features/stacks/StacksListScreen.test.tsx`
- Modify: `web/src/styles/features.css` (the stacks list rules, and three comments)
- Modify: `web/src/dev/StyleGuide.tsx` (one specimen hint)

**Interfaces:**
- Consumes: `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableHead`, `TableCell` (Task 1); `Empty`, `EmptyHeader`, `EmptyMedia`, `EmptyDescription` (PR 3); `Button`, `buttonVariants` (PR 1); `Breadcrumb` (PR 2).
- Produces: nothing new. The test IDs listed in Global Constraints stay on the same states.

- [ ] **Step 1: Add the failing tests**

In `web/src/features/stacks/StacksListScreen.test.tsx`, change the Testing Library import to:

```tsx
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
```

Add these tests inside `describe("StacksListScreen", …)`, after `renders an empty state when the API returns no visible stacks`:

```tsx
  it("lists the stacks in a table, one row each, name then slug", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [
      stack(),
      stack({ id: "stack_2", name: "Billing", slug: "billing" })
    ]);

    renderScreen(queryClient);

    const table = screen.getByRole("table");
    expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Name", "Slug"]);
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell").map((cell) => cell.textContent))).toEqual([
      ["Payments", "payments"],
      ["Billing", "billing"]
    ]);
    expect(within(rows[0]).getByRole("link", { name: "Payments" }).getAttribute("href")).toBe("/stacks/stack_1");
  });

  // Fixed layout keeps each column's width whatever a cell holds. A name or
  // slug longer than its column ends in an ellipsis, and hovering shows it
  // whole.
  it("cuts a long name or slug to its column and shows it whole on hover", () => {
    const name = "payments-core-production-eu-west-1-and-then-some";
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack({ name, slug: name })]);

    renderScreen(queryClient);

    const link = screen.getByRole("link", { name });
    expect(link.getAttribute("title")).toBe(name);
    expect(link.classList).toContain("truncate");
    const slug = within(screen.getByRole("table")).getAllByRole("cell")[1];
    expect(slug.getAttribute("title")).toBe(name);
    expect(slug.classList).toContain("truncate");
  });

  it("declares the table's column widths, so a long name moves no column", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    renderScreen(queryClient);

    const table = screen.getByRole("table");
    expect(table.classList).toContain("table-fixed");
    expect(table.querySelectorAll("colgroup > col")).toHaveLength(2);
  });

  it("says so, under a heading, when there are no stacks", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), []);

    renderScreen(queryClient);

    const empty = screen.getByTestId("stacks-list-empty");
    expect(empty.getAttribute("data-slot")).toBe("empty");
    expect(within(empty).getByRole("heading", { level: 2, name: "No stacks yet" })).toBeTruthy();
    expect(within(empty).getByText("No stacks visible to you yet.")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  // base.css gives every h2 the legacy 32px display type until PR 9.
  it("sets the empty state's heading type itself", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), []);

    renderScreen(queryClient);

    const heading = screen.getByRole("heading", { level: 2 }).classList;
    for (const name of ["font-heading", "text-sm", "font-medium", "tracking-tight"]) {
      expect(heading).toContain(name);
    }
  });

  // --legacy-touch-target gave every control 44px on a touch screen. The
  // row's link fills its cell, so padding it out makes the whole cell 44px.
  it("gives every control a 44px target on coarse pointers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    renderScreen(queryClient);

    expect(screen.getByTestId("create-stack-link").classList).toContain("pointer-coarse:h-11");
    const link = screen.getByRole("link", { name: "Payments" }).classList;
    expect(link).toContain("block");
    expect(link).toContain("pointer-coarse:py-3");
  });

  it("gives the retry button a 44px target on coarse pointers", async () => {
    vi.spyOn(globalThis, "fetch").mockRejectedValue(new TypeError("network down"));

    renderScreen(testQueryClient());

    await waitFor(() => expect(screen.getByTestId("stacks-list-retry").classList).toContain("pointer-coarse:h-11"));
  });

  // Until PR 9, body keeps the legacy text colour and base.css colours bare
  // links. The screen and its links set their own.
  it("sets its own text colour and its links' colour", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stacks("tenant_123"), [stack()]);

    const { container } = renderScreen(queryClient);

    expect((container.firstElementChild as HTMLElement).classList).toContain("text-foreground");
    const link = screen.getByRole("link", { name: "Payments" }).classList;
    expect(link).toContain("text-foreground");
    expect(link).toContain("no-underline");
  });
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/stacks/StacksListScreen.test.tsx`
Expected: FAIL in all eight new tests:
- the two table tests, with `Unable to find an accessible element with the role "table"`;
- the long-name test, with `expected null to be 'payments-core-…'`, since the link has no `title`;
- the empty-state test, with `expected null to be 'empty'`;
- the class tests, on `showcase__title gradient-text`, `primary-button` and `stacks-list-screen`.

The ten existing tests pass.

- [ ] **Step 3: Rewrite the screen**

Replace the whole of `web/src/features/stacks/StacksListScreen.tsx` with:

```tsx
import { Layers, Loader2, Plus, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { useStacksQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Button, buttonVariants } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// The list is authz-filtered by the backend (AUTH-013) — the screen renders
// whatever listStacks returns and never filters client-side.
//
// Until PR 9, body keeps the legacy text colour and base.css styles bare a
// and h2 from the legacy layer, so the screen sets its own colour and the
// links and heading set their own type and decoration.
export default function StacksListScreen() {
  const { data: stacks, status, error, refetch } = useStacksQuery(tenantID);
  const boundary = useQueryErrorBoundary(error);

  if (status === "pending") {
    return (
      <section className="text-foreground" data-testid="stacks-list-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading stacks…
        </p>
      </section>
    );
  }

  if (status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4 text-foreground" data-testid="stacks-list-error">
        <Breadcrumb items={[{ label: "Stacks" }]} className="mb-0" />
        <p className="text-muted-foreground">Something went wrong while loading stacks.</p>
        <Button className="pointer-coarse:h-11" data-testid="stacks-list-retry" onClick={() => refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  return (
    <section className="text-foreground">
      <header className="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <Breadcrumb items={[{ label: "Stacks" }]} className="mb-0" />
        <RequireCapability capability="canCreateStack">
          {/* A link that looks like the page's primary action. Full width on
              a phone, where the header stacks. */}
          <Link
            className={cn(buttonVariants(), "w-full no-underline pointer-coarse:h-11 md:w-auto")}
            to="/stacks/new"
            data-testid="create-stack-link"
          >
            <Plus data-icon="inline-start" aria-hidden="true" />
            Create stack
          </Link>
        </RequireCapability>
      </header>
      {stacks.length === 0 ? (
        <Empty className="border" data-testid="stacks-list-empty">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Layers aria-hidden="true" />
            </EmptyMedia>
            <h2 className="font-heading text-sm font-medium tracking-tight">No stacks yet</h2>
            <EmptyDescription>No stacks visible to you yet.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        // shadcn's bordered table frame; overflow-hidden clips the rows'
        // hover fill to its rounded corners.
        <div className="overflow-hidden rounded-lg border" data-testid="stacks-list">
          <Table>
            <colgroup>
              <col />
              <col className="w-48" />
            </colgroup>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Slug</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stacks.map((stack) => (
                <TableRow key={stack.id}>
                  {/* The link fills its cell, so the whole cell is the
                      target: 44px tall on a coarse pointer. Fixed layout
                      keeps the column's width whatever the name, so a long
                      one ends in an ellipsis, whole on hover. The focus
                      outline is drawn inside, where the frame can't clip it. */}
                  <TableCell className="p-0">
                    <Link
                      className="block truncate p-2 font-medium text-foreground no-underline hover:underline focus-visible:-outline-offset-2 pointer-coarse:py-3"
                      to={`/stacks/${stack.id}`}
                      title={stack.name}
                    >
                      {stack.name}
                    </Link>
                  </TableCell>
                  <TableCell className="truncate font-mono text-xs text-muted-foreground" title={stack.slug}>
                    {stack.slug}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  );
}
```

`focus-visible:-outline-offset-2` pulls the legacy focus outline (`:focus-visible` in `base.css`, 2px with a 2px offset) inside the link. Otherwise the frame's `overflow-hidden` would clip it on the first and last rows.

- [ ] **Step 4: Run the screen's tests and the router's**

Run: `npx vitest run src/features/stacks/StacksListScreen.test.tsx src/app/router.test.tsx`
Expected: all pass. The router test finds `data-testid="stacks-list"` on the frame.

- [ ] **Step 5: Run the dead-CSS guard to see what is now unused**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `styles/features.css styles only classes a component uses`, with `delete the rules for: stacks-list-header`.

The guard misses the rest. `.stacks-list` and every `.stacks-list li …` rule are dead too, but the name survives as the `stacks-list` test ID. Delete them all by hand.

- [ ] **Step 6: Delete the stacks list rules**

In `web/src/styles/features.css`:

1. In the `/* Compact variant, … */` comment above `.showcase--compact`, replace:

   ```css
   /* Compact variant, used by the stacks empty state in Task 6: it sits inside
      a page that already has a header, so it neither fills the viewport nor
      needs a full-size title. These two values are the only difference. */
   ```

   with:

   ```css
   /* Compact variant, used by the registry's empty state: it sits inside a page
      that already has a header, so it neither fills the viewport nor needs a
      full-size title. These two values are the only difference. */
   ```

2. Replace everything from `/* ---- Stacks list ---- */` down to, but not including, `/* ---- Templates list ---- */` with:

   ```css
   /* ---- Title rows and the templates list's cards ---- */
   .templates-list-header,
   .template-detail-header {
     display: flex;
     align-items: center;
     justify-content: space-between;
     gap: var(--legacy-space-4);
     margin-bottom: var(--legacy-space-6);
   }

   .templates-list {
     display: grid;
     gap: var(--legacy-space-3);
     margin: 0;
     padding: 0;
     list-style: none;
   }

   .templates-list li {
     display: flex;
     align-items: center;
     gap: var(--legacy-space-4);
     border: 1px solid var(--legacy-color-border);
     border-radius: var(--legacy-radius-xl);
     padding: var(--legacy-space-2) var(--legacy-space-4);
     background: var(--legacy-color-card);
     box-shadow: var(--legacy-shadow-sm);
     transition: all var(--legacy-duration-lift) var(--legacy-ease-out);
   }

   .templates-list li:hover {
     transform: translateY(-2px);
     border-color: var(--legacy-color-accent-border);
     box-shadow: var(--legacy-shadow-lg);
   }

   /* Row names. */
   .templates-list__name {
     color: var(--legacy-color-fg);
     font-size: var(--legacy-text-base);
     font-weight: 400;
     text-decoration: none;
   }

   ```

   Every declaration is the one the templates list had before. Only the `.stacks-list` selectors and rules, the stacks empty-state comment, and the comments about sharing go. The two `.templates-list__name` rules merge into one, which is safe: they were adjacent, with the same selector.

3. In the `/* ---- Templates list ---- */` section, replace the comment `/* Typography is set with .stacks-list li a above; only the wrapping differs. */` with `/* Typography is set above, with the cards; only the wrapping differs. */`.

4. In the `@media (max-width: 760px)` block, delete the line `  .stacks-list-header,` from the header-row rule, and the line `  .stacks-list-header .primary-button,` from the full-width CTA rule.

In `web/src/dev/StyleGuide.tsx`, change the showcase specimen's hint from `"stacks and registry empty states"` to `"registry empty state"`.

- [ ] **Step 7: Check nothing is left**

Run: `rg -n "stacks-list" src/styles`
Expected: no output.

- [ ] **Step 8: Run the guards, the whole suite and the type-check**

Run: `npx vitest run src/styles && npm test && npx tsc -b`
Expected: all pass; `tsc` reports no errors. `HeroGraphic`, `useInView` and the showcase CSS still have users (`TemplateRegistryScreen`, `/styleguide`).

- [ ] **Step 9: Commit**

```bash
git add src/features/stacks/StacksListScreen.tsx src/features/stacks/StacksListScreen.test.tsx src/styles/features.css src/dev/StyleGuide.tsx
git commit -m "feat(web): stacks list on the shadcn Table and Empty" -m "Stacks list in a fixed-layout Table, name then slug. A long value ends in an ellipsis and shows whole on hover, and each row's link fills its cell so the cell is a 44px target on touch. The empty state is an Empty in place of the showcase and HeroGraphic. The stacks list CSS goes; the dead-CSS guard couldn't see most of it, because the name lives on as a test ID." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Create stack on Card, Label, Input, Button and Alert

The form keeps its layout: two columns on a wide screen, so the name field takes half the card, and the submit button on its own row at its own width. On a phone it is one column with a full-width button. The legacy `form-grid` gave the same, and so did the phone-width rule that made its button full width. The legacy `form-grid`, `panel` and `alert` rules stay, because other screens use them.

The name `<input>` gains an `id`, because Label points at its field with `htmlFor` and doesn't wrap it. The failure message moves into an Alert, which sets `role="alert"`, so a screen reader announces it. The legacy `.alert` div had no role.

**Files:**
- Modify: `web/src/features/stacks/CreateStackScreen.tsx` (imports, and everything from `if (mutation.isSuccess && mutation.data) {` to the end)
- Test: `web/src/features/stacks/CreateStackScreen.test.tsx`

**Interfaces:**
- Consumes: `Card`, `CardContent`, `Alert`, `AlertTitle`, `Input`, `Label` (PR 3); `Button` (PR 1); `Breadcrumb` (PR 2).
- Produces: nothing new. The new field ID is `create-stack-name`.

- [ ] **Step 1: Add the tests**

In `web/src/features/stacks/CreateStackScreen.test.tsx`, add after the Testing Library import:

```tsx
import userEvent from "@testing-library/user-event";
```

Add these tests inside `describe("CreateStackScreen", …)`, before `shows an inline error message for a handled API error status`:

```tsx
  it("labels the name field with a shadcn Label and Input", () => {
    renderScreen();

    const field = screen.getByLabelText("Name");
    expect(field.getAttribute("data-slot")).toBe("input");
    expect(document.querySelector(`label[for="${field.id}"]`)?.getAttribute("data-slot")).toBe("label");
  });

  it("puts the cursor in the name field", () => {
    renderScreen();

    expect(document.activeElement).toBe(screen.getByLabelText("Name"));
  });

  it("creates the stack from the keyboard", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(stack()));
    renderScreen();

    await userEvent.setup().type(screen.getByLabelText("Name"), "My Stack{Enter}");

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
    expect(JSON.parse(fetchSpy.mock.calls[0]?.[1]?.body as string).name).toBe("My Stack");
  });

  // A slow answer must not turn a second Enter into a second stack.
  it("creates one stack when Enter is pressed again while the first is in flight", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
    renderScreen();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Name"), "My Stack{Enter}");
    await screen.findByRole("button", { name: /creating/i });
    await user.type(screen.getByLabelText("Name"), "{Enter}");

    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });

  // role="alert" is how a screen reader hears the failure. Alert sets it, so
  // the screen must not add a second one.
  it("announces a failed create once", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "conflict", message: "A stack with that slug already exists" }, 409)
    );
    renderScreen();

    await userEvent.setup().type(screen.getByLabelText("Name"), "My Stack{Enter}");

    await screen.findByTestId("create-stack-error");
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0].textContent).toContain("A stack with that slug already exists");
  });

  // --legacy-touch-target gave every control 44px on a touch screen.
  it("gives every control a 44px target on coarse pointers", () => {
    renderScreen();

    expect(screen.getByLabelText("Name").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: /create stack/i }).classList).toContain("pointer-coarse:h-11");
  });

  it("sits in a shadcn Card and sets its own text colour", () => {
    renderScreen();

    const form = screen.getByLabelText("Name").closest("form") as HTMLElement;
    expect(form.closest('[data-slot="card"]')).not.toBeNull();
    expect(form.closest("[data-unsaved], section")?.classList).toContain("text-foreground");
  });
```

- [ ] **Step 2: Run them and read the result carefully**

Run: `npx vitest run src/features/stacks/CreateStackScreen.test.tsx`
Expected: FAIL in exactly four tests:
- `labels the name field with a shadcn Label and Input`, with `expected null to be 'input'`;
- `announces a failed create once`, with `Unable to find an accessible element with the role "alert"`;
- `gives every control a 44px target on coarse pointers`;
- `sits in a shadcn Card and sets its own text colour`.

The other three new tests pass against today's markup: focus on arrival, Enter to create, and one stack for two Enters. That is on purpose: they pin behaviour the move to Base UI must keep. If any of them fails now, stop and find out why before going on.

- [ ] **Step 3: Rewrite the markup**

In `web/src/features/stacks/CreateStackScreen.tsx`, replace the lucide import with:

```tsx
import { CircleAlert, Loader2 } from "lucide-react";
```

add after the `Breadcrumb` import:

```tsx
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
```

and replace everything from `  if (mutation.isSuccess && mutation.data) {` to the end of the file with:

```tsx
  if (mutation.isSuccess && mutation.data) {
    return (
      <section className="text-foreground" data-testid="create-stack-success">
        <p className="text-muted-foreground">Redirecting to your new stack…</p>
      </section>
    );
  }

  return (
    // Read by SessionProvider's proactive re-auth timer: it defers navigating
    // away while a `[data-unsaved='true']` element is mounted, so a
    // half-typed stack name is never wiped out by a background sign-in
    // redirect.
    <section className="text-foreground" data-unsaved={trimmed !== "" ? "true" : undefined}>
      <Breadcrumb items={[{ label: "Stacks", to: "/stacks" }, { label: "Create stack" }]} />

      <Card>
        <CardContent className="grid gap-4">
          {errorMessage && (
            <Alert variant="destructive" data-testid="create-stack-error">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{errorMessage}</AlertTitle>
            </Alert>
          )}

          {/* Two columns on a wide screen, as the legacy form grid had, so
              the name field takes half the card. The button takes a row of
              its own and its own width, or the full width on a phone. */}
          <form className="grid gap-5 md:grid-cols-2" onSubmit={handleSubmit}>
            <div className="grid gap-2">
              <Label htmlFor="create-stack-name">Name</Label>
              <Input
                id="create-stack-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Production"
                autoFocus
                className="pointer-coarse:h-11"
              />
            </div>
            <Button
              type="submit"
              className="w-full pointer-coarse:h-11 md:col-span-2 md:w-auto md:justify-self-start"
              disabled={trimmed === "" || mutation.isPending}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
                  Creating…
                </>
              ) : (
                "Create stack"
              )}
            </Button>
          </form>
        </CardContent>
      </Card>
    </section>
  );
}
```

- [ ] **Step 4: Run the screen's tests and the router's**

Run: `npx vitest run src/features/stacks/CreateStackScreen.test.tsx src/app/router.test.tsx`
Expected: all pass. The existing tests click with `fireEvent.click`, which sends the plain click Base UI's Button handles.

- [ ] **Step 5: Confirm no CSS went dead, and type-check**

Run: `npx vitest run src/styles && npx tsc -b`
Expected: all pass; `tsc` reports no errors. `panel`, `alert` and `form-grid` still have users (`TemplateRegistrationScreen` among them), so nothing is deleted.

- [ ] **Step 6: Commit**

```bash
git add src/features/stacks/CreateStackScreen.tsx src/features/stacks/CreateStackScreen.test.tsx
git commit -m "feat(web): create stack on the shadcn Card, Input and Alert" -m "The form sits in a Card with a Label and Input and a shadcn Button, and a failed create shows in an Alert that screen readers announce. New tests pin what the move to Base UI must keep: focus on arrival, Enter to create, one stack for two Enters, and 44px touch targets." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The credentials panel and the Environment tab

`CredentialsPanel` is shared with the template's Credentials tab (`TemplateCredentialsTab.tsx`, PR 6), so that tab's panel changes here too. Its legacy wrapper, `.stack-template-tab`, moves in PR 6.

The panel becomes a Card:
- **Header.** The title is a real `h2` in CardTitle's look, since CardTitle renders a `div`. The subtitle and the write-only note are CardDescriptions.
- **List.** The credentials list is a Table with columns Name (the slack), Value (`w-28`, reading "configured") and an actions column (`w-16`, holding a 44px icon button plus the cell's padding), whose header is `sr-only` "Actions".
- **Form.** The name field, value field and Add button sit in a row, stacked on a phone. Both fields keep their `aria-label`s and have no visible label, as today.
- **Errors.** Validation and server errors show in an Alert below the form, where `.error-text` sat.

**Files:**
- Modify: `web/src/features/stacks/CredentialsPanel.tsx` (whole file)
- Modify: `web/src/features/stacks/EnvironmentScreen.tsx` (whole file)
- Test: `web/src/features/stacks/EnvironmentScreen.test.tsx`
- Modify: `web/src/styles/features.css` (the credentials rules)
- Modify: `web/src/styles/legacy.guard.test.ts` (one comment's example)

**Interfaces:**
- Consumes: `Table` and its parts (Task 1); `Card`, `CardHeader`, `CardDescription`, `CardContent`, `Alert`, `AlertTitle`, `Input` (PR 3); `Button` (PR 1).
- Produces: `CredentialsPanel` keeps its props exactly (`title`, `subtitle?`, `credentials`, `loading`, `busy`, `onCreate`, `onDelete`), so `TemplateCredentialsTab` needs no change.

- [ ] **Step 1: Add the tests**

In `web/src/features/stacks/EnvironmentScreen.test.tsx`, change the Testing Library import to:

```tsx
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
```

Add above `function renderScreen`:

```tsx
const credential = { id: "credential_1", name: "TF_VAR_TEST", scope: "stack" as const, created_at: "2026-07-19T00:00:00Z" };

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}
```

Add these tests inside `describe("EnvironmentScreen", …)`, before `deletes a stack credential through the existing API mutation`:

```tsx
  it("lists the credentials in a table, one row each, with a delete button", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      credential,
      { ...credential, id: "credential_2", name: "AWS_ACCESS_KEY_ID" }
    ]);

    renderScreen(queryClient);

    const table = screen.getByRole("table");
    expect(table.classList).toContain("table-fixed");
    expect(within(table).getAllByRole("columnheader").map((cell) => cell.textContent)).toEqual(["Name", "Value", "Actions"]);
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getAllByRole("cell").slice(0, 2).map((cell) => cell.textContent))).toEqual([
      ["TF_VAR_TEST", "configured"],
      ["AWS_ACCESS_KEY_ID", "configured"]
    ]);
    expect(within(rows[1]).getByRole("button", { name: "Delete AWS_ACCESS_KEY_ID" })).toBeTruthy();
  });

  it("says so when no credentials are configured", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(screen.getByText("No credentials configured")).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  // base.css gives every h2 the legacy 32px display type until PR 9.
  it("titles the panel with an h2 that sets its own type", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    const heading = screen.getByRole("heading", { level: 2, name: "Environment credentials" });
    for (const name of ["font-heading", "text-base", "font-medium", "tracking-normal"]) {
      expect(heading.classList).toContain(name);
    }
    expect(heading.closest('[data-slot="card"]')?.classList).toContain("text-foreground");
  });

  // The value is a secret: masked while typed, and gone from the page once
  // the server has it. A failed add keeps it, so the user can try again.
  it("masks the secret, and clears both fields once the credential is added", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(credential, 201));
    renderScreen(queryClient);
    const user = userEvent.setup();

    const name = screen.getByLabelText<HTMLInputElement>("Environment credential name");
    const value = screen.getByLabelText<HTMLInputElement>("Environment credential value");
    expect(value.type).toBe("password");
    await user.type(name, "TF_VAR_TEST");
    await user.type(value, "secret-value");
    await user.click(screen.getByRole("button", { name: "Add" }));

    await waitFor(() => expect(value.value).toBe(""));
    expect(name.value).toBe("");
    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
  });

  it("keeps what was typed, and says why, when the add fails", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ error: "conflict", message: "A credential with that name already exists" }, 409)
    );
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Environment credential name"), "TF_VAR_TEST");
    await user.type(screen.getByLabelText("Environment credential value"), "secret-value");
    await user.click(screen.getByRole("button", { name: "Add" }));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("A credential with that name already exists");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(screen.getByLabelText<HTMLInputElement>("Environment credential value").value).toBe("secret-value");
  });

  it("asks for both fields before sending anything", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    renderScreen(queryClient);
    const user = userEvent.setup();

    await user.type(screen.getByLabelText("Environment credential name"), "TF_VAR_TEST");
    await user.click(screen.getByRole("button", { name: "Add" }));

    expect((await screen.findByRole("alert")).textContent).toContain("Name and value are required");
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("uses shadcn fields for the credential form", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    for (const label of ["Environment credential name", "Environment credential value"]) {
      expect(screen.getByLabelText(label).getAttribute("data-slot")).toBe("input");
    }
  });

  // --legacy-touch-target gave every control 44px on a touch screen.
  it("gives every control a 44px target on coarse pointers", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [credential]);

    renderScreen(queryClient);

    expect(screen.getByLabelText("Environment credential name").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByLabelText("Environment credential value").classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: "Add" }).classList).toContain("pointer-coarse:h-11");
    expect(screen.getByRole("button", { name: "Delete TF_VAR_TEST" }).classList).toContain("pointer-coarse:size-11");
  });

  it("retries a failed load", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ error: "internal", message: "boom" }, 500))
      .mockResolvedValueOnce(jsonResponse([credential]));

    renderScreen(testQueryClient());

    const retry = await screen.findByTestId("environment-retry");
    expect(retry.classList).toContain("pointer-coarse:h-11");
    await userEvent.setup().click(retry);
    await screen.findByTestId("environment-screen");
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
```

- [ ] **Step 2: Run them and read the result carefully**

Run: `npx vitest run src/features/stacks/EnvironmentScreen.test.tsx`
Expected: FAIL in exactly seven tests:
- `lists the credentials in a table…`, with `Unable to find an accessible element with the role "table"`;
- `titles the panel with an h2…`;
- `keeps what was typed…` and `asks for both fields…`, with `Unable to find role="alert"`: `.error-text` has no role;
- `uses shadcn fields…`;
- `gives every control a 44px target…`;
- `retries a failed load`, on its `pointer-coarse:h-11` assertion.

`masks the secret, and clears both fields…` and `says so when no credentials are configured` pass against today's markup. They pin behaviour the move must keep. If either fails now, stop and find out why.

- [ ] **Step 3: Rewrite `CredentialsPanel`**

Replace the whole of `web/src/features/stacks/CredentialsPanel.tsx` with:

```tsx
import { useState } from "react";
import { CircleAlert, Loader2, Plus, Trash2 } from "lucide-react";
import type { CredentialMetadata } from "../../api/types";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

interface CredentialsPanelProps {
  title: string;
  // Distinguishes this panel's scope from a near-identically-named one
  // elsewhere (e.g. the stack-scoped Environment tab). Optional so existing
  // call sites that have no such ambiguity to resolve need no change.
  subtitle?: string;
  credentials: CredentialMetadata[];
  loading: boolean;
  busy: boolean;
  onCreate: (name: string, value: string) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

/**
 * Renders write-only credential management for either a Stack or StackTemplate scope.
 *
 * Until PR 9, body keeps the legacy text colour and base.css gives every h2
 * the legacy 32px display type, so the card sets its own colour and the
 * heading sets its own family, size, weight and tracking.
 */
export default function CredentialsPanel({ title, subtitle, credentials, loading, busy, onCreate, onDelete }: CredentialsPanelProps) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");

  /** Validates the form, sends the value once, then clears it from local state. */
  async function submit() {
    if (!name.trim() || !value) {
      setError("Name and value are required");
      return;
    }
    setError("");
    try {
      await onCreate(name.trim(), value);
      setName("");
      setValue("");
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Request failed");
    }
  }

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted. A half-typed
  // credential secret is the worst instance of the loss this guard exists to
  // prevent — the value never reaches the server until Add is clicked, so
  // losing it means retyping a password from scratch.
  const hasUnsavedCredential = name !== "" || value !== "";
  const fieldLabel = title.replace(/ credentials$/, " credential");

  return (
    <Card className="text-foreground" data-unsaved={hasUnsavedCredential ? "true" : undefined}>
      <CardHeader>
        <h2 className="font-heading text-base leading-snug font-medium tracking-normal">{title}</h2>
        {subtitle && <CardDescription>{subtitle}</CardDescription>}
        <CardDescription>
          Values are write-only and injected only when Terraform runs. Use TF_VAR_NAME for Terraform variables; provider credentials keep their provider-specific names.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        {loading ? (
          <p className="flex items-center gap-2 text-muted-foreground">
            <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading credentials…
          </p>
        ) : credentials.length === 0 ? (
          <p className="text-muted-foreground">No credentials configured</p>
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <colgroup>
                <col />
                <col className="w-28" />
                <col className="w-16" />
              </colgroup>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {credentials.map((credential) => (
                  <TableRow key={credential.id}>
                    <TableCell className="truncate font-mono" title={credential.name}>
                      {credential.name}
                    </TableCell>
                    <TableCell className="text-muted-foreground">configured</TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="pointer-coarse:size-11"
                        disabled={busy}
                        onClick={() => void onDelete(credential.id)}
                        aria-label={`Delete ${credential.name}`}
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {/* Name, value and Add on one row; stacked on a phone. */}
        <div className="flex flex-col gap-4 md:flex-row">
          <Input
            aria-label={`${fieldLabel} name`}
            placeholder="AWS_ACCESS_KEY_ID"
            value={name}
            onChange={(event) => setName(event.target.value)}
            className="pointer-coarse:h-11 md:flex-1"
          />
          <Input
            aria-label={`${fieldLabel} value`}
            placeholder="Secret value"
            type="password"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            className="pointer-coarse:h-11 md:flex-1"
          />
          <Button variant="outline" className="pointer-coarse:h-11" disabled={busy} onClick={() => void submit()}>
            <Plus data-icon="inline-start" aria-hidden="true" />
            Add
          </Button>
        </div>
        {error && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{error}</AlertTitle>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 4: Rewrite `EnvironmentScreen`**

Replace the whole of `web/src/features/stacks/EnvironmentScreen.tsx` with:

```tsx
import { RefreshCw } from "lucide-react";
import { useParams } from "react-router-dom";
import {
  useCreateStackCredentialMutation,
  useDeleteStackCredentialMutation,
  useStackCredentialsQuery
} from "../../api/queries";
import { tenantID } from "../../config";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import CredentialsPanel from "./CredentialsPanel";
import { Button } from "@/components/ui/button";

export default function EnvironmentScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const credentialsQuery = useStackCredentialsQuery(tenantID, stackId);
  const createMutation = useCreateStackCredentialMutation(tenantID, stackId);
  const deleteMutation = useDeleteStackCredentialMutation(tenantID, stackId);
  const boundary = useQueryErrorBoundary(credentialsQuery.error);

  if (credentialsQuery.status === "pending") {
    return (
      <section className="text-foreground" data-testid="environment-loading">
        <p className="text-muted-foreground">Loading environment…</p>
      </section>
    );
  }

  if (credentialsQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4 text-foreground" data-testid="environment-error">
        <p className="text-muted-foreground">Something went wrong while loading the environment.</p>
        <Button className="pointer-coarse:h-11" data-testid="environment-retry" onClick={() => credentialsQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  return (
    <section data-testid="environment-screen">
      <CredentialsPanel
        title="Environment credentials"
        credentials={credentialsQuery.data ?? []}
        loading={credentialsQuery.isPending}
        busy={createMutation.isPending || deleteMutation.isPending}
        onCreate={async (name, value) => {
          await createMutation.mutateAsync({ name, value });
        }}
        onDelete={(id) => deleteMutation.mutateAsync(id)}
      />
    </section>
  );
}
```

- [ ] **Step 5: Run every test that renders the panel**

Run: `npx vitest run src/features/stacks`
Expected: all pass. `StackTemplatePages.test.tsx` covers the template's Credentials tab. It finds the panel's title and subtitle by text, and the credential names by text.

- [ ] **Step 6: Run the dead-CSS guard to see what is now unused**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `styles/features.css styles only classes a component uses`, with `delete the rules for: credentials-panel, credentials-list, credential-row, credential-form`.

- [ ] **Step 7: Delete the credentials rules**

In `web/src/styles/features.css`:

1. Delete the whole `/* ---- Credentials ---- */` block, from that comment down to, but not including, `/* ---- User search ---- */`. It holds the `.credentials-panel`, `.credentials-list`, `.credential-row` and `.credential-form` rules.
2. In the `@media (max-width: 760px)` block, delete the rule:

   ```css
     .credential-form,
     .credential-row {
       grid-template-columns: 1fr;
     }
   ```

In `web/src/styles/legacy.guard.test.ts`, the comment in `describe("legacy field rules", …)` gives `.credential-form input` as its example of a descendant rule. No such rule ever existed, and the class is gone now. Replace:

```ts
  // Only rules that start at the element are checked. A descendant rule such
  // as `.credential-form input` names a legacy class, and when a screen PR
  // migrates that markup the class goes with it and the dead-CSS guard above
  // makes the rule go too.
```

with:

```ts
  // Only rules that start at the element are checked. A descendant rule such
  // as `.checkbox-label input[type="checkbox"]` names a legacy class, and when
  // a screen PR migrates that markup the class goes with it and the dead-CSS
  // guard above makes the rule go too.
```

- [ ] **Step 8: Check nothing is left**

Run: `rg -n "credential" src/styles`
Expected: no output.

- [ ] **Step 9: Run the guards, the whole suite and the type-check**

Run: `npx vitest run src/styles && npm test && npx tsc -b`
Expected: all pass; `tsc` reports no errors.

- [ ] **Step 10: Commit**

```bash
git add src/features/stacks/CredentialsPanel.tsx src/features/stacks/EnvironmentScreen.tsx src/features/stacks/EnvironmentScreen.test.tsx src/styles/features.css src/styles/legacy.guard.test.ts
git commit -m "feat(web): credentials on the shadcn Card, Table and Input" -m "The credentials panel, shared by a stack's Environment tab and a template's Credentials tab, is a Card with a fixed-layout Table of names, a row of shadcn fields and an Alert for errors. The Environment tab's loading and error states move with it. New tests pin that the secret stays masked, clears once added, and survives a failed add. The credentials CSS goes." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Let `drive-web.mjs` answer and stall requests

The audit has to reach two states PR 3's `--fail` can't produce:
- the stacks list's empty state, which needs a 200 with `[]`;
- each loading state, which needs a request that never ends.

It also needs long names that the local database doesn't have. This task adds two flags beside `--fail`, sharing its glob handling and its timing, since both take effect after sign-in:

- `--respond <glob>=<json>` answers matching requests with 200 and that JSON body.
- `--stall <glob>` holds matching requests and never answers them.

One pitfall: the script's header is a `/* … */` comment, so a usage example there can't contain `*/`. A glob like `'*/v1/…'` would close the comment early. That is why the existing example reads `'*v1/me=500'`.

**Files:**
- Modify: `scripts/drive-web.mjs`

**Interfaces:**
- Consumes: nothing.
- Produces, for Task 7: `node scripts/drive-web.mjs [--respond '<glob>=<json>']… [--stall '<glob>']… …`, with the other flags unchanged. A request matched by several flags gets `--stall` first, then `--respond`, then `--fail`.

- [ ] **Step 1: Document the flags**

In the header comment of `scripts/drive-web.mjs`, add under `Usage:`, after the `--fail` example:

```js
 *   node scripts/drive-web.mjs --respond '*v1/tenants/*stacks=[]' --reload --shot empty.png
```

Under `Flags:`, add after the `--fail` entry:

```js
 *   --respond <glob>=<json>
 *                     answer requests whose URL matches glob with 200 and that
 *                     JSON body (repeatable), such as an empty list. Split at
 *                     the first "=" whose remainder parses as JSON. Globs and
 *                     timing as for --fail.
 *   --stall <glob>    never answer requests whose URL matches glob
 *                     (repeatable), so their screen stays in its loading
 *                     state. Globs and timing as for --fail.
```

- [ ] **Step 2: Parse and check them**

1. Replace `const clicks = [], fails = [], evals = [];` with:

   ```js
   const clicks = [], fails = [], evals = [], responds = [], stalls = [];
   ```

2. In the argument loop, after the `--fail` line, add:

   ```js
     else if (argv[i] === "--respond") responds.push(next());
     else if (argv[i] === "--stall") stalls.push(next());
   ```

3. Replace the `--fail` spec parsing:

   ```js
   // --fail specs, split at the last "=" so a glob may contain one.
   const failures = fails.map((spec = "") => {
     const at = spec.lastIndexOf("=");
     const glob = at > 0 ? spec.slice(0, at) : "";
     const pattern = glob.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
     return { glob, status: Number(spec.slice(at + 1)), test: new RegExp(`^${pattern}$`) };
   });
   ```

   with:

   ```js
   // Chrome's URL pattern syntax, as a regex that picks which spec a paused
   // request belongs to.
   const globTest = (glob) =>
     new RegExp(`^${glob.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".")}$`);

   // --fail specs, split at the last "=" so a glob may contain one.
   const failures = fails.map((spec = "") => {
     const at = spec.lastIndexOf("=");
     const glob = at > 0 ? spec.slice(0, at) : "";
     return { glob, status: Number(spec.slice(at + 1)), test: globTest(glob) };
   });
   ```

4. After the `if (failures.some(…)) { … process.exit(2); }` block, add:

   ```js
   // --respond specs, split at the first "=" whose remainder is JSON, so a glob
   // may contain one and so may the body.
   const responses = responds.map((spec = "") => {
     for (let at = spec.indexOf("="); at > 0; at = spec.indexOf("=", at + 1)) {
       try {
         const body = JSON.stringify(JSON.parse(spec.slice(at + 1)));
         return { glob: spec.slice(0, at), body, test: globTest(spec.slice(0, at)) };
       } catch {
         // Not JSON from here; the "=" belongs to the glob.
       }
     }
     return { glob: "" };
   });
   if (responses.some((r) => !r.glob || r.glob.includes("\\"))) {
     console.error("--respond takes <glob>=<json>: a glob with no backslash and a JSON body, for example '*/v1/tenants/*/stacks=[]'.");
     process.exit(2);
   }

   const stalled = stalls.map((glob = "") => ({ glob, test: globTest(glob) }));
   if (stalled.some((s) => !s.glob || s.glob.includes("\\"))) {
     console.error("--stall takes a glob with no backslash, for example '*/v1/tenants/*/stacks'.");
     process.exit(2);
   }
   const intercepted = [...stalled, ...responses, ...failures];
   ```

   The backslash check is the one `--fail` makes, and for the same reason: Chrome's pattern reads a backslash as an escape, and the regex doesn't.

- [ ] **Step 3: Answer the paused requests**

Replace the comment and first line of `answer`:

```js
// Chrome pauses each request a --fail glob matches and waits for an answer.
// The body is the API's error shape, so the app's error handling runs as it
// would for a real failure.
const answer = ({ requestId, request }) => {
  const failure = failures.find((f) => f.test.test(request.url));
```

with:

```js
// Chrome pauses each request a --stall, --respond or --fail glob matches and
// waits for an answer. A stalled one never gets one. A --fail body is the
// API's error shape, so the app's error handling runs as it would for a real
// failure.
const answer = ({ requestId, request }) => {
  if (stalled.some((s) => s.test.test(request.url))) return Promise.resolve();
  const response = responses.find((r) => r.test.test(request.url));
  if (response) {
    return send("Fetch.fulfillRequest", {
      requestId,
      responseCode: 200,
      responseHeaders: [{ name: "Content-Type", value: "application/json" }],
      body: Buffer.from(response.body).toString("base64")
    });
  }
  const failure = failures.find((f) => f.test.test(request.url));
```

In the `Fetch.requestPaused` branch of the message listener, replace:

```js
    // A request left paused hangs the page, and the screenshot would show that
    // rather than the error state. Let it through and fail the run instead.
    answer(msg.params).catch((error) => {
      console.error("--fail: Chrome refused the answer:", error.message);
```

with:

```js
    // A request left paused by accident hangs the page, and the screenshot
    // would show that rather than the state asked for. Let it through and
    // fail the run instead.
    answer(msg.params).catch((error) => {
      console.error("Chrome refused the answer:", error.message);
```

- [ ] **Step 4: Intercept every flag's requests after sign-in**

Replace:

```js
if (failures.length) {
  await send("Fetch.enable", { patterns: failures.map((f) => ({ urlPattern: f.glob, requestStage: "Request" })) });
}
```

with:

```js
if (intercepted.length) {
  await send("Fetch.enable", { patterns: intercepted.map((i) => ({ urlPattern: i.glob, requestStage: "Request" })) });
}
```

The comment above it (`// After sign-in, so a --fail on /v1/me doesn't stop the driver signing in.`) stays as it is.

- [ ] **Step 5: Check the syntax and the argument checks**

Run from the repository root:

```bash
node --check scripts/drive-web.mjs
bash -c '
check() { node scripts/drive-web.mjs --signed-out --port 1 "$@" 2>&1 | head -1; echo "  exit=${PIPESTATUS[0]} for [$*]"; }
check --respond bad
check --respond "*nojson=notjson"
check --respond "=[]"
check --respond "a\\b=[]"
check --stall
check --stall "a\\b"
'
```

Expected: `node --check` prints nothing. Each `--respond` case prints the `--respond takes <glob>=<json>…` message and `exit=2`. Each `--stall` case prints the `--stall takes a glob…` message and `exit=2`. Run it under `bash`: zsh doesn't split `$@`-style words the same way, and every case would pass one unknown argument.

- [ ] **Step 6: Try it against the running app**

This needs the local stack (`docker compose --profile auth up -d` from the repository root) and a dev server on port 5173 started from `web/` on this branch (`npm run dev`, after `docker compose stop web`, which frees the port). It also needs headless Chrome, as the script's header describes. From the repository root, export the credentials without printing them:

```bash
export OPENPLAN_USER="$(grep '^KEYCLOAK_PLATFORM_ADMIN_USERNAME=' .env | cut -d= -f2-)"
export OPENPLAN_PASS="$(grep '^KEYCLOAK_PLATFORM_ADMIN_PASSWORD=' .env | cut -d= -f2-)"
node scripts/drive-web.mjs --respond '*/v1/tenants/*/stacks=[]' --reload --probe '!!document.querySelector("[data-testid=stacks-list-empty]")'
node scripts/drive-web.mjs --stall '*/v1/tenants/*/stacks' --reload --probe '!!document.querySelector("[data-testid=stacks-list-loading]")'
node scripts/drive-web.mjs --probe '!!document.querySelector("[data-testid=stacks-list]")'
```

Expected: `true`, `true`, `true`. The empty state and the loading state are both reached, and without the flags the list is back. The third needs at least one stack in the local database.

- [ ] **Step 7: Commit**

From the repository root:

```bash
git add scripts/drive-web.mjs
git commit -m "chore: let drive-web answer and stall requests" -m "--respond <glob>=<json> answers matching requests with 200 and that body, and --stall <glob> never answers them. With --fail they reach a screen's empty, loading and error states, and long values the local database doesn't have." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Browser audit, spec amendments, and the PR

This audit checks two things:

1. The screens PR 4 didn't migrate must not move. Their computed styles are compared before and after. Two migrated pieces sit inside them:
   - the stack tab row, above the templates list and the access screen;
   - the credentials card, on a template's Credentials tab.

   Only those two pieces' margins are compared.
2. The migrated screens must look right, in every state: screenshots, plus the checks in Review Focus.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`
- Scratch, not committed: everything under `$AUDIT`

Scratch files live in `$AUDIT`, a directory outside the repository: this session's scratchpad directory plus `/audit4`. Shell variables don't survive between commands here, so every command block below starts by exporting it, along with the driver's credentials where they're needed.

**Interfaces:**
- Consumes: everything above; `scripts/drive-web.mjs` with `--respond` and `--stall` (Task 6), and `--fail`, `--eval`, `--reload` and `--width` (PR 3).
- Produces: the PR.

- [ ] **Step 1: Save the dump and diff scripts**

Save as `$AUDIT/dump.js`. It is PR 3's dump plus a migrated-piece exemption, keyed so that a `nav` becoming a `div` isn't read as a DOM change:

```js
(() => {
  const PROPS = ["display", "position", "vertical-align", "list-style-type", "min-height",
    "margin-top", "margin-right", "margin-bottom", "margin-left",
    "padding-top", "padding-right", "padding-bottom", "padding-left",
    "border-top-width", "border-top-style", "border-top-color", "border-bottom-width", "border-bottom-style",
    "font-family", "font-size", "font-weight", "line-height", "letter-spacing",
    "color", "background-color", "box-shadow", "text-decoration-line", "cursor"];
  // What PR 4 migrated inside screens it otherwise leaves alone: the stack's
  // tab row (a <nav>, now shadcn's Tabs) and the credentials card on a
  // template's Credentials tab. Their insides change on purpose, so only
  // their margins are compared, under a key that doesn't name the tag.
  const MIGRATED = 'nav[aria-label="Stack sections"], [data-slot="tabs"], .credentials-panel, [data-slot="card"]';
  const OUTER = ["margin-top", "margin-right", "margin-bottom", "margin-left"];
  const main = document.getElementById("main-content");
  const pick = (el, props) => {
    const cs = getComputedStyle(el);
    return Object.fromEntries(props.map((p) => [p, cs.getPropertyValue(p)]));
  };
  const out = { main: pick(main, PROPS) };
  const walk = (el, path) => {
    [...el.children].forEach((child, i) => {
      const migrated = child.matches(MIGRATED);
      const key = `${path} > ${migrated ? "migrated" : child.tagName.toLowerCase()}[${i}]`;
      out[key] = pick(child, migrated ? OUTER : PROPS);
      if (!migrated) walk(child, key);
    });
  };
  walk(main, "main");
  return JSON.stringify(out);
})()
```

Save as `$AUDIT/diff.py`. It is PR 3's, unchanged:

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

The local stack must be up (`docker compose --profile auth up -d` from the repository root). The local database needs a stack with an installed template and at least one run of it, and at least one registry template. If any is missing, create it through the UI first.

Sign in once and collect real IDs: click into the stack, one of its templates, one run of that template, and one registry template. After each click, run `node scripts/drive-web.mjs --probe 'location.pathname'`.

Write `$AUDIT/screens.txt`, one `name path` pair per line. These are the screens PR 4 didn't migrate. `overview`, `stack-templates`, `stack-templates-new` and `access` show the stack tab row, and `template-credentials` shows the credentials card; the dump compares only their margins:

```
overview /stacks/{stackId}
stack-templates /stacks/{stackId}/templates
stack-templates-new /stacks/{stackId}/templates/new
template-runs /stacks/{stackId}/templates/{stackTemplateId}/runs
template-run /stacks/{stackId}/templates/{stackTemplateId}/runs/{runNumber}
template-variables /stacks/{stackId}/templates/{stackTemplateId}/variables
template-credentials /stacks/{stackId}/templates/{stackTemplateId}/credentials
template-settings /stacks/{stackId}/templates/{stackTemplateId}/settings
template-upgrade /stacks/{stackId}/templates/{stackTemplateId}/upgrade
access /stacks/{stackId}/access
registry /templates
registry-new /templates/new
registry-template /templates/{sourceTemplateId}
```

Write `$AUDIT/shots.sh`. It captures the migrated screens in every state PR 4 touched. It takes the output directory, a file prefix, the stack ID and, optionally, a phone width:

```bash
#!/usr/bin/env bash
# Usage: shots.sh <out-dir> <before|after> <stackId> [width]
set -u
out=$1 tag=$2 stack=$3 width=${4:-}
[ -n "$width" ] && tag="$tag-$width"
d() { node scripts/drive-web.mjs ${width:+--width "$width"} "$@"; }
caps='"effectiveCapabilities":{"canView":true,"canOperate":true,"canApprove":true,"canManageAccess":true}'
stack() { printf '{"id":"%s","tenant_id":"t","name":"%s","slug":"%s","tags":{},"default_credential_ids":[],"created_by":"u","created_at":"2026-09-30T00:00:00Z",%s}' "$1" "$2" "$3" "$caps"; }
long="[$(stack s1 "Payments core, production, EU West 1, the one with the long name" payments-core-production-eu-west-1-the-one-with-the-long-name),$(stack s2 Billing billing)]"
creds='[{"id":"c1","name":"TF_VAR_a_credential_name_far_too_long_for_its_column_on_any_screen","scope":"stack","created_at":"2026-09-30T00:00:00Z"},{"id":"c2","name":"AWS_ACCESS_KEY_ID","scope":"stack","created_at":"2026-09-30T00:00:00Z"}]'
submit='(() => {
  const input = document.querySelector("main form input");
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(input, "audit-stack");
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.form.requestSubmit();
  return "submitted";
})()'
env="/stacks/$stack/environment"
d --shot "$out/$tag-stacks.png"
d --respond "*/v1/tenants/*/stacks=$long" --reload --shot "$out/$tag-stacks-long.png"
d --respond '*/v1/tenants/*/stacks=[]' --reload --shot "$out/$tag-stacks-empty.png"
d --stall '*/v1/tenants/*/stacks' --reload --shot "$out/$tag-stacks-loading.png"
d --fail '*/v1/tenants/*/stacks=500' --reload --shot "$out/$tag-stacks-error.png"
d --goto /stacks/new --shot "$out/$tag-create.png"
d --fail '*/v1/tenants/*/stacks=409' --goto /stacks/new --eval "$submit" --shot "$out/$tag-create-error.png"
d --goto "/stacks/$stack" --shot "$out/$tag-overview.png"
d --goto "$env" --shot "$out/$tag-environment.png"
d --respond "*/v1/tenants/*/stacks/*/credentials=$creds" --goto "$env" --shot "$out/$tag-environment-long.png"
d --respond '*/v1/tenants/*/stacks/*/credentials=[]' --goto "$env" --click Add --shot "$out/$tag-environment-invalid.png"
d --stall '*/v1/tenants/*/stacks/*/credentials' --goto "$env" --shot "$out/$tag-environment-loading.png"
d --fail '*/v1/tenants/*/stacks/*/credentials=500' --goto "$env" --reload --shot "$out/$tag-environment-error.png"
d --signed-out --goto /styleguide --shot "$out/$tag-styleguide.png"
```

Some notes on the script:
- **`--fail` on a create.** The `409` on `/stacks` fails only the create: `/stacks/new` sends no list request after sign-in. So `audit-stack` is never created.
- **Why the reloads.** A `--reload` sits where the request goes out at page load (the list) or must exhaust the query's three retries (the Environment error), because interception starts only after sign-in.
- **The glob.** `*/v1/tenants/*/stacks` matches the list and the create, but not `/stacks/<id>`.

- [ ] **Step 3: Capture "before" from `feat/shadcn-standalone-screens`**

From the repository root:

```bash
export AUDIT=<this session's scratchpad>/audit4
export OPENPLAN_USER="$(grep '^KEYCLOAK_PLATFORM_ADMIN_USERNAME=' .env | cut -d= -f2-)"
export OPENPLAN_PASS="$(grep '^KEYCLOAK_PLATFORM_ADMIN_PASSWORD=' .env | cut -d= -f2-)"
git worktree add "$AUDIT/before-tree" feat/shadcn-standalone-screens
(cd "$AUDIT/before-tree/web" && npm ci)
docker compose stop web        # frees port 5173, the only redirect URI Keycloak accepts
(cd "$AUDIT/before-tree/web" && npm run dev) &
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu \
  --remote-debugging-port=9222 --user-data-dir="$AUDIT/chrome" --window-size=1512,950 about:blank &
mkdir -p "$AUDIT/before" "$AUDIT/shots"
while read -r name path; do
  node scripts/drive-web.mjs --goto "$path" --probe "$(cat "$AUDIT/dump.js")" > "$AUDIT/before/$name.json"
done < "$AUDIT/screens.txt"
bash "$AUDIT/shots.sh" "$AUDIT/shots" before <stackId>
bash "$AUDIT/shots.sh" "$AUDIT/shots" before <stackId> 375
```

The driver runs from the main working tree, which has Task 6's flags, against whichever app is on port 5173. Afterwards, stop that dev server (find it with `lsof -i :5173`) and keep Chrome running.

- [ ] **Step 4: Capture "after" from this branch**

Start `npm run dev` from `web/` on `feat/shadcn-stacks`. Then run the same loop into `$AUDIT/after/`, and the two `shots.sh` runs with `after` in place of `before`.

- [ ] **Step 5: Diff**

Run: `python3 "$AUDIT/diff.py" "$AUDIT/before" "$AUDIT/after"`
Expected: `CLEAN`. Every screen PR 4 didn't migrate is unchanged. The tab row keeps its 24px bottom margin, and the credentials card keeps zero margins inside the template tab's grid.

What a difference means depends on where it is:
- **At a `migrated` key on `overview`, `stack-templates`, `stack-templates-new` or `access`:** the tab row's margin changed. Check `RouteTabs`' `mb-6`.
- **At a `migrated` key on `template-credentials`:** the card picked up a margin.
- **Anywhere else:** a rule was deleted too broadly in Task 3 or Task 5.

Fix it in the task that owns it, re-capture "after", and diff again.

- [ ] **Step 6: Check what the diff can't**

Still on the "after" server:

1. **Every state was really reached.** Open each `after-*.png`:
   - `stacks-empty` shows "No stacks yet";
   - `stacks-loading` shows "Loading stacks…";
   - `stacks-error` shows "Something went wrong while loading stacks." and a Retry button;
   - `stacks-long` shows the long name cut with an ellipsis;
   - `create-error` shows a red Alert;
   - `environment-invalid` shows "Name and value are required";
   - `environment-loading` shows "Loading environment…";
   - `environment-error` shows the Retry button;
   - `environment-long` shows the long credential name cut with an ellipsis.

   If one shows the ordinary screen instead, its glob missed. Print the request URLs with `--probe 'JSON.stringify(performance.getEntriesByType("resource").map((e) => e.name).filter((n) => n.includes("/v1/")))'` and correct the glob.
2. **Fields and tabs are shadcn's size on a mouse.** Run:

   ```bash
   node scripts/drive-web.mjs --goto "/stacks/<stackId>/environment" --probe 'JSON.stringify({ inputs: [...document.querySelectorAll("main input")].map((i) => i.getBoundingClientRect().height), tabs: [...document.querySelectorAll("[role=tab]")].map((t) => Math.round(t.getBoundingClientRect().height)) })'
   ```

   Expected: `inputs` is `[32,32]`; `36` means a legacy `min-height` still reaches Input. Every entry in `tabs` is below 32, which is the line tab's height inside the `h-8` list.
3. **Nothing scrolls sideways at phone width, and every control is 44px on touch.** Save this probe as `$AUDIT/measure.js`:

   ```js
   JSON.stringify({
     scroll: document.documentElement.scrollWidth,
     width: innerWidth,
     small: [...document.querySelectorAll("main a[href], main button, main input, [role=tab]")]
       .filter((el) => !el.closest('nav[aria-label="Breadcrumb"]'))
       .map((el) => [el.textContent.trim() || el.getAttribute("aria-label") || el.tagName, Math.round(el.getBoundingClientRect().height)])
       .filter(([, height]) => height < 44)
   })
   ```

   Run it with `--width 375 --probe "$(cat "$AUDIT/measure.js")"` on each of these:
   - `--respond "*/v1/tenants/*/stacks=<the long JSON from shots.sh>" --reload`
   - `--goto /stacks/new`
   - `--respond "*/v1/tenants/*/stacks/*/credentials=<the creds JSON from shots.sh>" --goto "/stacks/<stackId>/environment"`

   Expected: `scroll` is at most `width`, and `small` is `[]`. The breadcrumb links are left out: PR 2 settled them, and they carry no touch target of their own.

Then compare the before and after screenshots side by side. Expected differences:
- the stacks list becomes a bordered table with a Name and a Slug column, in place of a stack of cards;
- the stacks empty state loses the large gradient title and the hero graphic, and shows a dashed Empty with an icon;
- create stack becomes a shadcn card with a 32px field, a shadcn button and a red Alert for errors;
- the stack's tabs become shadcn's line tabs, and the template's own tabs keep the legacy look until PR 6;
- the credentials panel, on both the Environment tab and a template's Credentials tab, becomes a card with a table, shadcn fields and an Alert;
- `/styleguide` gains the Table and Tabs specimens.

Nothing else should change.

Clean up from the repository root: stop Chrome and the dev server, run `docker compose start web`, then `git worktree remove --force "$AUDIT/before-tree"`.

- [ ] **Step 7: Bring the spec in line with what PR 4 built**

Edit `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`:

- **Status line:** `Approved. Guard and testing sections reviewed with PR 1. PRs 1 to 4 settled the details recorded below.`
- **Migration table, PR 4 row:** Main shadcn pieces becomes `Table; Tabs for the stack's tab row (\`RouteTabs\`); reuses Card, Input, Label, Button, Alert and Empty`.
- **Migration table, PR 6 row:** Main shadcn pieces becomes `\`RouteTabs\` from PR 4 for the template's tabs, so each tab keeps its URL; Select; Textarea`.
- **After the "What PR 3 settled" list,** add:

  ```markdown
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
  ```

- **Guards, table guard:** replace the first bullet with `No raw \`<table\` outside \`src/components/ui/\`, except in the files \`LEGACY_TABLES\` lists, which keep the legacy \`.data-table\` rules until PR 8 moves them.` Replace the second with `Every \`<Table>\` opens with a \`<colgroup>\`.` Replace the third with `\`src/components/ui/table.tsx\` is edited to always apply \`table-fixed\`, after the caller's \`className\`, so \`cn\` can't let a \`table-auto\` replace it. The reason is unchanged: with automatic layout, a status changing from \`queued\` to \`waiting_approval\` shifts every column after it.`
- **Testing, new behaviour tests:** add as the first bullet: `- PR 4, RouteTabs: Tab reaches only the active tab; ArrowLeft and ArrowRight move focus without navigating; Enter follows the focused tab; on every stack route the selected tab is the one its link marks \`aria-current\`.`

- [ ] **Step 8: Full verification**

Run from `web/`:

```bash
npx tsc -b
npm test
npm run build
```

Expected: `tsc` reports no errors; every test passes (about 726 in 56 files, up from 681 in 55); the build succeeds. Note the test count and the CSS and JS sizes the build prints, for the PR description. No native dependency changed, so `docker compose build web` isn't needed.

- [ ] **Step 9: Commit**

From the repository root:

```bash
git add docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md
git commit -m "docs: record what PR 4 of the shadcn migration settled" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 10: Push and open the PR against `feat/shadcn-standalone-screens`**

Write `$AUDIT/pr-body.md` first. It must cover:
- that this PR is stacked on #277 and implements PR 4 of the spec, with links to the spec and this plan;
- what moved:
  - the stacks list onto Table and Empty;
  - create stack onto Card, Label, Input, Button and Alert;
  - the stack shell's tabs onto `RouteTabs` (shadcn Tabs as links);
  - the credentials panel and the Environment tab onto Card, Table, Input, Button and Alert;
- the deviations from the spec's table, each with its reason:
  - Tabs arrived two PRs early;
  - the template's Credentials tab changed with the shared panel;
  - the table guard allows `LEGACY_TABLES` until PR 8;
- the one vendored edit (`table-fixed` after `className` in `table.tsx`) and why;
- what was deleted: the stacks list and credentials CSS, including the rules the dead-CSS guard couldn't see;
- the behaviour and look changes:
  - the stacks and credentials lists are tables;
  - the stack tabs are a tab list with arrow keys and one Tab stop;
  - errors are announced Alerts;
  - the stacks empty state is an Empty;
  - copy, test IDs and flows are unchanged;
- the mixed look until PR 6: the stack's tabs are shadcn's, the template's still legacy;
- the new `drive-web.mjs` flags;
- configuration impact: none. No new dependencies, no migrations;
- validation:
  - the commands above, with the test count;
  - the audit's `CLEAN` result;
  - the 32px field heights;
  - the phone-width probes with `small: []`;
- a `## Screenshots` heading listing the before/after pairs in `$AUDIT/shots/`, desktop and 375px. `gh` can't upload images, so tell the user to drag them into the PR description;
- a final line: `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

Then, from the repository root:

```bash
export AUDIT=<this session's scratchpad>/audit4
gh auth status
git push -u origin feat/shadcn-stacks
gh pr create --base feat/shadcn-standalone-screens --head feat/shadcn-stacks \
  --title "feat(web): stacks screens on shadcn" --body-file "$AUDIT/pr-body.md"
```

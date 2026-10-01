# Stack access on shadcn (PR 5) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the stack access screen onto shadcn components and Tailwind utilities, and delete the legacy CSS it stops using. The screen's parts are the grants list, the user search, the role picker and the undo banner. The user search gains a real combobox's keyboard and ARIA behaviour.

**Architecture:**
- **Vendoring.** The shadcn CLI vendors Combobox and Select. Combobox brings InputGroup and Textarea with it.
- **User search.** It becomes a Combobox over the server's answer:
  - `filter={null}` shows the results as the server sent them;
  - the list opens only once a search of two or more characters has answered;
  - the pick is the Combobox's value, so the input shows the picked name and the selected-user card goes.
- **Role picker.** It becomes a Select.
- **The rest of the screen:**
  - the two panels become Cards;
  - the grants list becomes a bordered `ul` with `RoleBadge`;
  - errors become Alerts;
  - the undo banner becomes a fixed `role="status"` toast built from Tailwind utilities.
- **Audit tooling.** A rebase dropped two features from `scripts/drive-web.mjs`: its phone emulation and its `--signed-out` check. Task 4 restores both, so the audit can measure touch targets and reach `/styleguide`.

**Tech Stack:** React 19, React Router 6, TanStack Query 5, Tailwind CSS 4.3, shadcn CLI 4.21 (`base-nova` style on Base UI 1.8), `cn`, lucide-react, Vitest 4 with Testing Library and `@testing-library/user-event`.

**Spec:** `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`. Read these sections before starting:
- "Theme";
- "Running old and new styles side by side": the layer order, the Preflight audit, and the known `base.css` leak;
- "What PR 2 settled", "What PR 3 settled" and "What PR 4 settled";
- "What every screen PR must do";
- "Guards";
- "Testing".

## Decisions the spec left open

The spec gives this screen one exception to "keep its behaviour": the user search gains keyboard and ARIA behaviour. Three choices follow from it, and this plan makes them as defaults. A reviewer can overrule any of them before Task 2 starts.

1. **The selected-user card goes.**
   - With a Combobox, the pick is the component's value, and the input shows the picked name. A card below it would show the name twice.
   - A clear button inside the input replaces the card's X. It keeps the card's accessible name, `Clear selected user`.
   - The vendored `showClear` button can't be used: it has no accessible name, and `combobox.tsx` doesn't export it.
2. **Typing searches but keeps the pick.**
   - The old input dropped the pick on every keystroke.
   - Base UI's single-select Combobox empties its input when its value goes to `null`. So dropping the pick while typing wipes what was just typed; the prototype showed it.
   - The pick now holds until another pick, the clear button, or Escape. Leaving the field without picking puts the picked name back. This is Base UI's own async-search pattern.
3. **The undo banner is a Tailwind toast, not Sonner.**
   - It adds no dependency, and it is `role="status"`.
   - It takes the popover look (white, a border and a shadow) instead of the old dark pill.
   - It is fixed and centred, and on a phone it stays inside the screen's edges.

Users who already hold a role stay in the results as disabled options that show their role, as the old dropdown showed them greyed out.

## Global Constraints

- **Branch.** `feat/shadcn-stack-access`, from `main`; PRs 0 to 4 are merged. The PR base is `main`.
- **Working directory.** All commands run from `web/` unless a step says otherwise.
- **Copy and behaviour stay word for word,** except the user search, as the Decisions above describe. The copy:
  - headings and states: `Current Grants`, `Loading grants...`, `Failed to load grants.`, `Retry`, `No users have been assigned access yet. Use the panel on the right to add the first grant.`;
  - revoking: `Remove access?`, `Confirm`, `Cancel`;
  - the search: `Search users by name or email...`, `No users found`;
  - the role picker: `Role`, `Owner`, `Operator`, `Approver`, `Viewer`;
  - assigning: `Assign Role` (both the heading and the button), `Assigning...`, `Replace Role`, `Replacing...`;
  - the banner: `Removed <name>'s <role> access.`, `Undo`;
  - errors: `Failed to assign role`, `Failed to revoke role`, `Failed to restore role`.

  A search result's second line stays `<email, or sub if there is none>`, followed by ` — <role>` for a user who holds one.
- **Accessible names don't change.** Other tests depend on them:
  - `Search users`, the search input;
  - `Role`, the role picker, through `<Label htmlFor="role-select">`;
  - `Revoke <name>'s <role> role`;
  - `Clear selected user`;
  - `Assign Role`.

  `StackDetailShell.test.tsx` also checks that the access route renders `Current Grants`.
- **Components come only from the CLI.** Run `yes n | npx shadcn add combobox select -y`. The `yes n` answers no when the CLI offers to overwrite `button.tsx`, which carries PR 1's `/90` hover edit. PR 5 edits no vendored file.
- **Classes.** App code uses theme colours and Tailwind's scale steps only: no arbitrary values and no built-in palette colours. `src/styles/tailwind.guard.test.ts` fails otherwise. Arbitrary *variants*, such as `data-[size=default]:` and `*:`, are allowed.
- **Headings.** `base.css` styles bare elements from the `legacy` layer until PR 9. Every migrated `h2` therefore sets its own family, size, weight and tracking: `font-heading text-base leading-snug font-medium tracking-normal`.
- **Text colour.** `body` keeps the legacy text colour, so the screen's outermost `section` sets `text-foreground` (Task 3). Cards already set `text-card-foreground` on themselves.
- **44px touch targets on coarse pointers,** where `--legacy-touch-target` gave them before:
  - `pointer-coarse:h-11` on every Button, and `pointer-coarse:size-11` on icon buttons, the clear button included;
  - on the search's InputGroup, `pointer-coarse:h-11 pointer-coarse:*:data-[slot=input-group-control]:h-full`, so the input fills the taller group;
  - on the Select trigger, `pointer-coarse:data-[size=default]:h-11`. The vendored height is `data-[size=default]:h-8`, a class plus an attribute selector, which a plain `pointer-coarse:h-11` can't outrank. In the compiled CSS, the coarse rule comes later with equal specificity;
  - `pointer-coarse:min-h-11` on every ComboboxItem and SelectItem.

  The prototype measured each of these at 44px in Chrome with touch emulation. The input itself measures 42px inside the group's 1px border, so the audit measures the group.
- **Dead CSS.** Each task deletes the legacy rules it stopped using. The dead-CSS guard (`src/styles/legacy.guard.test.ts`) misses four of them, which Tasks 2 and 3 delete by hand and check with `rg`:
  - `.search-dropdown` and `.undo-banner`, while a comment in `AppShell.tsx` names them;
  - `.assigned`, a word that appears in other screens' copy;
  - `.alert .secondary-button`, whose two classes both live on elsewhere.
- **Tests.**
  - Find elements by role, text, label, test ID or `data-slot`, never by legacy class name. Drive Base UI components with `@testing-library/user-event`.
  - While a Combobox popup is open, Base UI hides the rest of the page from assistive technology, and Testing Library computes an empty name for everything there. Query outside the popup only once it has closed.
  - `ComboboxEmpty` is also `role="status"`, so tests find the undo banner by its text.
- **No `src/test/setup.ts`.** Combobox and Select run in jsdom without stand-ins; the prototype confirmed it.
- **Commits.** Subjects are lowercase-prefixed (`feat(web):`, `refactor(web):`, `test(web):`, `fix:`, `docs:`). Every commit message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Someone picks a user, then searches again before pressing Assign.** Expected:
   - the picked user stays picked;
   - nothing is sent to search for the picked name;
   - leaving the field without a new pick puts the name back.

   Task 2 tests it.
2. **A search result already holds a role on this stack.** Expected: the result shows that role, and neither a click nor Enter picks it. Task 2 tests both.
3. **One typed character, or a search that finds nobody.** Expected:
   - one character opens nothing and sends nothing;
   - "No users found" appears only after a real search.

   Task 2 tests both.
4. **A grant with a very long name or email, on a 375px phone.** Expected:
   - the text ends in an ellipsis and shows whole on hover;
   - the confirm row wraps under the name;
   - the page doesn't scroll sideways.

   Task 3 tests the truncation and the `title`. Task 5 measures the page at 375px.
5. **Someone presses Assign twice on a slow network.** Expected: one grant request. While it is out, the button is disabled and reads `Assigning...`. Task 2 tests it.

---

### Setup: branch and plan commit

- [ ] **Step 1: Create the branch and commit this plan**

From the repository root:

```bash
git switch main
git pull --ff-only
git switch -c feat/shadcn-stack-access
git add docs/superpowers/plans/2026-10-01-shadcn-stack-access.md
git commit -m "docs: implementation plan for the stack access screen (PR 5)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 1: Vendor Combobox and Select, with their specimens

Combobox renders its input inside an InputGroup, and InputGroup imports Textarea, so the CLI adds four files. The spec's rule 4 puts every newly installed component on `/styleguide`, so this task adds four specimens. Textarea arrives a PR early; PR 6 uses it.

**Files:**
- Create (by the CLI): `web/src/components/ui/combobox.tsx`, `web/src/components/ui/select.tsx`, `web/src/components/ui/input-group.tsx`, `web/src/components/ui/textarea.tsx`
- Modify: `web/src/dev/StyleGuide.tsx`
- Test: `web/src/dev/StyleGuide.test.tsx`

**Interfaces:**
- Consumes: the theme colours in `src/styles/theme.css`. The vendored files import `cn` from the `cn` package directly, as the earlier ones do.
- Produces (CLI output, unedited). From `@/components/ui/combobox`: `Combobox`, `ComboboxInput`, `ComboboxContent`, `ComboboxList`, `ComboboxItem` and `ComboboxEmpty`.
  - **`Combobox`** is Base UI's `Combobox.Root`. Its props:
    - `items`;
    - `filter`, where `null` turns filtering off;
    - `value` and `onValueChange`;
    - `open` and `onOpenChange`;
    - `onInputValueChange(value, { reason })`. The reason is `"input-change"` for typing and `"item-press"` when a pick fills the input; anything else that empties the input arrives with `value` `""`;
    - `itemToStringLabel` and `isItemEqualToValue`.
  - **`ComboboxInput`** takes Base UI's Input props plus `showTrigger` (default `true`) and `showClear` (default `false`).
    - Its `className` goes on the InputGroup around the input, which has `h-8`, `role="group"` and `data-slot="input-group"`. Its `children` render inside that group.
    - The input itself is `role="combobox"`, with `aria-expanded`, `aria-activedescendant` and `data-slot="input-group-control"`.
    - The vendored clear button behind `showClear` has no accessible name, and the file doesn't export it.
  - **`ComboboxContent`** portals the popup to the body. Its Positioner is `isolate z-50`.
  - **`ComboboxList`** is `role="listbox"`. It takes a render function, `(item) => <ComboboxItem …>`.
  - **`ComboboxItem`** takes `value` and `disabled`. It is `role="option"`, has `data-highlighted` while highlighted, and has `aria-disabled="true"` when disabled.
  - **`ComboboxEmpty`** shows while the popup is open and the list is empty. It is `role="status"`.
- Also produced, from `@/components/ui/select`: `Select`, `SelectTrigger`, `SelectValue`, `SelectContent` and `SelectItem`, on Base UI Select.
  - `Select` takes `items` as `{ value, label }[]`, so that `SelectValue` shows the label, and `value` with `onValueChange`.
  - `SelectTrigger` is a button with `role="combobox"`. It keeps the `id` it is given, carries `data-size="default"`, and gets its height from `data-[size=default]:h-8`.
  - The popup's Positioner is `isolate z-50`.
  - Enter opens it, and the selected item takes focus; the arrows move focus. Enter commits and returns focus to the trigger.
- Also produced, from `@/components/ui/input-group`: `InputGroup`, `InputGroupAddon`, `InputGroupButton` and `InputGroupInput`. `InputGroupButton` takes `size: "xs" | "sm" | "icon-xs" | "icon-sm"`; `icon-xs` is `size-6`.
- Also produced, from `@/components/ui/textarea`: `Textarea`, with `data-slot="textarea"`.

- [ ] **Step 1: Vendor the components**

Run: `yes n | npx shadcn add combobox select -y`
Expected: the CLI asks whether to overwrite `button.tsx` and answers no. It ends with `Created 4 files` (`select.tsx`, `textarea.tsx`, `input-group.tsx` and `combobox.tsx`) and `Skipped 2 files` (`button.tsx` and `input.tsx`).

Without `yes n |`, the CLI stops at that prompt after writing only `select.tsx`. Delete that file and run the command again.

Then run: `git status --short`
Expected: exactly the four new files. `package.json`, the lockfile, `theme.css`, `components.json` and `button.tsx` are unchanged. If `button.tsx` changed anyway, run `git checkout src/components/ui/button.tsx`.

- [ ] **Step 2: Write the failing styleguide test**

In `web/src/dev/StyleGuide.test.tsx`, add inside `describe("StyleGuide", …)`, before `it("renders the real RouteMessage", …)`:

```tsx
  it("shows the components PR 5 added", () => {
    render(<StyleGuide />);
    const section = within(screen.getByTestId("sg-theme"));
    const combobox = section.getByRole("combobox", { name: "Combobox specimen" });
    expect(combobox.getAttribute("aria-expanded")).toBe("false");
    expect(combobox.closest('[data-slot="input-group"]')).not.toBeNull();
    const select = section.getByRole("combobox", { name: "Default role" });
    expect(select.getAttribute("data-slot")).toBe("select-trigger");
    expect(select.querySelector('[data-slot="select-value"]')?.textContent).toBe("Viewer");
    const field = section.getByRole("textbox", { name: "Input group specimen" });
    expect(field.closest('[data-slot="input-group"]')).not.toBeNull();
    expect(section.getByLabelText("Description").getAttribute("data-slot")).toBe("textarea");
  });
```

- [ ] **Step 3: Run it to see it fail**

Run: `npx vitest run src/dev/StyleGuide.test.tsx`
Expected: FAIL on `shows the components PR 5 added`, with `Unable to find an accessible element with the role "combobox" and name "Combobox specimen"`.

- [ ] **Step 4: Add the specimens**

In `web/src/dev/StyleGuide.tsx`, make these changes:

1. Add `Search` to the lucide import:

   ```tsx
   import { CircleAlert, Info, Plus, Search, SearchX } from "lucide-react";
   ```

2. Add the new imports beside the other `@/components/ui` imports, in alphabetical order:

   ```tsx
   import { Combobox, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from "@/components/ui/combobox";
   import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
   import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
   import { Textarea } from "@/components/ui/textarea";
   ```

3. Add two constants after the `titleCase` line:

   ```tsx
   const ROLE_ITEMS = ROLES.map((role) => ({ value: role, label: titleCase(role) }));
   const STACK_NAMES = ["payments-core", "payments-edge", "billing", "identity", "search"];
   ```

4. Inside the `theme` Section, after the Tabs specimen and before `</Section>`, add:

```tsx
            <Specimen label="Combobox" hint="filters these five as you type; the stack-access search asks the server instead" stack>
              <Combobox items={STACK_NAMES}>
                <ComboboxInput aria-label="Combobox specimen" placeholder="Find a stack" showTrigger={false} className="w-full max-w-sm" />
                <ComboboxContent>
                  <ComboboxEmpty>No stacks found</ComboboxEmpty>
                  <ComboboxList>
                    {(name: string) => (
                      <ComboboxItem key={name} value={name}>
                        {name}
                      </ComboboxItem>
                    )}
                  </ComboboxList>
                </ComboboxContent>
              </Combobox>
            </Specimen>
            <Specimen label="Select" stack>
              <div className="grid w-full max-w-sm gap-2">
                <Label htmlFor="sg-select">Default role</Label>
                <Select items={ROLE_ITEMS} defaultValue="viewer">
                  <SelectTrigger id="sg-select" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {ROLE_ITEMS.map((role) => (
                      <SelectItem key={role.value} value={role.value}>
                        {role.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </Specimen>
            <Specimen label="Input group" hint="Combobox's input is one" stack>
              <InputGroup className="max-w-sm">
                <InputGroupAddon>
                  <Search />
                </InputGroupAddon>
                <InputGroupInput aria-label="Input group specimen" placeholder="Search stacks" />
              </InputGroup>
            </Specimen>
            <Specimen label="Textarea" stack>
              <div className="grid w-full max-w-sm gap-2">
                <Label htmlFor="sg-textarea">Description</Label>
                <Textarea id="sg-textarea" placeholder="What this stack is for" />
              </div>
            </Specimen>
```

The specimens show the stock components, without the app's `pointer-coarse:` classes.

- [ ] **Step 5: Run the styleguide test, the guards and the type-check**

Run: `npx vitest run src/dev src/styles && npx tsc -b`
Expected: everything passes and `tsc` reports no errors. The class guard skips `src/components/ui/`, so the vendored arbitrary values (`max-h-(--available-height)`, `rounded-[min(var(--radius-md),10px)]`) don't trip it.

- [ ] **Step 6: Commit**

```bash
git add src/components/ui/combobox.tsx src/components/ui/select.tsx src/components/ui/input-group.tsx src/components/ui/textarea.tsx src/dev/StyleGuide.tsx src/dev/StyleGuide.test.tsx
git commit -m "feat(web): add the shadcn Combobox and Select" -m "Combobox brings InputGroup and Textarea with it. The CLI's offer to overwrite button.tsx, which carries PR 1's /90 hover, was declined. All four get a /styleguide specimen." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Assign Role on Card, Combobox and Select

This task migrates only the Assign Role panel. The page grid, the grants list and the undo banner stay legacy until Task 3, so for one commit a Card sits in the legacy `.workflow-grid`.

How the search works:
- **What it sends.** The search state holds what the user typed, and only typing changes it: `reason === "input-change"`. Anything that empties the input, such as Escape, the clear button or a finished assignment, empties the search too. A pick fills the input with the user's name and leaves the search alone, so the screen never searches for a name it already has.
- **When the list opens.** `results` is the server's answer for a search of two or more characters, and `undefined` otherwise. The list opens only while it has one: the controlled `open` is `searchOpen && results !== undefined`. One typed character therefore opens nothing, and "No users found" never shows before a search has run.
- **No second filter.** `filter={null}` keeps Base UI from filtering the answer again. The server matches on email as well as name, so a client-side filter on the name would drop real results.

**Files:**
- Modify: `web/src/features/stacks/StackAccessScreen.tsx` (replaced whole)
- Modify: `web/src/styles/features.css` (deletions)
- Modify: `web/src/app/AppShell.tsx` (the comment above `<header>`)
- Test: `web/src/features/stacks/StackAccessScreen.test.tsx`
- Test: `web/src/app/AppShell.test.tsx` (the layering test)

**Interfaces:**
- Consumes, from Task 1: `Combobox`, `ComboboxInput`, `ComboboxContent`, `ComboboxEmpty`, `ComboboxList`, `ComboboxItem`, `InputGroupAddon`, `InputGroupButton`, `Select`, `SelectTrigger`, `SelectValue`, `SelectContent` and `SelectItem`. From earlier PRs: `Card`, `CardHeader`, `CardContent`, `Alert`, `AlertTitle`, `Button` and `Label`.
- Produces, for Task 3:
  - in `StackAccessScreen.tsx`: the `ROLES` constant (`{ value, label }[]`), the `Role` type, `headingClass`, `grantsBySub` (a `Map<string, GrantView>`), and the Assign Role `Card`;
  - in `StackAccessScreen.test.tsx`: the helpers `serve({ grants?, users?, assign? }) => Sent[]`, `searches(sent) => (string | null)[]`, `afterDebounce()`, `renderWithGrants(grants?)`, `typeInSearch(text) => { user, input }` and `assignButton()`, and the fixtures `charlie`, `chad` and `dorothy`.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/stacks/StackAccessScreen.test.tsx`, make these changes:

1. Change the Testing Library import to:

   ```tsx
   import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
   ```

2. Change the types import to:

   ```tsx
   import type { GrantView, ListGrantsResponse, SearchUsersResponse, UserProfile } from "../../api/types";
   ```

3. Insert after the `searchResults` constant, before `describe("StackAccessScreen", …)`:

```tsx
const charlie = searchResults.users[0];
const chad: UserProfile = { sub: "u4", displayName: "Chad", email: "chad@example.com" };
// Matched by email: there is no "ch" in her name.
const dorothy: UserProfile = { sub: "u5", displayName: "Dorothy Vaughan", email: "dvaughan@chem.example.com" };

interface Sent {
  method: string;
  url: string;
  body?: Record<string, string>;
}

// Answers each request by its URL, so a test can count and inspect what the
// screen sent. A search gets `users`, a POST gets `assign` (or the grant it
// asked for), and anything else gets the grants list.
function serve({
  grants = [],
  users = [],
  assign
}: { grants?: GrantView[]; users?: UserProfile[]; assign?: () => Promise<Response> } = {}) {
  const sent: Sent[] = [];
  const json = (value: unknown) =>
    new Response(JSON.stringify(value), { status: 200, headers: { "content-type": "application/json" } });
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const url = String(input);
    const method = init?.method ?? "GET";
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, string>) : undefined;
    sent.push({ method, url, body });
    if (url.includes("/users/search")) return json({ users, first: 0, max: 20 });
    if (method === "POST") {
      return assign ? assign() : json({ userSub: body?.user_sub, role: body?.role, displayName: "", email: "" });
    }
    return json({ grants });
  });
  return sent;
}

/** The query of each user search the screen sent, in order. */
const searches = (sent: Sent[]) =>
  sent
    .filter(({ url }) => url.includes("/users/search"))
    .map(({ url }) => new URL(url, "http://localhost").searchParams.get("q"));

/** The search waits 300ms after the last keystroke. */
const afterDebounce = () => new Promise((resolve) => setTimeout(resolve, 400));

async function renderWithGrants(grants: GrantView[] = []) {
  render(<StackAccessScreen />, { wrapper: wrapper() });
  await screen.findByText(grants.length === 0 ? /No users have been assigned/ : grants[0].displayName);
}

async function typeInSearch(text: string) {
  const user = userEvent.setup();
  const input = screen.getByRole("combobox", { name: "Search users" }) as HTMLInputElement;
  await user.click(input);
  await user.type(input, text);
  return { user, input };
}

// Only while no popup is open: Base UI hides the rest of the page from
// assistive technology while one is, and a role query can't name it then.
const assignButton = () => screen.getByRole("button", { name: "Assign Role" }) as HTMLButtonElement;
```

4. In `it("shows error banner on failed revoke", …)`, after its final `await waitFor(…)`, add:

   ```tsx
       expect(screen.getByText("cannot remove the last owner").closest('[data-slot="alert"]')).not.toBeNull();
   ```

5. Add before the closing `});` of `describe("StackAccessScreen", …)`:

```tsx
  describe("user search", () => {
    it("is a combobox that stays shut until a search of two characters answers", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("c");
      await afterDebounce();
      expect(input.getAttribute("aria-expanded")).toBe("false");
      expect(screen.queryByText("No users found")).toBeNull();
      expect(searches(sent)).toEqual([]);

      await user.type(input, "h");
      await screen.findByRole("option", { name: /Charlie Brown/ });
      expect(input.getAttribute("aria-expanded")).toBe("true");
      expect(searches(sent)).toEqual(["ch"]);
    });

    // The server matches on name and email; the list must not filter again.
    it("lists every user the server answers with, even one matched by email", async () => {
      serve({ users: [charlie, dorothy] });
      await renderWithGrants();

      await typeInSearch("ch");
      expect(await screen.findByRole("option", { name: /Dorothy Vaughan/ })).toBeDefined();
      expect(screen.getAllByRole("option")).toHaveLength(2);
    });

    it("moves the highlight with ArrowDown and ArrowUp, and picks with Enter", async () => {
      serve({ users: [charlie, chad] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("ch");
      const [first, second] = await screen.findAllByRole("option");
      await user.keyboard("{ArrowDown}");
      expect(input.getAttribute("aria-activedescendant")).toBe(first.id);
      await user.keyboard("{ArrowDown}");
      expect(input.getAttribute("aria-activedescendant")).toBe(second.id);
      expect(second.hasAttribute("data-highlighted")).toBe(true);
      await user.keyboard("{ArrowUp}");
      expect(input.getAttribute("aria-activedescendant")).toBe(first.id);

      await user.keyboard("{Enter}");
      expect(input.value).toBe("Charlie Brown");
      expect(input.getAttribute("aria-expanded")).toBe("false");
      expect(assignButton().disabled).toBe(false);
    });

    it("closes and clears on Escape", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await screen.findByRole("option", { name: /Charlie Brown/ });
      await user.keyboard("{Escape}");
      expect(input.getAttribute("aria-expanded")).toBe("false");
      expect(screen.queryByRole("option")).toBeNull();
      expect(input.value).toBe("");
    });

    it("keeps the pick, and searches only what was typed, while the user looks again", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      expect(input.value).toBe("Charlie Brown");
      await afterDebounce();
      expect(searches(sent)).toEqual(["cha"]);

      // Typing searches again; leaving without a new pick puts the pick back.
      await user.type(input, "x");
      await user.tab();
      expect(input.value).toBe("Charlie Brown");
      expect(assignButton().disabled).toBe(false);
    });

    it("clears the pick with the clear button", async () => {
      serve({ users: [charlie] });
      await renderWithGrants();

      const { user, input } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));
      await user.click(screen.getByRole("button", { name: "Clear selected user" }));
      expect(input.value).toBe("");
      expect(screen.queryByRole("button", { name: "Clear selected user" })).toBeNull();
      expect(assignButton().disabled).toBe(true);
    });

    it("shows a user who already holds a role with it, and won't pick them", async () => {
      const alice = twoGrants.grants[0];
      serve({ grants: [alice], users: [{ sub: alice.userSub, displayName: alice.displayName, email: alice.email }] });
      await renderWithGrants([alice]);

      const { user, input } = await typeInSearch("ali");
      const option = await screen.findByRole("option", { name: /Alice/ });
      expect(option.getAttribute("aria-disabled")).toBe("true");
      expect(option.textContent).toContain("alice@example.com — owner");

      await user.keyboard("{ArrowDown}{Enter}");
      expect(input.value).toBe("ali");
      // jsdom has no CSS, so the click reaches the option that pointer-events-none
      // shields in a browser. Either way nothing is picked.
      await user.click(option);
      await user.keyboard("{Escape}");
      expect(input.value).toBe("");
      expect(assignButton().disabled).toBe(true);
    });

    it("says No users found when the search finds nobody", async () => {
      serve({ users: [] });
      await renderWithGrants();

      await typeInSearch("zz");
      expect(await screen.findByText("No users found")).toBeDefined();
    });
  });

  describe("role picker", () => {
    it("opens from the keyboard, moves with the arrows, and commits with Enter", async () => {
      const sent = serve({ users: [charlie] });
      await renderWithGrants();
      const { user } = await typeInSearch("cha");
      await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));

      const role = screen.getByRole("combobox", { name: "Role" });
      const shown = () => role.querySelector('[data-slot="select-value"]')?.textContent;
      expect(shown()).toBe("Viewer");

      role.focus();
      await user.keyboard("{Enter}");
      const listbox = await screen.findByRole("listbox");
      expect(within(listbox).getAllByRole("option").map((option) => option.textContent)).toEqual([
        "Owner",
        "Operator",
        "Approver",
        "Viewer"
      ]);
      expect(document.activeElement?.textContent).toBe("Viewer");
      await user.keyboard("{ArrowUp}{ArrowUp}");
      expect(document.activeElement?.textContent).toBe("Operator");
      await user.keyboard("{ArrowDown}");
      expect(document.activeElement?.textContent).toBe("Approver");
      await user.keyboard("{Enter}");
      await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
      expect(shown()).toBe("Approver");
      expect(document.activeElement).toBe(role);

      await user.click(assignButton());
      await waitFor(() =>
        expect(sent.find(({ method }) => method === "POST")?.body).toEqual({ user_sub: "u3", role: "approver" })
      );
    });
  });

  it("sends one assignment however often Assign is pressed while it is out", async () => {
    const sent = serve({ users: [charlie], assign: () => new Promise<Response>(() => {}) });
    await renderWithGrants();
    const { user } = await typeInSearch("cha");
    await user.click(await screen.findByRole("option", { name: /Charlie Brown/ }));

    const assign = assignButton();
    await user.click(assign);
    expect(assign.textContent).toBe("Assigning...");
    expect(assign.disabled).toBe(true);
    await user.click(assign);
    expect(sent.filter(({ method }) => method === "POST")).toHaveLength(1);
  });
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/stacks/StackAccessScreen.test.tsx`
Expected: 11 failed, 7 passed.
- The ten new tests fail with `Unable to find an accessible element with the role "combobox" and name "Search users"`: the legacy search input is a plain textbox.
- `shows error banner on failed revoke` fails with `expected null not to be null`: the legacy error is a `div.alert`, which has no `data-slot`.
- The other seven, which existed before this task, pass.

- [ ] **Step 3: Replace the screen**

Replace the whole of `web/src/features/stacks/StackAccessScreen.tsx` with:

```tsx
import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { CircleAlert, Loader2, Search, Shield, Trash2, X } from "lucide-react";
import {
  useStackGrantsQuery,
  useSearchUsersQuery,
  useAssignStackRoleMutation,
  useRevokeStackRoleMutation
} from "../../api/queries";
import type { GrantView, UserProfile } from "../../api/types";
import { tenantID } from "../../config";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Combobox, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from "@/components/ui/combobox";
import { InputGroupAddon, InputGroupButton } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const ROLES = [
  { value: "owner", label: "Owner" },
  { value: "operator", label: "Operator" },
  { value: "approver", label: "Approver" },
  { value: "viewer", label: "Viewer" }
] as const;
type Role = (typeof ROLES)[number]["value"];

interface UndoEntry {
  userSub: string;
  role: string;
  displayName: string;
}

// base.css gives every h2 the legacy 32px display type until PR 9, so each
// heading sets its own family, size, weight and tracking.
const headingClass = "flex items-center gap-2 font-heading text-base leading-snug font-medium tracking-normal";

export default function StackAccessScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const grants = useStackGrantsQuery(tenantID, stackId);

  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserProfile | null>(null);
  const [selectedRole, setSelectedRole] = useState<Role>("viewer");
  const [undoEntry, setUndoEntry] = useState<UndoEntry | null>(null);
  const [mutationError, setMutationError] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);
  const debouncedSearch = useDebounce(search, 300);
  const searchResults = useSearchUsersQuery(tenantID, debouncedSearch);
  const assignMutation = useAssignStackRoleMutation(tenantID, stackId);
  const revokeMutation = useRevokeStackRoleMutation(tenantID, stackId);
  const undoTimeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  const grantsBySub = new Map((grants.data?.grants ?? []).map((g) => [g.userSub, g]));
  const replacing = selectedUser !== null && grantsBySub.has(selectedUser.sub);
  // The list opens once a search has answered, as the old dropdown did: before
  // that there is nothing to show, and "No users found" would be untrue.
  const results = debouncedSearch.length >= 2 ? searchResults.data?.users : undefined;

  const handleAssign = useCallback(async () => {
    if (!selectedUser) return;
    setMutationError("");
    try {
      await assignMutation.mutateAsync({
        user_sub: selectedUser.sub,
        role: selectedRole
      });
      setSelectedUser(null);
      setSearch("");
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : "Failed to assign role");
    }
  }, [selectedUser, selectedRole, assignMutation]);

  const handleRevoke = useCallback(
    async (grant: GrantView) => {
      setMutationError("");
      setConfirmRevoke(null);
      try {
        await revokeMutation.mutateAsync(grant.userSub);
        setUndoEntry({
          userSub: grant.userSub,
          role: grant.role,
          displayName: grant.displayName
        });
      } catch (err) {
        setMutationError(err instanceof Error ? err.message : "Failed to revoke role");
      }
    },
    [revokeMutation]
  );

  const handleUndo = useCallback(async () => {
    if (!undoEntry) return;
    setMutationError("");
    try {
      await assignMutation.mutateAsync({
        user_sub: undoEntry.userSub,
        role: undoEntry.role
      });
    } catch {
      setMutationError("Failed to restore role");
    }
    setUndoEntry(null);
  }, [undoEntry, assignMutation]);

  useEffect(() => {
    if (!undoEntry) return;
    undoTimeoutRef.current = setTimeout(() => setUndoEntry(null), 5000);
    return () => clearTimeout(undoTimeoutRef.current);
  }, [undoEntry]);

  useEffect(() => {
    if (revokeMutation.isSuccess || assignMutation.isSuccess) {
      setMutationError("");
    }
  }, [revokeMutation.isSuccess, assignMutation.isSuccess]);

  return (
    <section className="workflow-grid">
      <section className="panel">
        <h2>
          <Shield size={16} />
          Current Grants
        </h2>
        {grants.isLoading && (
          <p className="muted">
            <Loader2 size={14} className="spin" /> Loading grants...
          </p>
        )}
        {grants.isError && (
          <div className="alert">
            Failed to load grants.
            <button
              className="secondary-button"
              onClick={() => grants.refetch()}
            >
              Retry
            </button>
          </div>
        )}
        {grants.data && grants.data.grants.length === 0 && (
          <p className="muted">
            No users have been assigned access yet. Use the panel on the right
            to add the first grant.
          </p>
        )}
        {grants.data && grants.data.grants.length > 0 && (
          <ul className="grants-list">
            {grants.data.grants.map((grant) => (
              <li key={grant.userSub} className="grant-row">
                <div className="grant-user">
                  <span>{grant.displayName}</span>
                  {grant.email && <small>{grant.email}</small>}
                </div>
                <span className={`role-badge role-badge--${grant.role}`}>{grant.role}</span>
                <div className="grant-actions">
                  {confirmRevoke === grant.userSub ? (
                    <>
                      <span className="confirm-label">
                        Remove access?
                      </span>
                      <button
                        className="danger"
                        onClick={() => handleRevoke(grant)}
                      >
                        Confirm
                      </button>
                      <button onClick={() => setConfirmRevoke(null)}>
                        Cancel
                      </button>
                    </>
                  ) : (
                    <button
                      className="danger"
                      onClick={() => setConfirmRevoke(grant.userSub)}
                      aria-label={`Revoke ${grant.displayName}'s ${grant.role} role`}
                    >
                      <Trash2 size={14} />
                    </button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </section>

      <Card>
        <CardHeader>
          <h2 className={headingClass}>
            <Search aria-hidden="true" className="size-4" />
            Assign Role
          </h2>
        </CardHeader>
        <CardContent className="grid gap-4">
          {mutationError && (
            <Alert variant="destructive">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{mutationError}</AlertTitle>
            </Alert>
          )}
          {/* The server searches, by name and email, so the list shows its
              answer as it is (filter={null}). The pick is the Combobox's
              value: the input shows the picked name, and keeps it while the
              user searches again, until another pick, the clear button or
              Escape. */}
          <Combobox<UserProfile>
            items={results ?? []}
            filter={null}
            open={searchOpen && results !== undefined}
            onOpenChange={setSearchOpen}
            value={selectedUser}
            onValueChange={(user) => setSelectedUser(user)}
            onInputValueChange={(value, { reason }) => {
              // Typing searches. Picking a user writes their name into the
              // input, which is not a query, so only typing sets one.
              if (reason === "input-change") {
                setSearch(value);
              } else if (value === "") {
                setSearch("");
              }
            }}
            itemToStringLabel={(user) => user.displayName}
            isItemEqualToValue={(a, b) => a.sub === b.sub}
          >
            <ComboboxInput
              aria-label="Search users"
              placeholder="Search users by name or email..."
              showTrigger={false}
              className="w-full pointer-coarse:h-11 pointer-coarse:*:data-[slot=input-group-control]:h-full"
            >
              {selectedUser && (
                <InputGroupAddon align="inline-end">
                  <InputGroupButton
                    size="icon-xs"
                    aria-label="Clear selected user"
                    className="pointer-coarse:size-11"
                    onClick={() => setSelectedUser(null)}
                  >
                    <X aria-hidden="true" />
                  </InputGroupButton>
                </InputGroupAddon>
              )}
            </ComboboxInput>
            <ComboboxContent>
              <ComboboxEmpty>No users found</ComboboxEmpty>
              <ComboboxList>
                {(user: UserProfile) => {
                  const grant = grantsBySub.get(user.sub);
                  return (
                    <ComboboxItem key={user.sub} value={user} disabled={grant !== undefined} className="pointer-coarse:min-h-11">
                      <div className="grid min-w-0 flex-1">
                        <span className="truncate">{user.displayName}</span>
                        <span className="truncate font-mono text-xs text-muted-foreground">
                          {user.email || user.sub}
                          {grant && ` — ${grant.role}`}
                        </span>
                      </div>
                    </ComboboxItem>
                  );
                }}
              </ComboboxList>
            </ComboboxContent>
          </Combobox>

          <div className="grid gap-2">
            <Label htmlFor="role-select">Role</Label>
            <Select items={ROLES} value={selectedRole} onValueChange={(role) => setSelectedRole(role as Role)}>
              <SelectTrigger id="role-select" className="w-full pointer-coarse:data-[size=default]:h-11">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((role) => (
                  <SelectItem key={role.value} value={role.value} className="pointer-coarse:min-h-11">
                    {role.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <Button
            className="w-full pointer-coarse:h-11 md:w-auto md:justify-self-start"
            onClick={handleAssign}
            disabled={!selectedUser || assignMutation.isPending}
          >
            {assignMutation.isPending ? (
              <>
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
                {replacing ? "Replacing..." : "Assigning..."}
              </>
            ) : replacing ? (
              "Replace Role"
            ) : (
              "Assign Role"
            )}
          </Button>
        </CardContent>
      </Card>

      {undoEntry && (
        <div className="undo-banner">
          <span>
            Removed {undoEntry.displayName}&apos;s {undoEntry.role} access.
          </span>
          <button onClick={handleUndo}>
            {assignMutation.isPending ? (
              <Loader2 size={14} className="spin" />
            ) : (
              "Undo"
            )}
          </button>
        </div>
      )}
    </section>
  );
}

function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
```

- [ ] **Step 4: Run the screen's tests, the shell's, and the guards**

Run: `npx vitest run src/features/stacks/StackAccessScreen.test.tsx src/features/stacks/StackDetailShell.test.tsx && npx tsc -b`
Expected: all pass, and `tsc` reports no errors.

Run: `npx vitest run src/styles`
Expected: FAIL in `legacy.guard.test.ts` with `delete the rules for: search-wrapper, search-result-item, selected-user-card, form-row`.

- [ ] **Step 5: Delete the user-search CSS**

In `web/src/styles/features.css`:
1. Delete from the line `/* ---- User search ---- */` up to, but not including, `/* ---- Undo banner ---- */`. That block holds `.search-wrapper`, `.search-dropdown`, `.search-result-item` (with `:hover`, `.assigned` and `small`), and `.selected-user-card` (with `span`, `button`, and the `:where(.selected-user-card button svg)` Preflight restoration).
2. Delete the `.form-row { … }` rule and the blank line after it.
3. In `@media (max-width: 760px)`, delete the comment `/* 44px minimum touch targets where density and accessibility conflict. */`, the `.selected-user-card button { … }` rule under it, and the blank line after that rule.

Run: `npx vitest run src/app/AppShell.test.tsx`
Expected: FAIL in `keeps the sticky header below the legacy overlays`, with `expected 5 to be less than NaN`. The test reads `.search-dropdown`'s z-index from `features.css`, and the rule is gone.

- [ ] **Step 6: Hold the header below the overlays that replaced it**

In `web/src/app/AppShell.test.tsx`, replace everything from the comment `// The header is sticky and opaque.` to the end of the file with:

```ts
// The header is sticky and opaque. Whatever opens over scrolled content must
// paint above it: the Combobox and Select popups, which Base UI portals to the
// body, and the legacy .undo-banner on stack access until Task 3 of PR 5 moves
// it. Update this test when an overlay is added or moves.
describe("AppShell layering", () => {
  const read = (path: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), path), "utf8");
  const stripComments = (css: string) => css.replace(/\/\*[\s\S]*?\*\//g, "");
  // The bare z-N, not a variant's such as md:z-10.
  const zIndex = (classes: string) => Number(classes.match(/(?:^|\s)z-(\d+)(?=\s|$)/)?.[1]);
  const positioner = (path: string, primitive: string) =>
    read(path).match(new RegExp(`<${primitive}\\.Positioner[^>]*?className="([^"]*)"`))?.[1] ?? "";

  it("keeps the sticky header below the overlays that open over it", () => {
    const header = zIndex(read("AppShell.tsx").match(/<header className="([^"]*)"/)?.[1] ?? "");
    const overlays = {
      "Combobox popup": zIndex(positioner("../components/ui/combobox.tsx", "ComboboxPrimitive")),
      "Select popup": zIndex(positioner("../components/ui/select.tsx", "SelectPrimitive")),
      ".undo-banner": Number(
        stripComments(read("../styles/features.css")).match(/\.undo-banner \{[^}]*z-index: (\d+);/)?.[1]
      )
    };

    expect(header).toBeGreaterThan(0);
    for (const [overlay, z] of Object.entries(overlays)) {
      expect(z, overlay).toBeGreaterThan(header);
    }
  });
});
```

In `web/src/app/AppShell.tsx`, replace the comment above `<header>`:

```tsx
      {/* z-5 keeps the legacy overlays that must cover this bar above it:
          .search-dropdown (10) and .undo-banner (20) in features.css.
          AppShell.test.tsx checks the order. */}
```

with:

```tsx
      {/* z-5 keeps the overlays that open over this bar above it: Base UI's
          popups (z-50) and the legacy .undo-banner (20) in features.css.
          AppShell.test.tsx checks the order. */}
```

- [ ] **Step 7: Run everything this task touched**

Run: `npx vitest run src/features/stacks src/app src/styles src/dev && npx tsc -b`
Expected: all pass, and `tsc` reports no errors.

Run: `rg -n "search-wrapper|search-dropdown|search-result-item|selected-user-card|form-row|\.assigned" src`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add src/features/stacks/StackAccessScreen.tsx src/features/stacks/StackAccessScreen.test.tsx src/styles/features.css src/app/AppShell.tsx src/app/AppShell.test.tsx
git commit -m "feat(web): assign a stack role through the shadcn Combobox and Select" -m "The user search is a Combobox over the server's answer: filter={null}, opened once a search of two or more characters has answered, with arrow keys, Enter and Escape. The pick is the Combobox's value, so the input shows it and the selected-user card goes; a labelled clear button replaces its X. Typing keeps the pick, because Base UI empties its input when the value goes to null. Users who already hold a role are disabled options. The role picker is a Select. The AppShell layering test now checks the header against Base UI's popups." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Current Grants, the page layout, and the undo banner

What this task migrates:
- **The page grid.** The legacy `.workflow-grid` becomes a Tailwind grid with the same split on a wide screen: seven columns, 3 and 4, near the legacy 0.85fr and 1.15fr. It is one column below `lg`.
- **The grants panel.** It becomes a Card, and the list a bordered `ul` with a rule between rows. Each row's name and email truncate, with a `title`. Each role is a `RoleBadge`. On a narrow row the actions wrap to a line of their own (`flex-wrap`, `ml-auto`).
- **The undo banner.** It becomes a toast: fixed `inset-x-4 bottom-6`, and `mx-auto w-fit`, which centres it and keeps it inside the screen's edges. It is `role="status"`, with `z-20`.
- **The load error.** "Failed to load grants." becomes an Alert, with an outline Retry button below it, as the other migrated screens' Retry buttons sit.
- **Legacy CSS.** The `role-badge` rules go too, since this screen was their last user. So does `.alert .secondary-button`: the grants error's Retry was the only `.secondary-button` inside an `.alert`.

**Files:**
- Modify: `web/src/features/stacks/StackAccessScreen.tsx` (replaced whole)
- Modify: `web/src/styles/features.css` and `web/src/styles/primitives.css` (deletions)
- Modify: `web/src/styles/styles.guard.test.ts` (one stale allowlist entry)
- Modify: `web/src/app/AppShell.tsx` (the comment above `<header>`)
- Modify: `web/src/features/stacks/StackDetailShell.tsx` (a comment)
- Test: `web/src/features/stacks/StackAccessScreen.test.tsx`
- Test: `web/src/app/AppShell.test.tsx` (the layering test)

**Interfaces:**
- Consumes, from Task 2: everything that task produces. From PR 2: `RoleBadge` (`src/shared/RoleBadge.tsx`, `<RoleBadge stackRole={role} />`), which renders a Badge with `data-slot="badge"` and `data-role`.
- Produces: the finished screen. Its undo banner is the only `<div role="status" className="…">` in `StackAccessScreen.tsx`, and the layering test finds it that way.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/stacks/StackAccessScreen.test.tsx`, add before the closing `});` of `describe("StackAccessScreen", …)`:

```tsx
  describe("current grants", () => {
    it("shows each grant's role as a RoleBadge", async () => {
      serve({ grants: twoGrants.grants });
      await renderWithGrants(twoGrants.grants);

      const owner = screen.getByText("owner");
      expect(owner.getAttribute("data-slot")).toBe("badge");
      expect(owner.getAttribute("data-role")).toBe("owner");
    });

    it("cuts a long name or email short and shows it whole on hover", async () => {
      const name = "Bartholomew Montgomery-Fitzwilliam the Third, Platform Operations";
      const email = "bartholomew.montgomery-fitzwilliam@platform-operations.example.com";
      serve({ grants: [{ userSub: "u9", role: "viewer", displayName: name, email }] });
      render(<StackAccessScreen />, { wrapper: wrapper() });

      for (const text of [name, email]) {
        const element = await screen.findByText(text);
        expect(element.classList).toContain("truncate");
        expect(element.getAttribute("title")).toBe(text);
      }
    });

    it("puts a failed load in an Alert, with a Retry that loads again", async () => {
      let calls = 0;
      vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
        calls += 1;
        return calls === 1
          ? new Response(JSON.stringify({ error: "internal", message: "boom" }), {
              status: 500,
              headers: { "content-type": "application/json" }
            })
          : new Response(JSON.stringify(twoGrants), { status: 200, headers: { "content-type": "application/json" } });
      });
      render(<StackAccessScreen />, { wrapper: wrapper() });

      expect((await screen.findByRole("alert")).textContent).toContain("Failed to load grants.");
      await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }));
      expect(await screen.findByText("Alice")).toBeDefined();
    });

    it("offers Undo in a status banner after a revoke, and Undo restores the role", async () => {
      const sent = serve({ grants: twoGrants.grants });
      await renderWithGrants(twoGrants.grants);
      const user = userEvent.setup();

      await user.click(screen.getByRole("button", { name: "Revoke Bob's viewer role" }));
      await user.click(screen.getByRole("button", { name: "Confirm" }));
      const banner = (await screen.findByText(/Removed Bob.*viewer access/)).closest<HTMLElement>('[role="status"]');
      expect(banner).not.toBeNull();

      await user.click(within(banner as HTMLElement).getByRole("button", { name: "Undo" }));
      await waitFor(() =>
        expect(sent.find(({ method }) => method === "POST")?.body).toEqual({ user_sub: "u2", role: "viewer" })
      );
    });
  });
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/stacks/StackAccessScreen.test.tsx`
Expected: 4 failed, 18 passed:
- `shows each grant's role as a RoleBadge`: `expected null to be 'badge'`;
- `cuts a long name or email short…`: `expected "" to contain "truncate"`;
- `puts a failed load in an Alert…`: `Unable to find role="alert"`;
- `offers Undo in a status banner…`: `expected null not to be null`.

- [ ] **Step 3: Replace the screen**

Replace the whole of `web/src/features/stacks/StackAccessScreen.tsx` with:

```tsx
import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { CircleAlert, Loader2, RefreshCw, Search, Shield, Trash2, X } from "lucide-react";
import {
  useStackGrantsQuery,
  useSearchUsersQuery,
  useAssignStackRoleMutation,
  useRevokeStackRoleMutation
} from "../../api/queries";
import type { GrantView, UserProfile } from "../../api/types";
import { tenantID } from "../../config";
import RoleBadge from "../../shared/RoleBadge";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Combobox, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from "@/components/ui/combobox";
import { InputGroupAddon, InputGroupButton } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const ROLES = [
  { value: "owner", label: "Owner" },
  { value: "operator", label: "Operator" },
  { value: "approver", label: "Approver" },
  { value: "viewer", label: "Viewer" }
] as const;
type Role = (typeof ROLES)[number]["value"];

interface UndoEntry {
  userSub: string;
  role: string;
  displayName: string;
}

// base.css gives every h2 the legacy 32px display type until PR 9, so each
// heading sets its own family, size, weight and tracking.
const headingClass = "flex items-center gap-2 font-heading text-base leading-snug font-medium tracking-normal";

export default function StackAccessScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const grants = useStackGrantsQuery(tenantID, stackId);

  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserProfile | null>(null);
  const [selectedRole, setSelectedRole] = useState<Role>("viewer");
  const [undoEntry, setUndoEntry] = useState<UndoEntry | null>(null);
  const [mutationError, setMutationError] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);
  const debouncedSearch = useDebounce(search, 300);
  const searchResults = useSearchUsersQuery(tenantID, debouncedSearch);
  const assignMutation = useAssignStackRoleMutation(tenantID, stackId);
  const revokeMutation = useRevokeStackRoleMutation(tenantID, stackId);
  const undoTimeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  const grantsBySub = new Map((grants.data?.grants ?? []).map((g) => [g.userSub, g]));
  const replacing = selectedUser !== null && grantsBySub.has(selectedUser.sub);
  // The list opens once a search has answered, as the old dropdown did: before
  // that there is nothing to show, and "No users found" would be untrue.
  const results = debouncedSearch.length >= 2 ? searchResults.data?.users : undefined;

  const handleAssign = useCallback(async () => {
    if (!selectedUser) return;
    setMutationError("");
    try {
      await assignMutation.mutateAsync({
        user_sub: selectedUser.sub,
        role: selectedRole
      });
      setSelectedUser(null);
      setSearch("");
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : "Failed to assign role");
    }
  }, [selectedUser, selectedRole, assignMutation]);

  const handleRevoke = useCallback(
    async (grant: GrantView) => {
      setMutationError("");
      setConfirmRevoke(null);
      try {
        await revokeMutation.mutateAsync(grant.userSub);
        setUndoEntry({
          userSub: grant.userSub,
          role: grant.role,
          displayName: grant.displayName
        });
      } catch (err) {
        setMutationError(err instanceof Error ? err.message : "Failed to revoke role");
      }
    },
    [revokeMutation]
  );

  const handleUndo = useCallback(async () => {
    if (!undoEntry) return;
    setMutationError("");
    try {
      await assignMutation.mutateAsync({
        user_sub: undoEntry.userSub,
        role: undoEntry.role
      });
    } catch {
      setMutationError("Failed to restore role");
    }
    setUndoEntry(null);
  }, [undoEntry, assignMutation]);

  useEffect(() => {
    if (!undoEntry) return;
    undoTimeoutRef.current = setTimeout(() => setUndoEntry(null), 5000);
    return () => clearTimeout(undoTimeoutRef.current);
  }, [undoEntry]);

  useEffect(() => {
    if (revokeMutation.isSuccess || assignMutation.isSuccess) {
      setMutationError("");
    }
  }, [revokeMutation.isSuccess, assignMutation.isSuccess]);

  return (
    // Two columns on a wide screen, split 3:4 as the legacy grid's 0.85fr and
    // 1.15fr were; one column below lg. The grid sets its own text colour,
    // since body keeps the legacy one until PR 9.
    <section className="grid gap-6 text-foreground lg:grid-cols-7">
      <Card className="lg:col-span-3">
        <CardHeader>
          <h2 className={headingClass}>
            <Shield aria-hidden="true" className="size-4" />
            Current Grants
          </h2>
        </CardHeader>
        <CardContent className="grid gap-4">
          {grants.isLoading && (
            <p className="flex items-center gap-2 text-muted-foreground">
              <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading grants...
            </p>
          )}
          {grants.isError && (
            <div className="grid justify-items-start gap-3">
              <Alert variant="destructive">
                <CircleAlert aria-hidden="true" />
                <AlertTitle>Failed to load grants.</AlertTitle>
              </Alert>
              <Button variant="outline" className="pointer-coarse:h-11" onClick={() => grants.refetch()}>
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
                Retry
              </Button>
            </div>
          )}
          {grants.data && grants.data.grants.length === 0 && (
            <p className="text-muted-foreground">
              No users have been assigned access yet. Use the panel on the right to add the first grant.
            </p>
          )}
          {grants.data && grants.data.grants.length > 0 && (
            <ul className="divide-y rounded-lg border">
              {grants.data.grants.map((grant) => (
                <li key={grant.userSub} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2">
                  <div className="grid min-w-0 flex-1 gap-0.5">
                    <span className="truncate" title={grant.displayName}>
                      {grant.displayName}
                    </span>
                    {grant.email && (
                      <span className="truncate text-xs text-muted-foreground" title={grant.email}>
                        {grant.email}
                      </span>
                    )}
                  </div>
                  <RoleBadge stackRole={grant.role} />
                  <div className="ml-auto flex items-center gap-2">
                    {confirmRevoke === grant.userSub ? (
                      <>
                        <span>Remove access?</span>
                        <Button variant="destructive" className="pointer-coarse:h-11" onClick={() => handleRevoke(grant)}>
                          Confirm
                        </Button>
                        <Button variant="outline" className="pointer-coarse:h-11" onClick={() => setConfirmRevoke(null)}>
                          Cancel
                        </Button>
                      </>
                    ) : (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="pointer-coarse:size-11"
                        onClick={() => setConfirmRevoke(grant.userSub)}
                        aria-label={`Revoke ${grant.displayName}'s ${grant.role} role`}
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card className="lg:col-span-4">
        <CardHeader>
          <h2 className={headingClass}>
            <Search aria-hidden="true" className="size-4" />
            Assign Role
          </h2>
        </CardHeader>
        <CardContent className="grid gap-4">
          {mutationError && (
            <Alert variant="destructive">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{mutationError}</AlertTitle>
            </Alert>
          )}
          {/* The server searches, by name and email, so the list shows its
              answer as it is (filter={null}). The pick is the Combobox's
              value: the input shows the picked name, and keeps it while the
              user searches again, until another pick, the clear button or
              Escape. */}
          <Combobox<UserProfile>
            items={results ?? []}
            filter={null}
            open={searchOpen && results !== undefined}
            onOpenChange={setSearchOpen}
            value={selectedUser}
            onValueChange={(user) => setSelectedUser(user)}
            onInputValueChange={(value, { reason }) => {
              // Typing searches. Picking a user writes their name into the
              // input, which is not a query, so only typing sets one.
              if (reason === "input-change") {
                setSearch(value);
              } else if (value === "") {
                setSearch("");
              }
            }}
            itemToStringLabel={(user) => user.displayName}
            isItemEqualToValue={(a, b) => a.sub === b.sub}
          >
            <ComboboxInput
              aria-label="Search users"
              placeholder="Search users by name or email..."
              showTrigger={false}
              className="w-full pointer-coarse:h-11 pointer-coarse:*:data-[slot=input-group-control]:h-full"
            >
              {selectedUser && (
                <InputGroupAddon align="inline-end">
                  <InputGroupButton
                    size="icon-xs"
                    aria-label="Clear selected user"
                    className="pointer-coarse:size-11"
                    onClick={() => setSelectedUser(null)}
                  >
                    <X aria-hidden="true" />
                  </InputGroupButton>
                </InputGroupAddon>
              )}
            </ComboboxInput>
            <ComboboxContent>
              <ComboboxEmpty>No users found</ComboboxEmpty>
              <ComboboxList>
                {(user: UserProfile) => {
                  const grant = grantsBySub.get(user.sub);
                  return (
                    <ComboboxItem key={user.sub} value={user} disabled={grant !== undefined} className="pointer-coarse:min-h-11">
                      <div className="grid min-w-0 flex-1">
                        <span className="truncate">{user.displayName}</span>
                        <span className="truncate font-mono text-xs text-muted-foreground">
                          {user.email || user.sub}
                          {grant && ` — ${grant.role}`}
                        </span>
                      </div>
                    </ComboboxItem>
                  );
                }}
              </ComboboxList>
            </ComboboxContent>
          </Combobox>

          <div className="grid gap-2">
            <Label htmlFor="role-select">Role</Label>
            <Select items={ROLES} value={selectedRole} onValueChange={(role) => setSelectedRole(role as Role)}>
              <SelectTrigger id="role-select" className="w-full pointer-coarse:data-[size=default]:h-11">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((role) => (
                  <SelectItem key={role.value} value={role.value} className="pointer-coarse:min-h-11">
                    {role.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <Button
            className="w-full pointer-coarse:h-11 md:w-auto md:justify-self-start"
            onClick={handleAssign}
            disabled={!selectedUser || assignMutation.isPending}
          >
            {assignMutation.isPending ? (
              <>
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
                {replacing ? "Replacing..." : "Assigning..."}
              </>
            ) : replacing ? (
              "Replace Role"
            ) : (
              "Assign Role"
            )}
          </Button>
        </CardContent>
      </Card>

      {/* A toast: fixed above the page, centred, and inside the screen's
          edges on a phone. z-20 puts it over the sticky header (z-5) and
          under Base UI's popups (z-50); AppShell.test.tsx checks the order. */}
      {undoEntry && (
        <div
          role="status"
          className="fixed inset-x-4 bottom-6 z-20 mx-auto flex w-fit items-center gap-4 rounded-lg border bg-popover py-2 pr-2 pl-4 text-sm text-popover-foreground shadow-lg"
        >
          <span>
            Removed {undoEntry.displayName}&apos;s {undoEntry.role} access.
          </span>
          <Button className="pointer-coarse:h-11" onClick={handleUndo} aria-label="Undo">
            {assignMutation.isPending ? <Loader2 aria-hidden="true" className="animate-spin" /> : "Undo"}
          </Button>
        </div>
      )}
    </section>
  );
}

function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
```

Run: `npx vitest run src/features/stacks/StackAccessScreen.test.tsx src/features/stacks/StackDetailShell.test.tsx && npx tsc -b`
Expected: all pass, 22 in `StackAccessScreen.test.tsx`, and `tsc` reports no errors.

- [ ] **Step 4: Delete the legacy CSS this screen was the last to use**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL in two tests:
- `primitives.css`: `delete the rules for: role-badge, role-badge--owner, role-badge--operator, role-badge--approver, role-badge--viewer`;
- `features.css`: `delete the rules for: grants-list, grant-row, grant-user, grant-actions, danger, confirm-label`.

`undo-banner` isn't listed because `AppShell.tsx`'s comment still names it; Step 5 changes that comment.

In `web/src/styles/features.css`:
1. Delete from the line `/* ---- Grants ---- */` up to, but not including, `/* ---- Layout grids ---- */`. That block holds every grants rule, the `.grant-actions` button rules with `.danger` and the Preflight `:where()` restoration, and every `.undo-banner` rule.
2. Delete this whole section, which leaves the `@media (max-width: 920px)` rule after it in place:

   ```css
   /* ---- Inline-style drain (Task 8) ---- */
   .alert .secondary-button {
     margin-left: var(--legacy-space-3);
   }

   .confirm-label {
     font-size: var(--legacy-text-sm);
   }

   ```

3. In `@media (max-width: 760px)`, delete the blank line and the `.undo-banner { … }` rule (with `left`, `right` and `transform`) before the block's closing `}`.

In `web/src/styles/primitives.css`, delete from the line `/* ---- Role badges ---- */` up to, but not including, `/* ---- Data tables ----`.

In `web/src/styles/styles.guard.test.ts`, delete this line from `OFF_SCALE_ALLOWED`; the rule it excused is gone:

```ts
  ".role-badge|min-width",              // sized to the longest role label so the pills form a column
```

Run: `npx vitest run src/app/AppShell.test.tsx`
Expected: FAIL in `keeps the sticky header below the overlays that open over it`, with `.undo-banner: expected NaN to be greater than 5`.

- [ ] **Step 5: Hold the header below the new banner, and update two comments**

In `web/src/app/AppShell.test.tsx`, replace everything from the comment `// The header is sticky and opaque.` to the end of the file with:

```ts
// The header is sticky and opaque. Whatever opens over scrolled content must
// paint above it: the Combobox and Select popups, which Base UI portals to the
// body, and the undo banner on stack access. Update this test when an overlay
// is added or moves.
describe("AppShell layering", () => {
  const read = (path: string) => readFileSync(join(dirname(fileURLToPath(import.meta.url)), path), "utf8");
  // The bare z-N, not a variant's such as md:z-10.
  const zIndex = (classes: string) => Number(classes.match(/(?:^|\s)z-(\d+)(?=\s|$)/)?.[1]);
  const positioner = (path: string, primitive: string) =>
    read(path).match(new RegExp(`<${primitive}\\.Positioner[^>]*?className="([^"]*)"`))?.[1] ?? "";

  it("keeps the sticky header below the overlays that open over it", () => {
    const header = zIndex(read("AppShell.tsx").match(/<header className="([^"]*)"/)?.[1] ?? "");
    const overlays = {
      "Combobox popup": zIndex(positioner("../components/ui/combobox.tsx", "ComboboxPrimitive")),
      "Select popup": zIndex(positioner("../components/ui/select.tsx", "SelectPrimitive")),
      "undo banner": zIndex(
        read("../features/stacks/StackAccessScreen.tsx").match(/<div\s+role="status"\s+className="([^"]*)"/)?.[1] ?? ""
      )
    };

    expect(header).toBeGreaterThan(0);
    for (const [overlay, z] of Object.entries(overlays)) {
      expect(z, overlay).toBeGreaterThan(header);
    }
  });
});
```

In `web/src/app/AppShell.tsx`, replace the comment above `<header>` with:

```tsx
      {/* z-5 keeps the overlays that open over this bar above it: Base UI's
          popups (z-50) and the undo banner on stack access (z-20).
          AppShell.test.tsx checks the order. */}
```

In `web/src/features/stacks/StackDetailShell.tsx`, the access screen now sets its own colour. Replace:

```tsx
    // No text colour here: the shell wraps screens still on the legacy
    // layer (templates, access), which inherit theirs from body until they
    // migrate. RouteTabs sets its own.
```

with:

```tsx
    // No text colour here: the shell wraps the template screens, still on
    // the legacy layer, which inherit theirs from body until they migrate.
    // RouteTabs sets its own.
```

- [ ] **Step 6: Run the whole suite**

Run: `npx tsc -b && npm test`
Expected: `tsc` reports no errors, and all tests pass: 734 in 56 files, up from 719 on `main`.

Run: `rg -n "grants-list|grant-row|grant-user|grant-actions|confirm-label|undo-banner|role-badge|button\.danger|\.alert \.secondary-button" src`
Expected: no output.

Run: `rg -n "className=\"(workflow-grid|panel|muted|alert|secondary-button|primary-button|danger|spin)\"" src/features/stacks/StackAccessScreen.tsx`
Expected: no output. The screen uses no legacy class.

- [ ] **Step 7: Commit**

```bash
git add src/features/stacks/StackAccessScreen.tsx src/features/stacks/StackAccessScreen.test.tsx src/styles/features.css src/styles/primitives.css src/styles/styles.guard.test.ts src/app/AppShell.tsx src/app/AppShell.test.tsx src/features/stacks/StackDetailShell.tsx
git commit -m "feat(web): current grants and the undo banner on shadcn" -m "Grants sit in a Card as a bordered list: names and emails truncate with a title, roles are RoleBadges, and a narrow row's actions wrap. The load error is an Alert with Retry. The undo banner is a role=status toast built from Tailwind, fixed and centred, z-20 between the sticky header and Base UI's popups. The grid splits 3:4 on a wide screen as the legacy one did. The role-badge, grants, undo-banner and .alert .secondary-button CSS go: this screen was their last user." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Restore `drive-web.mjs`'s phone emulation and `--signed-out`

When `6b55ade` ("chore: let drive-web emulate a phone") was rebased onto the Dex change, the conflict resolution kept the flag but dropped the code that acts on it. The original commit, `39ad2a9`, is still in the reflog. Two things broke:
- **`--width`** is parsed and documented, but nothing calls Chrome's emulation, so the page never sees a phone viewport or a coarse pointer.
- **`--signed-out`** no longer gates the two sign-in steps. The driver clicks "Sign in" and fills Dex's form anyway, with undefined credentials when none are set.

The audit in Task 5 needs both: 375px measurements on a touch pointer, and `/styleguide` signed out.

**Files:**
- Modify: `scripts/drive-web.mjs`

**Interfaces:**
- Consumes: nothing.
- Produces, for Task 5:
  - `--width <px>` sets a mobile viewport that wide, 812px tall, with touch emulation, so `(pointer: coarse)` matches;
  - `--signed-out` skips both sign-in steps.

- [ ] **Step 1: Emulate the phone before the first navigation**

In `scripts/drive-web.mjs`, replace:

```js
// localhost, not 127.0.0.1: the transaction cookie is set on the origin that
// starts sign-in, and the provider sends the browser back to localhost.
await send("Page.navigate", { url: "http://localhost:5173/stacks" });
```

with:

```js
// Before the first load, so the app starts on a phone: a mobile viewport,
// and touch, which is what makes (pointer: coarse) match.
if (width) {
  await send("Emulation.setDeviceMetricsOverride", { width, height: 812, deviceScaleFactor: 1, mobile: true });
  await send("Emulation.setTouchEmulationEnabled", { enabled: true, maxTouchPoints: 5 });
}
// localhost, not 127.0.0.1: the transaction cookie is set on the origin that
// starts sign-in, and the provider sends the browser back to localhost.
await send("Page.navigate", { url: "http://localhost:5173/stacks" });
```

- [ ] **Step 2: Skip sign-in when asked to**

Replace:

```js
if (await evaluate("!!document.querySelector('[data-testid=signin-submit]')")) {
```

with:

```js
if (!signedOut && await evaluate("!!document.querySelector('[data-testid=signin-submit]')")) {
```

and:

```js
if (await evaluate("!!document.querySelector('#login')")) {
```

with:

```js
if (!signedOut && await evaluate("!!document.querySelector('#login')")) {
```

- [ ] **Step 3: Check the syntax**

Run from the repository root: `node --check scripts/drive-web.mjs`
Expected: no output.

- [ ] **Step 4: Try it against the running app**

This needs a dev server on port 5173: the local stack's `web` container is a production build, and production builds don't register `/styleguide`. It also needs headless Chrome, started as the script's header shows. From the repository root:

```bash
docker compose stop web        # frees port 5173
(cd web && npm run dev) &
node scripts/drive-web.mjs --signed-out --goto /styleguide --probe 'JSON.stringify({ styleguide: !!document.querySelector("[data-testid=styleguide]"), coarse: matchMedia("(pointer: coarse)").matches, width: innerWidth })'
node scripts/drive-web.mjs --signed-out --width 375 --goto /styleguide --probe 'JSON.stringify({ styleguide: !!document.querySelector("[data-testid=styleguide]"), coarse: matchMedia("(pointer: coarse)").matches, width: innerWidth })'
```

Expected: `{"styleguide":true,"coarse":false,"width":1512}`, then `{"styleguide":true,"coarse":true,"width":375}`. Before this task, the second printed `"coarse":false` and `"width":1512`, and with no credentials in the environment both ended on Dex's login page (`"styleguide":false`).

Leave the dev server and Chrome running if Task 5 follows straight away. Otherwise stop them, and run `docker compose start web`.

- [ ] **Step 5: Commit**

From the repository root:

```bash
git add scripts/drive-web.mjs
git commit -m "fix: restore drive-web's phone emulation and --signed-out" -m "Rebasing 6b55ade onto the Dex change kept --width's parsing but dropped the Emulation calls that act on it, and dropped the signedOut check from both sign-in steps. --width emulated nothing, and --signed-out signed in anyway. Both are back as 39ad2a9 had them." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Browser audit, spec amendments, and the PR

This audit checks two things:
1. **The screens PR 5 didn't migrate must not move.** Their computed styles are compared before and after. Every rule PR 5 deletes belonged to the access screen, so the expected result is CLEAN.
2. **The access screen must look right,** in every state. That means screenshots, plus the checks in Review Focus.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`
- Scratch, not committed: everything under `$AUDIT`

Scratch files live in `$AUDIT`: this session's scratchpad directory plus `/audit5`, outside the repository. Shell variables don't survive between commands here, so every command block below starts by exporting it, along with the driver's credentials where they're needed. The credentials are the local Dex root user from `docs/authentication.md`, a local-only password.

**Interfaces:**
- Consumes:
  - everything above;
  - `scripts/drive-web.mjs` with `--width` and `--signed-out` (Task 4), `--respond` and `--stall` (PR 4), and `--fail`, `--eval` and `--reload` (PR 3).
- Produces: the PR.

- [ ] **Step 1: Save the dump and diff scripts**

Save as `$AUDIT/dump.js`. It is PR 4's dump, unchanged. Its exemption for the pieces PR 4 migrated, the stack tab row and the credentials card, still holds: those pieces are the same before and after.

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

Save as `$AUDIT/diff.py`. It is PR 4's, unchanged:

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

- [ ] **Step 2: List the screens and write the screenshot script**

The local stack must be up: `docker compose up -d` from the repository root. The local database needs a stack with an installed template and at least one run of it, and at least one registry template. If any is missing, create it through the UI first.

Sign in once and collect real IDs: click into the stack, one of its templates, one run of that template, and one registry template. After each click, run `node scripts/drive-web.mjs --probe 'location.pathname'`.

Write `$AUDIT/screens.txt`, one `name path` pair per line. These are the screens PR 5 didn't migrate: PR 4's list without `access`.

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
registry /templates
registry-new /templates/new
registry-template /templates/{sourceTemplateId}
```

Write `$AUDIT/shots.sh`. It captures the access screen in every state PR 5 touched. It takes the output directory, a file prefix, the stack ID and, optionally, a phone width:

```bash
#!/usr/bin/env bash
# Usage: shots.sh <out-dir> <before|after> <stackId> [width]
set -u
out=$1 tag=$2 stack=$3 width=${4:-}
[ -n "$width" ] && tag="$tag-$width"
d() { node scripts/drive-web.mjs ${width:+--width "$width"} "$@"; }
access="/stacks/$stack/access"
list='*/v1/tenants/*/stacks/*/grants'
one='*/v1/tenants/*/stacks/*/grants/*'
search='*/v1/tenants/*/users/search*'
grant() { printf '{"userSub":"%s","role":"%s","displayName":"%s","email":"%s"}' "$1" "$2" "$3" "$4"; }
four="{\"grants\":[$(grant u1 owner 'Ada Lovelace' ada@example.com),$(grant u2 operator 'Grace Hopper' grace@example.com),$(grant u3 approver 'Margaret Hamilton' margaret@example.com),$(grant u4 viewer 'Katherine Johnson' katherine@example.com)]}"
long="{\"grants\":[$(grant u1 owner 'Bartholomew Montgomery-Fitzwilliam the Third, Platform Operations' bartholomew.montgomery-fitzwilliam@platform-operations.example.com),$(grant u2 viewer Bo bo@example.com)]}"
users='{"users":[{"sub":"u5","displayName":"Dorothy Vaughan","email":"dorothy@example.com"},{"sub":"u1","displayName":"Ada Lovelace","email":"ada@example.com"}],"first":0,"max":20}'
nobody='{"users":[],"first":0,"max":20}'
# Types the way a keyboard does. Base UI's input doesn't open its list for a
# value set from script plus a synthetic input event.
type='(async () => {
  const input = document.querySelector("[aria-label=\"Search users\"]");
  input.focus();
  document.execCommand("insertText", false, "do");
  await new Promise((r) => setTimeout(r, 1500));
  return document.querySelectorAll("[role=option]").length + " options";
})()'
# Picks Dorothy: ArrowDown and Enter on the Combobox, and a click for the
# legacy dropdown, which has no keyboard support.
pick='(async () => {
  const wait = (ms) => new Promise((r) => setTimeout(r, ms));
  const input = document.querySelector("[aria-label=\"Search users\"]");
  const key = (k) => input.dispatchEvent(new KeyboardEvent("keydown", { key: k, bubbles: true, cancelable: true }));
  input.focus();
  document.execCommand("insertText", false, "do");
  await wait(1500);
  key("ArrowDown");
  await wait(200);
  key("Enter");
  await wait(500);
  document.querySelector("[role=option]")?.click();
  return input.value;
})()'
revoke='document.querySelector("[aria-label^=\"Revoke Katherine\"]").click(); "revoke"'
confirm='[...document.querySelectorAll("button")].find((b) => b.textContent.trim() === "Confirm").click(); "confirm"'
role='document.getElementById("role-select").click(); "role"'
d --goto "$access" --shot "$out/$tag-access.png"
d --respond "$list=$four" --goto "$access" --shot "$out/$tag-access-roles.png"
d --respond "$list=$long" --goto "$access" --shot "$out/$tag-access-long.png"
d --respond "$list={\"grants\":[]}" --goto "$access" --shot "$out/$tag-access-empty.png"
d --stall "$list" --goto "$access" --shot "$out/$tag-access-loading.png"
d --fail "$list=500" --goto "$access" --reload --shot "$out/$tag-access-error.png"
d --respond "$list=$four" --respond "$search=$users" --goto "$access" --eval "$type" --shot "$out/$tag-access-search.png"
d --respond "$list=$four" --respond "$search=$nobody" --goto "$access" --eval "$type" --shot "$out/$tag-access-nobody.png"
d --respond "$list=$four" --respond "$search=$users" --goto "$access" --eval "$pick" --shot "$out/$tag-access-picked.png"
d --respond "$list=$four" --goto "$access" --eval "$role" --shot "$out/$tag-access-role.png"
d --respond "$list=$four" --goto "$access" --eval "$revoke" --shot "$out/$tag-access-confirm.png"
d --respond "$list=$four" --respond "$one={}" --goto "$access" --eval "$revoke" --eval "$confirm" --shot "$out/$tag-access-undo.png"
d --respond "$list=$four" --fail "$one=409" --goto "$access" --eval "$revoke" --eval "$confirm" --shot "$out/$tag-access-revoke-error.png"
d --signed-out --goto /styleguide --shot "$out/$tag-styleguide.png"
```

Some notes on the script:
- **No real data changes.** `--respond` answers the list, the search and the revoke, so no grant in the local database changes. The `{}` answers the DELETE, and `requestNoContent` reads only the status.
- **The globs.** The list glob doesn't match one grant's URL, since a glob must match the whole URL. The search glob ends in `*` to cover the query string.
- **Why the reload on the error.** The grants query retries three times before the screen shows its error. `--reload` re-runs the query against `--fail` and settles long enough for the retries.
- **The undo shot.** It is taken inside the banner's five seconds: each `--eval` settles 1.5s.

- [ ] **Step 3: Capture "before" from `main`**

If Task 4 left its dev server running, stop it first (find it with `lsof -i :5173`). If its Chrome is still running on port 9222, leave out the Chrome line below and keep using that one.

From the repository root:

```bash
export AUDIT=<this session's scratchpad>/audit5
export OPENPLAN_USER=admin@openplan.local
export OPENPLAN_PASS=admin-local-only
git worktree add "$AUDIT/before-tree" main
(cd "$AUDIT/before-tree/web" && npm ci)
docker compose stop web        # frees port 5173, the only redirect URI Dex accepts
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

The driver runs from this branch's working tree, which has Task 4's fixes, against whichever app is on port 5173. Afterwards, stop that dev server (find it with `lsof -i :5173`) and keep Chrome running.

- [ ] **Step 4: Capture "after" from this branch**

Start `npm run dev` from `web/` on `feat/shadcn-stack-access`. Then run the same loop into `$AUDIT/after/`, and the two `shots.sh` runs with `after` in place of `before`.

- [ ] **Step 5: Diff**

Run: `python3 "$AUDIT/diff.py" "$AUDIT/before" "$AUDIT/after"`
Expected: `CLEAN`. Every screen PR 5 didn't migrate is unchanged.

A difference means a rule was deleted too broadly in Task 2 or Task 3. The likeliest is `.alert .secondary-button`, if some screen's Retry sat in an `.alert` after all. Fix it in the task that owns it, re-capture "after", and diff again.

- [ ] **Step 6: Check what the diff can't**

Still on the "after" server:

1. **Every state was really reached.** Open each `after-*.png`:
   - `access-roles` shows four rows with blue, green, amber and grey badges;
   - `access-long` shows the long name and email cut with an ellipsis;
   - `access-empty` shows "No users have been assigned access yet…";
   - `access-loading` shows "Loading grants...";
   - `access-error` shows a red Alert reading "Failed to load grants." and a Retry button;
   - `access-search` shows Dorothy Vaughan, and Ada Lovelace greyed out with "— owner";
   - `access-nobody` shows "No users found";
   - `access-picked` shows "Dorothy Vaughan" in the input, with an X button;
   - `access-role` shows the open role list, with Viewer checked;
   - `access-confirm` shows "Remove access?", Confirm and Cancel on Katherine's row;
   - `access-undo` shows the banner "Removed Katherine Johnson's viewer access." with Undo;
   - `access-revoke-error` shows a red Alert in the Assign Role card.

   If a shot shows the ordinary screen instead, its glob missed. Print the request URLs with `--probe 'JSON.stringify(performance.getEntriesByType("resource").map((e) => e.name).filter((n) => n.includes("/v1/")))'` and correct the glob.
2. **Fields and headings are shadcn's size on a mouse.** Run:

   ```bash
   node scripts/drive-web.mjs --goto "/stacks/<stackId>/access" --probe 'JSON.stringify({ search: document.querySelector("[data-slot=input-group]").getBoundingClientRect().height, role: document.getElementById("role-select").getBoundingClientRect().height, headings: [...document.querySelectorAll("main h2")].map((h) => getComputedStyle(h).fontSize) })'
   ```

   Expected: `{"search":32,"role":32,"headings":["16px","16px"]}`. A `32px` heading means the `base.css` `h2` rule is leaking through; check `headingClass`.
3. **Nothing scrolls sideways at phone width, and every control is 44px on touch.** Save this probe as `$AUDIT/measure.js`. It measures the Combobox's InputGroup rather than the input inside its border:

   ```js
   JSON.stringify({
     scroll: document.documentElement.scrollWidth,
     width: innerWidth,
     small: [...document.querySelectorAll('main a[href], main button, main input:not([data-slot="input-group-control"]), main [data-slot="input-group"], [role="option"], [role="tab"]')]
       .filter((el) => !el.closest('nav[aria-label="Breadcrumb"]'))
       .map((el) => [el.textContent.trim() || el.getAttribute("aria-label") || el.getAttribute("data-slot") || el.tagName, Math.round(el.getBoundingClientRect().height)])
       .filter(([, height]) => height < 44)
   })
   ```

   Run it with `--width 375 --probe "$(cat "$AUDIT/measure.js")"`. Each run below combines `--respond "*/v1/tenants/*/stacks/*/grants=<the four JSON from shots.sh>"` and `--goto "/stacks/<stackId>/access"` with one of these:
   - nothing more: the list and the form;
   - `--respond "*/v1/tenants/*/users/search*=<the users JSON>" --eval "<the pick script>"`: the clear button;
   - `--respond "*/v1/tenants/*/users/search*=<the users JSON>" --eval "<the type script>"`: the options;
   - `--eval "<the role script>"`: the role options;
   - `--eval "<the revoke script>"`: Confirm and Cancel;
   - `--respond "*/v1/tenants/*/stacks/*/grants/*={}" --eval "<the revoke script>" --eval "<the confirm script>"`: the Undo button.

   Also run it once with `--respond "*/v1/tenants/*/stacks/*/grants=<the long JSON>"` and `--eval "<the revoke script with Revoke Bo>"`. That puts the confirm row next to a long name.

   Expected: `scroll` is at most `width`, and `small` is `[]`, every time. The breadcrumb links are left out: PR 2 settled them, and they carry no touch target of their own.

Then compare the before and after screenshots side by side. Expected differences:
- the two legacy panels become shadcn Cards with 16px headings and icons;
- each grant is a row in one bordered list, with a `RoleBadge` and a ghost trash button;
- the user search is an input whose list opens below it, with Ada greyed out; the picked user shows in the input with an X, and the selected-user card is gone;
- the role picker is a shadcn Select with a popup list;
- errors are red Alerts, and Retry is an outline button below the load error;
- the undo banner is a white toast with a border and a shadow, not a dark pill;
- `/styleguide` gains the Combobox, Select, Input group and Textarea specimens.

Nothing else should change.

Clean up from the repository root: stop Chrome and the dev server, run `docker compose start web`, then `git worktree remove --force "$AUDIT/before-tree"`.

- [ ] **Step 7: Bring the spec in line with what PR 5 built**

Edit `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`:

- **Status line:** `Approved. Guard and testing sections reviewed with PR 1. PRs 1 to 5 settled the details recorded below.`
- **Migration table, PR 5 row:** Main shadcn pieces becomes `Combobox for the user search; Select for the role picker; reuses Card, Alert, Button, Label and \`RoleBadge\``.
- **Migration table, PR 6 row:** Main shadcn pieces becomes `\`RouteTabs\` from PR 4 for the template's tabs, so each tab keeps its URL; Select; Textarea (vendored in PR 5)`.
- **After the "What PR 4 settled" list,** before the `HeroGraphic` paragraph, add:

  ```markdown
  **What PR 5 settled.**

  - Combobox brought InputGroup and Textarea with it, so PR 6 starts with
    Textarea vendored. `shadcn add` offers to overwrite `button.tsx`, which
    carries PR 1's `/90` hover; answer no (`yes n | npx shadcn add …`).
  - The user search is a Combobox over the server's answer, with
    `filter={null}`: the server matches email as well as name. The list
    opens once a search of two or more characters has answered, so "No
    users found" never shows before one has. Only typing sets the query; a
    pick filling the input doesn't search for the picked name.
  - The pick is the Combobox's value: the input shows the picked name, and
    the selected-user card is gone. A clear button inside the input, still
    named "Clear selected user", replaces the card's X. Typing keeps the
    pick, because Base UI empties its input when the value goes to `null`;
    Escape, the clear button or another pick changes it. Users who already
    hold a role are disabled options that show it.
  - While a Combobox popup is open, Base UI hides the rest of the page from
    assistive technology, so tests query outside the popup only once it has
    closed. `ComboboxEmpty` is `role="status"`; tests find the undo banner,
    also a status, by its text.
  - Select's trigger height is `data-[size=default]:h-8`, which a plain
    `pointer-coarse:h-11` can't outrank; `pointer-coarse:data-[size=default]:h-11`
    does. The search input fills its taller group on a coarse pointer
    through `pointer-coarse:*:data-[slot=input-group-control]:h-full`.
    Audits measure the InputGroup, not the input inside its border.
  - The undo banner is a Tailwind toast (fixed, `role="status"`, the
    popover look), not Sonner. `AppShell.test.tsx` now holds the header
    (`z-5`) below Base UI's popups (`z-50`) and the banner (`z-20`), the
    overlays that replaced `.search-dropdown` and `.undo-banner`.
  - `role-badge` CSS went with this screen, its last user.
  - Neither Combobox nor Select needed a jsdom stand-in.
  - Rebasing onto the Dex change had dropped `scripts/drive-web.mjs`'s phone
    emulation and its `--signed-out` check: `--width` emulated nothing and
    `--signed-out` signed in anyway. PR 5 restored both. To type into a
    Base UI input from `--eval`, use `document.execCommand("insertText", …)`;
    setting `value` and dispatching `input` doesn't open the list.
  ```

- **Testing, new behaviour tests:** replace the PR 5 Combobox bullet with `- PR 5, Combobox: the input has \`role="combobox"\` and \`aria-expanded\`; typing searches, and the options (\`role="option"\`) are exactly the server's answer; ArrowDown and ArrowUp move the highlight; Enter selects; Escape closes and clears.`

- [ ] **Step 8: Full verification**

Run from `web/`:

```bash
npx tsc -b
npm test
npm run build
```

Expected: `tsc` reports no errors, every test passes (734 in 56 files, up from 719), and the build succeeds. Note the test count, and the CSS and JS sizes the build prints, for the PR description. No dependency changed, so `docker compose build web` isn't needed.

- [ ] **Step 9: Commit**

From the repository root:

```bash
git add docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md
git commit -m "docs: record what PR 5 of the shadcn migration settled" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 10: Push and open the PR against `main`**

Write `$AUDIT/pr-body.md` first. It must cover:
- that this PR implements PR 5 of the spec, with links to the spec and this plan;
- what moved:
  - the user search onto Combobox, with arrow keys, Enter, Escape and ARIA roles;
  - the role picker onto Select;
  - both panels onto Card, the grants onto a list with `RoleBadge`, errors onto Alert, and the undo banner onto a Tailwind toast;
- the three decisions, each with its reason:
  - the selected-user card goes;
  - typing keeps the pick;
  - the toast is not Sonner;
- that InputGroup and Textarea were vendored as Combobox's dependencies, and that the CLI's offer to overwrite `button.tsx` was declined;
- what was deleted:
  - the access screen's grants, search, undo-banner and `form-row` CSS;
  - the last `role-badge` rules;
  - `.alert .secondary-button` and `.confirm-label`;
  - the stale `.role-badge` allowlist entry;
- the `drive-web.mjs` fix: what the rebase dropped, and that phone-width audits run after that rebase measured a desktop pointer;
- configuration impact: none. No new dependencies, no migrations;
- validation:
  - the commands above, with the test count;
  - the audit's `CLEAN` result;
  - the 32px field heights and 16px headings;
  - the phone-width probes with `small: []`;
- a `## Screenshots` heading listing the before/after pairs in `$AUDIT/shots/`, desktop and 375px. `gh` can't upload images, so tell the user to drag them into the PR description;
- a final line: `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

Then, from the repository root:

```bash
export AUDIT=<this session's scratchpad>/audit5
gh auth status
git push -u origin feat/shadcn-stack-access
gh pr create --base main --head feat/shadcn-stack-access \
  --title "feat(web): stack access on shadcn" --body-file "$AUDIT/pr-body.md"
```

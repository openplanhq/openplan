# Standalone Screens on shadcn (PR 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the screens that stand alone (sign-in, not found, access denied, service unavailable, the route placeholder, and SessionProvider's two session-error screens) onto shadcn components and Tailwind utilities. Delete the legacy CSS they stop using.

**Architecture:** The shadcn CLI vendors Card, Alert, Input, Label and Empty. The four route messages become one `RouteMessage` component on Empty. Each screen file keeps its name and test ID, and passes in its own icon and copy. Sign-in and the session errors share `AuthCard`, a Card centred on the viewport. The legacy `label`/`input`/`textarea`/`select` rules in `primitives.css` are scoped away from `[data-slot]` elements, so Input and Label render as shadcn ships them. `scripts/drive-web.mjs` learns to fail chosen requests, so the audit can reach the 403, 503 and session-error screens.

**Tech Stack:** React 19, Tailwind CSS 4.3, shadcn CLI 4.21 (`base-nova` style on Base UI 1.8), `class-variance-authority`, `cn`, lucide-react, Vitest 4 with Testing Library and `@testing-library/user-event`.

**Spec:** `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`. Read these sections before starting: "Theme", "Running old and new styles side by side" (the layer order, the Preflight audit, and the known `base.css` leak), "What PR 2 settled", "What every screen PR must do" and "Guards".

## Global Constraints

- Branch `feat/shadcn-standalone-screens`, created from `feat/shadcn-shell-shared` (PR #276, stacked on #274 and #273). The PR base is `feat/shadcn-shell-shared`.
- All commands run from `web/` unless a step says otherwise.
- Screens keep their structure, copy and behaviour, word for word. Only the look changes. The route screens lose `HeroGraphic` and the entrance reveal, and gain an Empty with an icon. Sign-in and the session errors become a Card centred on the viewport.
- Test IDs don't change, because other test files depend on them: `route-not-found`, `route-access-denied`, `route-service-unavailable`, `route-placeholder`, `signin-error`, `signin-sso`, `signin-cookies-blocked`, `signin-cookies-retry`, `auth-error`, `auth-retry-button`, `auth-loop-error`, `auth-loop-retry-button`.
- The field IDs `signin-username` and `signin-password` don't change: `scripts/drive-web.mjs` signs in through them.
- Components are added only with `npx shadcn add`. PR 3 doesn't edit any vendored file in `src/components/ui/`.
- App code uses theme colours and Tailwind's scale steps only: no arbitrary values (`w-[37px]`) and no built-in palette colours (`bg-blue-500`). `src/styles/tailwind.guard.test.ts` fails otherwise.
- `base.css` still styles bare `h1` from the `legacy` layer until PR 9, and a rule on an element beats anything it would inherit. So every migrated `h1` sets its own font family, size, weight and tracking. `body` also keeps the legacy text colour, so each migrated screen's outermost element sets `text-foreground`.
- Migrated controls keep the 44px target on coarse pointers that `--legacy-touch-target` gave them: `pointer-coarse:h-11` on every Button and Input.
- Each task deletes the legacy rules it stopped using. The dead-CSS guard (`src/styles/legacy.guard.test.ts`) catches class selectors. It can't catch element selectors, so Task 4 deletes the bare `h1` in the phone-width rule by hand.
- Tests find elements by role, text, label, test ID or `data-slot`, never by legacy class name. Tests that click Base UI components use `@testing-library/user-event`.
- No `src/test/setup.ts` unless a test fails on a missing browser API. Card, Alert, Label and Empty render plain elements. Base UI's Input is a thin `<input>` wrapper.
- Commit subjects are lowercase-prefixed (`feat(web):`, `refactor(web):`, `test(web):`, `chore:`, `docs:`). Every commit message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Someone fills in a legacy form after the field rules are scoped** (create stack, add a credential, register a template). Expected: every field looks exactly as before, because the scoping reaches only `[data-slot]` elements. Task 1 guards the selectors. Task 6 diffs the computed styles of the eight screens that have forms.
2. **A password manager or the browser's autofill fills in the sign-in form.** Expected: it finds the fields as it does today, because `name`, `autocomplete`, `type` and `required` survive the move to Base UI's Input. Task 3 tests all four.
3. **A keyboard user signs in on a slow network and presses Enter twice.** Expected: Enter in the password field submits, the button disables while the request is out, and the second Enter doesn't sign in again. Task 3 tests both.
4. **A screen-reader user mistypes their password.** Expected: the error is announced once. Alert sets `role="alert"` itself, so the screen must not add a second one. Task 3 checks that exactly one alert is present.
5. **The sign-in card, a session error or a route message on a 375px touch screen.** Expected: no sideways scroll, and a 44px target for every control. Tasks 3 and 4 pin `pointer-coarse:h-11`. Task 6 measures `scrollWidth` at phone width.

---

### Setup: branch and plan commit

- [ ] **Step 1: Create the branch and commit this plan**

From the repository root:

```bash
git switch feat/shadcn-shell-shared
git pull --ff-only
git switch -c feat/shadcn-standalone-screens
git add docs/superpowers/plans/2026-09-30-shadcn-standalone-screens.md
git commit -m "docs: implementation plan for the standalone screens (PR 3)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 1: Vendor Card, Alert, Input, Label and Empty; keep the legacy field rules off them

The spec's migration table puts Input and Label in PR 4. The sign-in form needs them, so they arrive here. Empty is shadcn's stock empty state, and it replaces `HeroGraphic` on the route screens. PR 4 and PR 7 reuse it for the stacks and registry empty states.

`primitives.css` styles the bare `label`, `input`, `textarea` and `select` elements. A rule on an element beats what shadcn leaves to inheritance or to its own height, so on a shadcn field these legacy rules leak through. The label would take the legacy muted colour, and the legacy `min-height: 36px` would outgrow Input's `h-8`. Scoping each selector with `:where(:not([data-slot]))` keeps the rules off shadcn fields, which all carry `data-slot`. `:where()` adds no specificity, so legacy fields match exactly as before.

**Files:**
- Modify: `web/src/styles/primitives.css:112-165`
- Test: `web/src/styles/legacy.guard.test.ts`
- Create (by the CLI): `web/src/components/ui/card.tsx`, `alert.tsx`, `input.tsx`, `label.tsx`, `empty.tsx`
- Modify: `web/src/dev/StyleGuide.tsx`
- Test: `web/src/dev/StyleGuide.test.tsx`

**Interfaces:**
- Consumes: the theme colours in `src/styles/theme.css`.
- Produces (all from the CLI, unedited):
  - `import { Card, CardHeader, CardTitle, CardDescription, CardAction, CardContent, CardFooter } from "@/components/ui/card"`. Each part is a `<div>` with a matching `data-slot` (`card`, `card-header`, …). `Card` takes `size?: "default" | "sm"`. `CardTitle` is a `div`, not a heading.
  - `import { Alert, AlertTitle, AlertDescription, AlertAction } from "@/components/ui/alert"`. `Alert` is a `<div data-slot="alert" role="alert">` with `variant?: "default" | "destructive"`. A leading `<svg>` child takes the first grid column.
  - `import { Input } from "@/components/ui/input"`. Base UI's Input, rendering `<input data-slot="input">`, `h-8`. It accepts every `<input>` prop.
  - `import { Label } from "@/components/ui/label"`. A plain `<label data-slot="label">`.
  - `import { Empty, EmptyHeader, EmptyMedia, EmptyTitle, EmptyDescription, EmptyContent } from "@/components/ui/empty"`. `<div data-slot="empty">`, centred and `p-6`. `EmptyMedia` takes `variant?: "default" | "icon"`; `icon` draws a `size-8` muted tile. `EmptyTitle` is a `div`.

- [ ] **Step 1: Write the failing guard test**

Append to `web/src/styles/legacy.guard.test.ts`:

```ts
describe("legacy field rules", () => {
  // shadcn's Label, Input and Textarea carry data-slot. A legacy rule on the
  // element would beat what they leave to inheritance or to their height: the
  // label would turn muted, and Input's h-8 would grow to the legacy 36px
  // min-height. :where() adds no specificity, so legacy fields still match
  // exactly as they did.
  it("skip elements that carry data-slot", () => {
    const unscoped = [...readSheet("styles/primitives.css").matchAll(/([^{}]+)\{/g)]
      .flatMap(([, selector]) => selector.split(","))
      .map((selector) => selector.trim())
      .filter((selector) => /^(label|input|textarea|select)(?![\w-])/.test(selector))
      .filter((selector) => !/^(label|input|textarea|select):where\(:not\(\[data-slot\]\)\)/.test(selector));
    expect(unscoped, `scope with :where(:not([data-slot])): ${unscoped.join(", ")}`).toEqual([]);
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `skip elements that carry data-slot`, with `scope with :where(:not([data-slot])): label, input, textarea, select, input::placeholder, textarea::placeholder, input:focus, textarea:focus, select:focus, textarea`.

- [ ] **Step 3: Scope the field rules**

In `web/src/styles/primitives.css`, replace the block from `/* ---- Inputs ---- */` down to the end of the `textarea { … resize: vertical; }` rule with this. Every declaration and comment inside the rules stays as it is; only the selectors and the new comment change:

```css
/* ---- Inputs ---- */
/* Every selector here skips [data-slot]: shadcn's Label, Input and Textarea
   carry it, and these element rules would otherwise beat what they leave to
   inheritance or to their own height (a muted label, a 36px Input). :where()
   adds no specificity, so legacy fields match exactly as before.
   legacy.guard.test.ts checks every selector. */
label:where(:not([data-slot])) {
  display: grid;
  gap: var(--legacy-space-2);
  color: var(--legacy-color-muted-fg);
  font-family: var(--legacy-font-body);
  font-size: var(--legacy-text-sm);
  font-weight: 400;
}

input:where(:not([data-slot])),
textarea:where(:not([data-slot])),
select:where(:not([data-slot])) {
  /* Fields are block-level form controls and should fill their container.
     Without this they fall back to an intrinsic inline-block width — roughly
     200px for text inputs and content-width for selects — which only looks
     right where the field happens to be wrapped in a `label { display: grid }`
     and inherits its stretch. A field placed as a sibling of its label instead
     stays stubbornly narrow. */
  width: 100%;
  min-height: var(--legacy-control-height);
  min-width: 0;
  border: 1px solid var(--legacy-color-border);
  border-radius: var(--legacy-radius-lg);
  /* Zero vertical padding so --legacy-control-height governs; see the button rule. */
  padding: 0 var(--legacy-space-4);
  color: var(--legacy-color-fg);
  background: var(--legacy-color-card);
  font-family: var(--legacy-font-body);
  font-size: var(--legacy-text-sm);
  transition: border-color var(--legacy-duration-fast) var(--legacy-ease-out),
              box-shadow var(--legacy-duration-fast) var(--legacy-ease-out);
}

input:where(:not([data-slot]))::placeholder,
textarea:where(:not([data-slot]))::placeholder {
  color: var(--legacy-color-muted-fg);
}

input:where(:not([data-slot])):focus,
textarea:where(:not([data-slot])):focus,
select:where(:not([data-slot])):focus {
  border-color: var(--legacy-color-accent);
  box-shadow: var(--legacy-shadow-ring);
  outline: none;
}

textarea:where(:not([data-slot])) {
  min-height: var(--legacy-space-24);
  /* Multi-line, so it opts back into vertical padding: unlike a single-line
     input it has no centred line box to sit in, and text would touch the edge. */
  padding: var(--legacy-space-3) var(--legacy-space-4);
  resize: vertical;
}
```

- [ ] **Step 4: Run the style guards**

Run: `npx vitest run src/styles`
Expected: all pass. `styles.guard.test.ts` reads declarations, not selectors, so it is unaffected.

- [ ] **Step 5: Commit the scoping**

```bash
git add src/styles/primitives.css src/styles/legacy.guard.test.ts
git commit -m "refactor(web): keep the legacy field rules off shadcn fields" -m "primitives.css styles bare label, input, textarea and select. On a shadcn field those rules beat what the component leaves to inheritance: the label turned muted and Input's h-8 grew to the legacy 36px min-height. Each selector now skips [data-slot], inside :where() so legacy fields keep their specificity." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 6: Vendor the components**

Run: `npx shadcn add card alert input label empty -y`
Then run: `git status --short`
Expected: exactly five new files under `src/components/ui/` (`card.tsx`, `alert.tsx`, `input.tsx`, `label.tsx`, `empty.tsx`). `package.json`, the lockfile, `theme.css` and `components.json` are unchanged: `cn` is already a dependency, and none of these components adds theme variables. If anything else changed, inspect it with `git diff` before going on, and revert what the CLI rewrote without need.

- [ ] **Step 7: Write the failing styleguide test**

Append inside the `describe("StyleGuide", …)` block of `web/src/dev/StyleGuide.test.tsx`:

```tsx
  it("shows the components PR 3 added", () => {
    render(<StyleGuide />);
    const section = screen.getByTestId("sg-theme");
    for (const slot of ["card", "alert", "input", "label", "empty"]) {
      expect(section.querySelector(`[data-slot="${slot}"]`), `missing ${slot}`).toBeTruthy();
    }
    // The Label is wired to its Input, as every form on the app must be.
    expect(within(section).getByLabelText("Stack name").getAttribute("data-slot")).toBe("input");
  });
```

- [ ] **Step 8: Run it to see it fail**

Run: `npx vitest run src/dev/StyleGuide.test.tsx`
Expected: FAIL on `shows the components PR 3 added` with `missing card`.

- [ ] **Step 9: Add the specimens**

In `web/src/dev/StyleGuide.tsx`, change the lucide import to:

```tsx
import { CircleAlert, Info, Plus, SearchX } from "lucide-react";
```

and add, next to the other `@/components/ui` imports:

```tsx
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
```

Then, inside the `theme` Section, after the `LogSteps` specimen and before `</Section>`, add:

```tsx
            <Specimen label="Card" stack>
              <Card className="w-full max-w-sm">
                <CardHeader>
                  <CardTitle>payments-core</CardTitle>
                  <CardDescription>3 templates · last run 21 Sept</CardDescription>
                </CardHeader>
                <CardContent>
                  <p className="text-muted-foreground">One subject per card: a sign-in, a summary, a form.</p>
                </CardContent>
              </Card>
            </Specimen>
            <Specimen label="Alert" stack>
              <div className="grid w-full max-w-md gap-3">
                <Alert>
                  <Info />
                  <AlertTitle>Plan queued</AlertTitle>
                  <AlertDescription>It starts when a worker is free.</AlertDescription>
                </Alert>
                <Alert variant="destructive">
                  <CircleAlert />
                  <AlertTitle>Incorrect username or password.</AlertTitle>
                </Alert>
              </div>
            </Specimen>
            <Specimen label="Input and Label" stack>
              <div className="grid w-full max-w-sm gap-4">
                <div className="grid gap-2">
                  <Label htmlFor="sg-input">Stack name</Label>
                  <Input id="sg-input" placeholder="payments-core" />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="sg-input-invalid">Invalid</Label>
                  <Input id="sg-input-invalid" defaultValue="Payments Core" aria-invalid />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="sg-input-disabled">Disabled</Label>
                  <Input id="sg-input-disabled" defaultValue="payments-core" disabled />
                </div>
              </div>
            </Specimen>
            <Specimen label="Empty" stack>
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <SearchX />
                  </EmptyMedia>
                  <EmptyTitle>No stacks yet</EmptyTitle>
                  <EmptyDescription>Stacks you can see appear here.</EmptyDescription>
                </EmptyHeader>
              </Empty>
            </Specimen>
```

- [ ] **Step 10: Run the styleguide test, the guards and the type-check**

Run: `npx vitest run src/dev src/styles && npx tsc -b`
Expected: all pass; `tsc` prints nothing.

- [ ] **Step 11: Commit**

```bash
git add src/components/ui src/dev/StyleGuide.tsx src/dev/StyleGuide.test.tsx
git commit -m "feat(web): add the shadcn Card, Alert, Input, Label and Empty" -m "Input and Label arrive a PR early because the sign-in form needs them. Empty replaces HeroGraphic on the route screens. Each one gets a /styleguide specimen." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The route messages on Empty

`NotFound`, `AccessDenied`, `ServiceUnavailable` and `RoutePlaceholder` are the same screen with different copy: a title, one sentence, and `HeroGraphic`. They become one `RouteMessage` on Empty. Each file keeps its name, export, copy and test ID, because `router.tsx`, `RequireCapability.tsx`, `queryErrorBoundary.tsx` and `TemplateDetailScreen.tsx` render them, and eight test files look for their test IDs.

The title stays a real `h1`, as it is today, because `EmptyTitle` renders a `div`. It is `text-lg`: the size shadcn's `new-york` Empty gives its title. `base-nova`'s `text-sm` is sized for an empty panel, not a whole page.

`HeroGraphic`, `useInView` and the `showcase`, `reveal` and `gradient-text` CSS stay, because the stacks and registry empty states and `/styleguide` still use them. This task deletes no CSS; Step 8 confirms it.

**Files:**
- Create: `web/src/app/RouteMessage.tsx`
- Test: `web/src/app/RouteMessage.test.tsx`
- Modify: `web/src/app/NotFound.tsx`, `AccessDenied.tsx`, `ServiceUnavailable.tsx`, `RoutePlaceholder.tsx`
- Test: `web/src/app/NotFound.test.tsx`, `AccessDenied.test.tsx`, `ServiceUnavailable.test.tsx`, `RoutePlaceholder.test.tsx`

**Interfaces:**
- Consumes: `Empty`, `EmptyHeader`, `EmptyMedia`, `EmptyDescription` from Task 1.
- Produces: `export default function RouteMessage(props: { icon: LucideIcon; title: string; description: string; testId: string })`. It renders `<div data-slot="empty" data-testid={testId}>` holding an icon tile, an `h1` with `title`, and the description.

- [ ] **Step 1: Write the failing test**

Create `web/src/app/RouteMessage.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { SearchX } from "lucide-react";
import { afterEach, describe, expect, it } from "vitest";
import RouteMessage from "./RouteMessage";

afterEach(cleanup);

function renderMessage() {
  return render(
    <RouteMessage
      icon={SearchX}
      title="Page not found"
      description="The page you were looking for doesn't exist."
      testId="route-not-found"
    />
  );
}

describe("RouteMessage", () => {
  it("renders the title as its one heading, an h1", () => {
    renderMessage();
    const headings = screen.getAllByRole("heading");
    expect(headings).toHaveLength(1);
    expect(headings[0].tagName).toBe("H1");
    expect(headings[0].textContent).toBe("Page not found");
  });

  it("explains the title below it", () => {
    renderMessage();
    expect(screen.getByText("The page you were looking for doesn't exist.")).toBeTruthy();
  });

  it("is shadcn's Empty, carrying the screen's test id", () => {
    renderMessage();
    expect(screen.getByTestId("route-not-found").getAttribute("data-slot")).toBe("empty");
  });

  it("hides the icon from assistive technology", () => {
    const { container } = renderMessage();
    expect(container.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });

  // Until PR 9, base.css gives every h1 the legacy 40px display type, and body
  // the legacy text colour. A rule on the element beats anything inherited, so
  // the h1 sets its own type and the message sets its own colour.
  it("sets its own type and colour", () => {
    renderMessage();
    const heading = screen.getByRole("heading", { level: 1 }).classList;
    for (const name of ["font-heading", "text-lg", "font-medium", "tracking-tight"]) {
      expect(heading).toContain(name);
    }
    expect(screen.getByTestId("route-not-found").classList).toContain("text-foreground");
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npx vitest run src/app/RouteMessage.test.tsx`
Expected: FAIL, `Failed to resolve import "./RouteMessage"`.

- [ ] **Step 3: Write `RouteMessage`**

Create `web/src/app/RouteMessage.tsx`:

```tsx
import type { LucideIcon } from "lucide-react";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";

/**
 * A page that is only a message: not found, not permitted, a service that is
 * down, a screen still to come. It renders inside the app shell, in place of
 * the screen that couldn't render.
 *
 * shadcn's Empty with a real h1: the message is the page's heading, and
 * EmptyTitle renders a div. Until PR 9, base.css styles every h1 from the
 * legacy layer, so the h1 sets its own family, size, weight and tracking; and
 * body keeps the legacy text colour, so Empty sets its own.
 */
export default function RouteMessage({
  icon: Icon,
  title,
  description,
  testId
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  testId: string;
}) {
  return (
    <Empty className="py-16 text-foreground md:py-24" data-testid={testId}>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon aria-hidden="true" />
        </EmptyMedia>
        <h1 className="font-heading text-lg font-medium tracking-tight">{title}</h1>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `npx vitest run src/app/RouteMessage.test.tsx`
Expected: PASS, 5 tests.

- [ ] **Step 5: Pin the four screens to Empty**

In each of the four screen tests, add one assertion after the existing `expect(markup).toContain('data-testid="…"')` line:

```tsx
    expect(markup).toContain('data-slot="empty"');
```

The four files are `src/app/NotFound.test.tsx`, `AccessDenied.test.tsx`, `ServiceUnavailable.test.tsx` and `RoutePlaceholder.test.tsx`.

Run: `npx vitest run src/app/NotFound.test.tsx src/app/AccessDenied.test.tsx src/app/ServiceUnavailable.test.tsx src/app/RoutePlaceholder.test.tsx`
Expected: FAIL in all four on the new assertion.

- [ ] **Step 6: Move the four screens onto `RouteMessage`**

Replace `web/src/app/NotFound.tsx` with:

```tsx
import { SearchX } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function NotFound() {
  return (
    <RouteMessage
      icon={SearchX}
      title="Page not found"
      description="The page you were looking for doesn't exist."
      testId="route-not-found"
    />
  );
}
```

Replace `web/src/app/AccessDenied.tsx` with:

```tsx
import { LockKeyhole } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function AccessDenied() {
  return (
    <RouteMessage
      icon={LockKeyhole}
      title="Not permitted"
      description="You don't have permission to do this."
      testId="route-access-denied"
    />
  );
}
```

Replace `web/src/app/ServiceUnavailable.tsx` with:

```tsx
import { ServerCrash } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function ServiceUnavailable() {
  return (
    <RouteMessage
      icon={ServerCrash}
      title="Authorization service unavailable"
      description="Authorization service unavailable — try again shortly."
      testId="route-service-unavailable"
    />
  );
}
```

Replace `web/src/app/RoutePlaceholder.tsx` with:

```tsx
import { Construction } from "lucide-react";
import RouteMessage from "./RouteMessage";

export default function RoutePlaceholder({ title }: { title: string }) {
  return (
    <RouteMessage
      icon={Construction}
      title={title}
      description="This screen has not been built yet."
      testId="route-placeholder"
    />
  );
}
```

- [ ] **Step 7: Run every test that renders them**

Run: `npx vitest run src/app src/auth/RequireCapability.test.tsx src/shared/queryErrorBoundary.test.tsx src/features`
Expected: all pass. The feature tests find these screens by test ID, which hasn't changed.

- [ ] **Step 8: Confirm no CSS went dead, and type-check**

Run: `npx vitest run src/styles && npx tsc -b`
Expected: all pass; `tsc` prints nothing. The `showcase`, `reveal`, `gradient-text` and `hero-graphic` rules still have users (`StacksListScreen`, `TemplateRegistryScreen`, `/styleguide`), so the dead-CSS guard stays green.

- [ ] **Step 9: Commit**

```bash
git add src/app
git commit -m "feat(web): route messages on the shadcn Empty" -m "Not found, access denied, service unavailable and the route placeholder were one screen with four sets of copy. They now share RouteMessage: Empty with an icon and a real h1, in place of HeroGraphic and the entrance reveal. Copy and test IDs are unchanged." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Sign-in on Card, Alert, Input, Label and Button

**Files:**
- Create: `web/src/auth/AuthCard.tsx`
- Test: `web/src/auth/AuthCard.test.tsx`
- Modify: `web/src/auth/SignInScreen.tsx` (imports and the two `return` blocks; everything above `if (cookiesBlocked)` stays)
- Test: `web/src/auth/SignInScreen.test.tsx`
- Test: `web/src/app/router.test.tsx:353`
- Modify: `web/src/styles/features.css` (the `/* ---- Sign in ---- */` block)

**Interfaces:**
- Consumes: `Card`, `CardHeader`, `CardContent` (Task 1); `Alert`, `AlertTitle` (Task 1); `Input`, `Label` (Task 1); `Button` from `@/components/ui/button` (PR 1).
- Produces, in `src/auth/AuthCard.tsx`:
  - `export default function AuthCard(props: ComponentProps<typeof Card>)`. It renders `<main>` filling the viewport, with the `Card` centred inside, and passes every prop, `data-testid` included, to the `Card`.
  - `export function AuthCardTitle(props: ComponentProps<"h1">)`. An `h1` in CardTitle's look.
  
  Task 4 uses both.

- [ ] **Step 1: Write the failing `AuthCard` test**

Create `web/src/auth/AuthCard.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import AuthCard, { AuthCardTitle } from "./AuthCard";

afterEach(cleanup);

describe("AuthCard", () => {
  it("is the page's main landmark, with the card inside it", () => {
    render(<AuthCard data-testid="frame-card">body</AuthCard>);
    const card = screen.getByTestId("frame-card");
    expect(card.getAttribute("data-slot")).toBe("card");
    expect(screen.getByRole("main").contains(card)).toBe(true);
  });

  // body keeps the legacy colours until PR 9, so the frame paints its own.
  it("sets its own background and text colour", () => {
    render(<AuthCard>body</AuthCard>);
    const classes = screen.getByRole("main").classList;
    expect(classes).toContain("bg-background");
    expect(classes).toContain("text-foreground");
  });
});

describe("AuthCardTitle", () => {
  // base.css gives every h1 the legacy 40px display type until PR 9.
  it("is a level-one heading that sets its own type", () => {
    render(<AuthCardTitle>Sign in</AuthCardTitle>);
    const heading = screen.getByRole("heading", { level: 1, name: "Sign in" });
    for (const name of ["font-heading", "text-base", "font-medium", "tracking-normal"]) {
      expect(heading.classList).toContain(name);
    }
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npx vitest run src/auth/AuthCard.test.tsx`
Expected: FAIL, `Failed to resolve import "./AuthCard"`.

- [ ] **Step 3: Write `AuthCard`**

Create `web/src/auth/AuthCard.tsx`:

```tsx
import type { ComponentProps } from "react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

/**
 * The page frame for the screens outside the app shell: sign-in, and the
 * session errors SessionProvider shows before there is a shell to show. A card
 * centred on the viewport, which marks them as a threshold rather than a place
 * inside the product.
 *
 * Until PR 9, body keeps the legacy colours (see base.css), so the frame sets
 * its own background and text colour.
 */
export default function AuthCard({ className, ...props }: ComponentProps<typeof Card>) {
  return (
    <main className="grid min-h-svh place-items-center bg-background p-6 text-foreground">
      <Card className={cn("w-full max-w-sm", className)} {...props} />
    </main>
  );
}

/**
 * CardTitle's look on a real h1, since CardTitle renders a div. Until PR 9,
 * base.css gives every h1 the legacy 40px display type, and a rule on the
 * element beats anything it would inherit, so this sets its own family, size,
 * weight and tracking.
 */
export function AuthCardTitle({ className, ...props }: ComponentProps<"h1">) {
  return (
    <h1 className={cn("font-heading text-base leading-snug font-medium tracking-normal", className)} {...props} />
  );
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `npx vitest run src/auth/AuthCard.test.tsx`
Expected: PASS, 3 tests.

- [ ] **Step 5: Add the sign-in tests**

In `web/src/auth/SignInScreen.test.tsx`, add these tests inside `describe("SignInScreen", …)`:

```tsx
  // Password managers and browser autofill find the fields by these
  // attributes. Base UI's Input must pass every one of them through.
  it("keeps the attributes autofill relies on", () => {
    renderSignIn();
    const username = screen.getByLabelText<HTMLInputElement>("Username");
    const password = screen.getByLabelText<HTMLInputElement>("Password");
    expect(username.name).toBe("username");
    expect(username.getAttribute("autocomplete")).toBe("username");
    expect(username.required).toBe(true);
    expect(password.name).toBe("password");
    expect(password.type).toBe("password");
    expect(password.getAttribute("autocomplete")).toBe("current-password");
    expect(password.required).toBe(true);
  });

  it("puts the cursor in the username field", () => {
    renderSignIn();
    expect(document.activeElement).toBe(screen.getByLabelText("Username"));
  });

  it("submits from the keyboard", async () => {
    renderSignIn();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("Username"), "root");
    await user.type(screen.getByLabelText("Password"), "hunter2{Enter}");

    await waitFor(() => {
      expect(signInMock).toHaveBeenCalledWith("root", "hunter2");
    });
  });

  // A slow answer must not turn a second Enter into a second sign-in.
  it("disables the submit button while signing in", async () => {
    signInMock.mockReturnValue(new Promise(() => {}));
    renderSignIn();
    const user = await signIn();

    expect(screen.getByRole<HTMLButtonElement>("button", { name: /sign in/i }).disabled).toBe(true);
    await user.type(screen.getByLabelText("Password"), "{Enter}");
    expect(signInMock).toHaveBeenCalledTimes(1);
  });

  // role="alert" is how a screen reader hears the error. Alert sets it, so
  // the screen must not add a second one, or the error is read twice.
  it("announces a rejected sign-in once", async () => {
    signInMock.mockRejectedValue(new ApiRequestError(401, "unauthorized", "authentication failed"));
    renderSignIn();
    await signIn();

    await screen.findByTestId("signin-error");
    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(1);
    expect(alerts[0].textContent).toContain("Incorrect username or password");
  });

  // --legacy-touch-target gave every control 44px on a touch screen.
  it("gives every control a 44px target on coarse pointers", async () => {
    authMethodsMock.mockResolvedValue({ local: true, oidc: true });
    renderSignIn();
    await screen.findByTestId("signin-sso");

    const controls = [
      screen.getByLabelText("Username"),
      screen.getByLabelText("Password"),
      screen.getByRole("button", { name: /sign in/i }),
      screen.getByTestId("signin-sso")
    ];
    for (const control of controls) {
      expect(control.classList).toContain("pointer-coarse:h-11");
    }
  });
```

Also extend the existing test `explains a blocked cookie once the attempts are spent`. Add these lines after its last `expect`:

```tsx
    expect(screen.getByRole("heading", { level: 1, name: "We could not keep you signed in" })).toBeTruthy();
    expect(screen.getByTestId("signin-cookies-retry").classList).toContain("pointer-coarse:h-11");
```

- [ ] **Step 6: Run them and read the result carefully**

Run: `npx vitest run src/auth/SignInScreen.test.tsx`
Expected: FAIL in exactly two tests: `gives every control a 44px target on coarse pointers` and `explains a blocked cookie once the attempts are spent`, both on the `pointer-coarse:h-11` assertion. The other new tests already pass against today's markup. That is on purpose: they pin behaviour the move to Base UI must keep. If any of them fails now, stop and find out why before going on.

- [ ] **Step 7: Rewrite the sign-in markup**

In `web/src/auth/SignInScreen.tsx`, replace the lucide import line with:

```tsx
import { CircleAlert, KeyRound, Loader2, LogIn } from "lucide-react";
```

and add after the `./loginAttempts` import:

```tsx
import AuthCard, { AuthCardTitle } from "./AuthCard";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CardContent, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
```

Replace everything from `if (cookiesBlocked) {` to the end of the file with:

```tsx
  if (cookiesBlocked) {
    return (
      <AuthCard data-testid="signin-cookies-blocked">
        <CardHeader>
          <AuthCardTitle>We could not keep you signed in</AuthCardTitle>
        </CardHeader>
        <CardContent className="grid gap-4">
          <p className="text-muted-foreground">
            Your username and password were accepted, but this browser did not hold on to the
            session, so every page load started it over. That happens when cookies are blocked for
            this site — by browser settings, an extension, or a privacy mode that clears them
            between page loads.
          </p>
          <p className="text-muted-foreground">
            Allow cookies for this site and try again. If it keeps failing, contact your
            administrator.
          </p>
          <Button className="w-full pointer-coarse:h-11" onClick={handleRetry} data-testid="signin-cookies-retry">
            Try again
          </Button>
        </CardContent>
      </AuthCard>
    );
  }

  return (
    <AuthCard>
      <CardHeader>
        <p className="font-mono text-xs tracking-wide text-muted-foreground uppercase">openplan</p>
        <AuthCardTitle>Sign in</AuthCardTitle>
      </CardHeader>

      <CardContent className="grid gap-4">
        {error && (
          <Alert variant="destructive" data-testid="signin-error">
            <CircleAlert />
            <AlertTitle>{error}</AlertTitle>
          </Alert>
        )}

        <form className="grid gap-4" onSubmit={handleSubmit}>
          <div className="grid gap-2">
            <Label htmlFor="signin-username">Username</Label>
            <Input
              id="signin-username"
              name="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
              autoComplete="username"
              autoFocus
              required
              className="pointer-coarse:h-11"
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="signin-password">Password</Label>
            <Input
              id="signin-password"
              name="password"
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="current-password"
              required
              className="pointer-coarse:h-11"
            />
          </div>
          {/* Full width, like the fields above it: a content-width button
              under full-width fields reads as an afterthought rather than the
              action the screen exists for. */}
          <Button type="submit" className="w-full pointer-coarse:h-11" disabled={busy}>
            {busy ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <LogIn data-icon="inline-start" />}
            Sign in
          </Button>
        </form>

        {methods?.oidc && (
          <>
            {/* A rule with the word set into it. The pseudo-elements are the
                two halves of the line, so the gap tracks the word's width. */}
            <p className="flex items-center gap-3 text-xs tracking-wide text-muted-foreground uppercase before:h-px before:flex-1 before:bg-border after:h-px after:flex-1 after:bg-border">
              or
            </p>
            <Button
              variant="outline"
              className="w-full pointer-coarse:h-11"
              onClick={handleSSO}
              data-testid="signin-sso"
            >
              <KeyRound data-icon="inline-start" />
              Continue with single sign-on
            </Button>
          </>
        )}
      </CardContent>
    </AuthCard>
  );
}
```

`before:` and `after:` utilities add `content: ""` on their own in Tailwind 4, so the divider needs no arbitrary `content-[…]`.

- [ ] **Step 8: Update the router test that matched the old page class**

In `web/src/app/router.test.tsx`, in `renders the sign-in screen at /signin outside the session boundary`, replace:

```tsx
    expect(markup).toContain('class="signin-page"');
```

with:

```tsx
    expect(markup).toContain('id="signin-username"');
```

- [ ] **Step 9: Run the auth and router tests**

Run: `npx vitest run src/auth src/app/router.test.tsx`
Expected: all pass.

- [ ] **Step 10: Run the dead-CSS guard to see what is now unused**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `styles/features.css styles only classes a component uses`, with `delete the rules for: signin-page, signin-card, signin-header, signin-wordmark, signin-title, signin-lede, signin-form, signin-submit, signin-divider`.

- [ ] **Step 11: Delete the sign-in rules**

In `web/src/styles/features.css`, delete the whole sign-in block. It runs from the `/* ---- Sign in ---- */` comment through the `.signin-card > .signin-divider + .secondary-button { margin-top: 0; }` rule and its comment, and ends right before `/* ---- Session errors ---- */`.

- [ ] **Step 12: Run the guards, the whole suite and the type-check**

Run: `npx vitest run src/styles && npm test && npx tsc -b`
Expected: all pass; `tsc` prints nothing.

- [ ] **Step 13: Commit**

```bash
git add src/auth src/app/router.test.tsx src/styles/features.css
git commit -m "feat(web): sign-in on the shadcn Card, Input and Alert" -m "The sign-in form and its cookies-blocked screen sit in AuthCard, a Card centred on the viewport, with Label and Input fields, an Alert for the error and shadcn Buttons. New tests pin what the move to Base UI must keep: autofill attributes, focus on arrival, Enter to submit, no double submit, one announced error, and 44px touch targets. The sign-in CSS goes." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The session-error screens on `AuthCard`

SessionProvider shows two screens before any shell exists. `auth-loop-error` appears when sign-in keeps succeeding but the cookie never sticks. `auth-error` appears when `/v1/me` fails for a reason other than a 401. Both are bare markup today, and they rely on the Preflight restorations in `features.css`, which PR 9 deletes. No PR in the spec's table names them, so they migrate here, beside the sign-in screen they mirror.

**Files:**
- Modify: `web/src/auth/SessionProvider.tsx:181-213`
- Test: `web/src/auth/SessionProvider.test.tsx`
- Modify: `web/src/styles/features.css` (the `/* ---- Session errors ---- */` block, and the phone-width `h1` rule)

**Interfaces:**
- Consumes: `AuthCard` and `AuthCardTitle` (Task 3); `CardHeader` and `CardContent` (Task 1); `Button`.
- Produces: nothing new. The test IDs `auth-loop-error`, `auth-loop-retry-button`, `auth-error` and `auth-retry-button` stay as they are.

- [ ] **Step 1: Add the failing assertions**

In `web/src/auth/SessionProvider.test.tsx`:

In `renders an error state when /v1/me fails for a non-auth reason`, add after its last `expect`:

```tsx
    expect(screen.getByTestId("auth-error").getAttribute("data-slot")).toBe("card");
    expect(screen.getByTestId("auth-retry-button").classList).toContain("pointer-coarse:h-11");
```

In `stops redirecting and explains once the attempts are spent`, add after its last `expect`:

```tsx
    expect(screen.getByTestId("auth-loop-error").getAttribute("data-slot")).toBe("card");
    expect(screen.getByRole("heading", { level: 1, name: "We could not keep you signed in" })).toBeTruthy();
    expect(screen.getByTestId("auth-loop-retry-button").classList).toContain("pointer-coarse:h-11");
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/auth/SessionProvider.test.tsx`
Expected: FAIL in those two tests on the `data-slot` assertion (`expected null to be 'card'`).

- [ ] **Step 3: Rewrite the two screens**

In `web/src/auth/SessionProvider.tsx`, add after the `./useMeQuery` import:

```tsx
import AuthCard, { AuthCardTitle } from "./AuthCard";
import { Button } from "@/components/ui/button";
import { CardContent, CardHeader } from "@/components/ui/card";
```

Replace the `if (status === "loop") { … }` block with:

```tsx
  if (status === "loop") {
    return (
      <AuthCard data-testid="auth-loop-error">
        <CardHeader>
          <AuthCardTitle>We could not keep you signed in</AuthCardTitle>
        </CardHeader>
        <CardContent className="grid gap-4">
          <p className="text-muted-foreground">
            Sign-in worked, but this browser did not hold on to the session, so every page load
            started it over. That happens when cookies are blocked for this site — by browser
            settings, an extension, or a privacy mode that clears them between page loads.
          </p>
          <p className="text-muted-foreground">
            Allow cookies for this site and try again. If it keeps failing, contact your
            administrator.
          </p>
          <Button className="w-full pointer-coarse:h-11" onClick={retryLogin} data-testid="auth-loop-retry-button">
            Try again
          </Button>
        </CardContent>
      </AuthCard>
    );
  }
```

Replace the `if (status === "error") { … }` block with:

```tsx
  if (status === "error") {
    return (
      <AuthCard data-testid="auth-error">
        <CardContent className="grid gap-4">
          <p>Authentication failed. The identity service may be unavailable.</p>
          <Button className="w-full pointer-coarse:h-11" onClick={retryMe} data-testid="auth-retry-button">
            Retry
          </Button>
        </CardContent>
      </AuthCard>
    );
  }
```

- [ ] **Step 4: Run the SessionProvider tests**

Run: `npx vitest run src/auth/SessionProvider.test.tsx`
Expected: all pass. `retries the request rather than the sign-in…` clicks with `retry.click()`, which sends the plain click Base UI's Button handles. If that test fails anyway, change it to `await userEvent.setup().click(retry)` rather than touching the component.

- [ ] **Step 5: Run the dead-CSS guard to see what is now unused**

Run: `npx vitest run src/styles/legacy.guard.test.ts`
Expected: FAIL on `styles/features.css styles only classes a component uses`, with `delete the rules for: session-error`.

- [ ] **Step 6: Delete the session-error rules and the bare `h1`**

In `web/src/styles/features.css`:

1. Delete the whole `/* ---- Session errors ---- */` block: the heading comment, the `/* Preflight audit: SessionProvider's error screens … */` comment, and the `:where(.session-error p)` and `:where(.session-error button)` rules.
2. In the `@media (max-width: 760px)` block, replace:

   ```css
     /* Bare h1 is included deliberately: the auth-loop error screen renders a
        plain h1, which would otherwise stay at 40px on a phone. The breadcrumb's
        h1 is unaffected: its text-sm utility outranks the legacy layer. */
     h1,
     .showcase__title {
       font-size: var(--legacy-text-2xl);
     }

     .showcase__title {
       overflow-wrap: break-word;
     }
   ```

   with:

   ```css
     .showcase__title {
       font-size: var(--legacy-text-2xl);
       overflow-wrap: break-word;
     }
   ```

   The bare `h1` was there only for the auth-loop screen, which now sets its own size. The one bare `h1` left is the `/styleguide` intro, a dev-only page. On a phone it goes back to the 40px `base.css` gives every `h1`.

- [ ] **Step 7: Run the guards, the whole suite and the type-check**

Run: `npx vitest run src/styles && npm test && npx tsc -b`
Expected: all pass; `tsc` prints nothing.

- [ ] **Step 8: Commit**

```bash
git add src/auth/SessionProvider.tsx src/auth/SessionProvider.test.tsx src/styles/features.css
git commit -m "feat(web): session-error screens on the sign-in card" -m "The cookie-loop and identity-service errors were bare markup that relied on Preflight restorations PR 9 would delete, and no PR in the spec's table named them. They now use AuthCard beside the sign-in screen they mirror. Their restorations go, and so does the bare h1 in the phone-width rule, which existed only for the loop screen." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Let `drive-web.mjs` fail requests

The screens this PR migrates are mostly error states. The spec's Preflight section says screen PRs should reach them "by stalling or failing `/v1/*` requests over the DevTools protocol". The driver can't do that yet, so this task adds three flags:

- `--fail <glob>=<status>` answers matching requests itself.
- `--eval <expr>` sets up state, such as the login-attempt count, before a fresh load.
- `--reload` reloads the page, so every query runs again against the failures.

**Files:**
- Modify: `scripts/drive-web.mjs`

**Interfaces:**
- Consumes: nothing.
- Produces, for Task 6: `node scripts/drive-web.mjs [--fail '<glob>=<status>']… [--eval '<expr>']… [--reload] …`, with the other flags unchanged. The order of operations is: sign in, enable the failures, apply each `--goto`, run each `--eval`, reload, apply each `--click`, then print `--fields`/`--probe` and write `--shot`.

- [ ] **Step 1: Document the flags**

In the header comment of `scripts/drive-web.mjs`, add under `Flags:`, after `--goto`:

```js
 *   --fail <glob>=<status>
 *                     answer requests whose URL matches glob with that status
 *                     and the API's JSON error body (repeatable). Takes effect
 *                     after sign-in, so sign-in itself still works. In the
 *                     glob, `*` is any run of characters and `?` is exactly
 *                     one, so a literal `?` can't be matched.
 *   --eval <expr>     evaluate an expression in the page after --goto and
 *                     before --reload (repeatable, 1.5s settle after each)
 *   --reload          reload the page after --eval, so every query runs again
 *                     against --fail
```

Under `Usage:`, add:

```js
 *   node scripts/drive-web.mjs --fail '*/v1/me=500' --reload --shot err.png
```

- [ ] **Step 2: Parse them**

Replace the argument-parsing block, from `const clicks = [];` to the end of the `for` loop, with:

```js
const clicks = [], fails = [], evals = [];
let shot = null, probe = null, fields = false, port = 9222;
let gotos = [], signedOut = false, reload = false;
for (let i = 0; i < argv.length; i++) {
  const next = () => argv[++i];
  if (argv[i] === "--click") clicks.push(next());
  else if (argv[i] === "--shot") shot = next();
  else if (argv[i] === "--probe") probe = next();
  else if (argv[i] === "--fields") fields = true;
  else if (argv[i] === "--port") port = Number(next());
  else if (argv[i] === "--goto") gotos.push(next());
  else if (argv[i] === "--signed-out") signedOut = true;
  else if (argv[i] === "--fail") fails.push(next());
  else if (argv[i] === "--eval") evals.push(next());
  else if (argv[i] === "--reload") reload = true;
}

// --fail specs, split at the last "=" so a glob may contain one.
const failures = fails.map((spec) => {
  const at = spec.lastIndexOf("=");
  const glob = spec.slice(0, at);
  const pattern = glob.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
  return { glob, status: Number(spec.slice(at + 1)), test: new RegExp(`^${pattern}$`) };
});
if (failures.some((f) => !f.glob || !Number.isInteger(f.status))) {
  console.error("--fail takes <glob>=<status>, for example '*/v1/me=500'.");
  process.exit(2);
}
```

- [ ] **Step 3: Answer the paused requests**

Replace the `ws.addEventListener("message", …)` block with:

```js
// Chrome pauses each request a --fail glob matches and waits for an answer.
// The body is the API's error shape, so the app's error handling runs as it
// would for a real failure.
const answer = ({ requestId, request }) => {
  const failure = failures.find((f) => f.test.test(request.url));
  if (!failure) return send("Fetch.continueRequest", { requestId });
  const body = JSON.stringify({ error: "injected", message: `drive-web --fail ${failure.glob}` });
  return send("Fetch.fulfillRequest", {
    requestId,
    responseCode: failure.status,
    responseHeaders: [{ name: "Content-Type", value: "application/json" }],
    body: Buffer.from(body).toString("base64")
  });
};
ws.addEventListener("message", (event) => {
  const msg = JSON.parse(event.data);
  if (msg.method === "Fetch.requestPaused") {
    answer(msg.params).catch((error) => console.error("--fail:", error.message));
    return;
  }
  const slot = msg.id && pending.get(msg.id);
  if (!slot) return;
  pending.delete(msg.id);
  msg.error ? slot.reject(new Error(JSON.stringify(msg.error))) : slot.resolve(msg.result);
});
```

- [ ] **Step 4: Enable the failures after sign-in, then eval and reload**

Replace the block from `console.error("signed in at:", …);` through the end of the `for (const path of gotos) { … }` loop with:

```js
console.error("signed in at:", await evaluate("location.pathname"));

// After sign-in, so a --fail on /v1/me doesn't stop the driver signing in.
if (failures.length) {
  await send("Fetch.enable", { patterns: failures.map((f) => ({ urlPattern: f.glob, requestStage: "Request" })) });
}

// Client-side navigation: react-router follows popstate, and a full load
// would drop the in-memory session state the SPA holds.
for (const path of gotos) {
  await evaluate(`history.pushState({}, "", ${JSON.stringify(path)}); dispatchEvent(new PopStateEvent("popstate"))`);
  await settle(3500);
}

for (const expression of evals) {
  console.error("eval:", await evaluate(expression));
  await settle(1500);
}

if (reload) {
  await send("Page.reload");
  // A failing query retries three times, 2 to 3 seconds apart, before its
  // screen gives up and shows the error (queryClient.ts, polling.ts).
  await settle(12000);
}
```

- [ ] **Step 5: Check the syntax**

Run from the repository root: `node --check scripts/drive-web.mjs`
Expected: no output.

- [ ] **Step 6: Try it against the running app**

This needs the local stack (`docker compose --profile auth up -d` from the repository root) and a dev server on port 5173 started from `web/` on this branch (`npm run dev`), plus headless Chrome as the script's header describes. Export the credentials without printing them:

```bash
export OPENPLAN_USER="$(grep '^KEYCLOAK_PLATFORM_ADMIN_USERNAME=' .env | cut -d= -f2-)"
export OPENPLAN_PASS="$(grep '^KEYCLOAK_PLATFORM_ADMIN_PASSWORD=' .env | cut -d= -f2-)"
node scripts/drive-web.mjs --fail '*/v1/me=500' --reload --probe '!!document.querySelector("[data-testid=auth-error]")'
node scripts/drive-web.mjs --probe '!!document.querySelector("[data-testid=auth-error]")'
```

Expected: `true`, then `false`. The first run reaches the session-error screen. The second has no `--fail`, and the ordinary app is back.

- [ ] **Step 7: Commit**

From the repository root:

```bash
git add scripts/drive-web.mjs
git commit -m "chore: let drive-web fail requests to reach error screens" -m "--fail <glob>=<status> answers matching requests over the DevTools Fetch domain with the API's error body. --eval sets up page state and --reload runs every query again against the failures. Together they reach the 403, 503 and session-error screens without breaking the backend." -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Browser audit, spec amendments, and the PR

This audit checks two things:

1. The screens PR 3 didn't migrate must not move, even though it scoped the legacy field rules and deleted CSS. Their computed styles are compared before and after.
2. The migrated screens must look right: screenshots, plus the checks in Review Focus.

**Files:**
- Modify: `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`
- Scratch, not committed: everything under `$AUDIT`

Scratch files live in `$AUDIT`, a directory outside the repository: this session's scratchpad directory plus `/audit3`. Shell variables don't survive between commands here, so every command block below starts by exporting it, along with the driver's credentials where they're needed.

**Interfaces:**
- Consumes: everything above; `scripts/drive-web.mjs` with `--fail`, `--eval` and `--reload` (Task 5).
- Produces: the PR.

- [ ] **Step 1: Save the dump and diff scripts**

Save as `$AUDIT/dump.js`. It is PR 2's dump without the migrated-piece exemption. The added properties catch a leaking field rule: `min-height`, `border-top-color`, `box-shadow`, `font-family` and `letter-spacing`.

```js
(() => {
  const PROPS = ["display", "position", "vertical-align", "list-style-type", "min-height",
    "margin-top", "margin-right", "margin-bottom", "margin-left",
    "padding-top", "padding-right", "padding-bottom", "padding-left",
    "border-top-width", "border-top-style", "border-top-color", "border-bottom-width", "border-bottom-style",
    "font-family", "font-size", "font-weight", "line-height", "letter-spacing",
    "color", "background-color", "box-shadow", "text-decoration-line", "cursor"];
  const main = document.getElementById("main-content");
  const pick = (el) => {
    const cs = getComputedStyle(el);
    return Object.fromEntries(PROPS.map((p) => [p, cs.getPropertyValue(p)]));
  };
  const out = { main: pick(main) };
  const walk = (el, path) => {
    [...el.children].forEach((child, i) => {
      const key = `${path} > ${child.tagName.toLowerCase()}[${i}]`;
      out[key] = pick(child);
      walk(child, key);
    });
  };
  walk(main, "main");
  return JSON.stringify(out);
})()
```

Save as `$AUDIT/diff.py`:

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

The local stack must be up (`docker compose --profile auth up -d` from the repository root). Sign in once and collect real IDs: click into one stack, one of its templates, one run of that template, and one registry template. After each click, run `node scripts/drive-web.mjs --probe 'location.pathname'`. If the local database has no stack with a template and a run, create them through the UI first.

Write `$AUDIT/screens.txt`, one `name path` pair per line. These are the screens PR 3 didn't migrate. The eight marked `# form` exercise the scoped field rules; strip the markers from the file.

```
stacks /stacks
stacks-new /stacks/new                                                        # form
stack-templates /stacks/{stackId}/templates
stack-templates-new /stacks/{stackId}/templates/new                           # form
template-runs /stacks/{stackId}/templates/{stackTemplateId}/runs
template-run /stacks/{stackId}/templates/{stackTemplateId}/runs/{runNumber}
template-variables /stacks/{stackId}/templates/{stackTemplateId}/variables    # form
template-credentials /stacks/{stackId}/templates/{stackTemplateId}/credentials # form
template-settings /stacks/{stackId}/templates/{stackTemplateId}/settings
template-upgrade /stacks/{stackId}/templates/{stackTemplateId}/upgrade        # form
environment /stacks/{stackId}/environment                                     # form
access /stacks/{stackId}/access                                               # form
registry /templates
registry-new /templates/new                                                   # form
registry-template /templates/{sourceTemplateId}
```

Write `$AUDIT/shots.sh`. It captures the migrated screens in every state PR 3 touched, and takes the output directory and file prefix as arguments:

```bash
#!/usr/bin/env bash
# Usage: shots.sh <out-dir> <before|after> <stackId>
set -u
out=$1 tag=$2 stack=$3
d() { node scripts/drive-web.mjs "$@"; }
attempts='sessionStorage.setItem("openplan.auth.loginAttempts", "3")'
wrong='(() => {
  const set = (el, v) => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(el, v);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  };
  const u = document.querySelector("#signin-username");
  set(u, "audit-nobody");
  set(document.querySelector("#signin-password"), "not-a-password");
  u.form.requestSubmit();
  return "submitted";
})()'
d --goto /no-such-page --shot "$out/$tag-not-found.png"
d --goto "/stacks/$stack" --shot "$out/$tag-placeholder.png"
d --fail '*/v1/tenants/*/stacks=403' --reload --shot "$out/$tag-access-denied.png"
d --fail '*/v1/tenants/*/stacks=503' --reload --shot "$out/$tag-service-unavailable.png"
d --fail '*/v1/me=500' --reload --shot "$out/$tag-auth-error.png"
d --fail '*/v1/me=401' --eval "$attempts" --reload --shot "$out/$tag-auth-loop.png"
d --signed-out --goto /signin --shot "$out/$tag-signin.png"
d --signed-out --goto /signin --eval "$wrong" --shot "$out/$tag-signin-error.png"
d --signed-out --goto /signin --eval "$attempts" --reload --shot "$out/$tag-signin-cookies.png"
d --signed-out --goto /styleguide --shot "$out/$tag-styleguide.png"
# Each run starts at /stacks with a real session, and a good /v1/me clears the
# attempt count, so the two runs above that set it leave nothing behind.
```

`audit-nobody` is not an account, so the wrong-password capture doesn't spend anyone's failed attempts. The server answers an unknown username with the same 401 as a wrong password.

- [ ] **Step 3: Capture "before" from `feat/shadcn-shell-shared`**

From the repository root:

```bash
export AUDIT=<this session's scratchpad>/audit3
export OPENPLAN_USER="$(grep '^KEYCLOAK_PLATFORM_ADMIN_USERNAME=' .env | cut -d= -f2-)"
export OPENPLAN_PASS="$(grep '^KEYCLOAK_PLATFORM_ADMIN_PASSWORD=' .env | cut -d= -f2-)"
git worktree add "$AUDIT/before-tree" feat/shadcn-shell-shared
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
```

The driver runs from the main working tree, which has Task 5's flags, against whichever app is on port 5173. Stop that dev server (find it with `lsof -i :5173`) and keep Chrome running.

- [ ] **Step 4: Capture "after" from this branch**

Start `npm run dev` from `web/` on `feat/shadcn-standalone-screens`. Then run the same loop into `$AUDIT/after/`, and `bash "$AUDIT/shots.sh" "$AUDIT/shots" after <stackId>`.

- [ ] **Step 5: Diff**

Run: `python3 "$AUDIT/diff.py" "$AUDIT/before" "$AUDIT/after"`
Expected: `CLEAN`. Every screen PR 3 didn't migrate is unchanged, fields included.

A difference on a form screen means the field scoping reached a legacy field. Check that field for a `data-slot` attribute, and check the selector in Task 1, Step 3. A difference elsewhere means a rule was deleted too broadly in Task 3 or Task 4. Fix it in the task that owns it, re-capture "after", and diff again.

- [ ] **Step 6: Check what the diff can't**

Still on the "after" server:

1. **The error screens were really reached.** Open each `after-*.png`. `access-denied` shows "Not permitted", `service-unavailable` shows "Authorization service unavailable", `auth-error` shows "Authentication failed", `auth-loop` and `signin-cookies` show "We could not keep you signed in", and `signin-error` shows "Incorrect username or password." If one shows the ordinary screen instead, its `--fail` glob missed. Print the request URLs with `--probe 'JSON.stringify(performance.getEntriesByType("resource").map((e) => e.name).filter((n) => n.includes("/v1/")))'` and correct the glob.
2. **The fields are shadcn's size.** Run `node scripts/drive-web.mjs --signed-out --goto /signin --probe 'JSON.stringify([...document.querySelectorAll("input")].map((i) => i.getBoundingClientRect().height))'`
   Expected: `[32,32]`. `36` means a legacy `min-height` still reaches Input.
3. **Nothing scrolls sideways at phone width.** Quit Chrome and restart it with `--window-size=375,812` and the same `--user-data-dir`, which keeps the session. Probe each of these with `--probe 'JSON.stringify({ scroll: document.documentElement.scrollWidth, width: innerWidth })'`:
   - `--signed-out --goto /signin`
   - `--signed-out --goto /signin --eval "<the wrong-password expression from shots.sh>"`
   - `--goto /no-such-page`
   - `--fail '*/v1/me=401' --eval "<the attempts expression>" --reload`

   Expected: `scroll` is at most `width` on each.

Then compare the before and after screenshots side by side. Expected differences:
- the route screens lose the large gradient title and hero graphic, and show an icon tile, a smaller title and a sentence;
- sign-in becomes a shadcn card with 32px fields and shadcn buttons;
- the session errors become that same card, where before they were unstyled text at the top left;
- `/styleguide` gains the Card, Alert, Input and Label, and Empty specimens.

Nothing else should change.

Clean up from the repository root: stop Chrome and the dev server, run `docker compose start web`, then `git worktree remove --force "$AUDIT/before-tree"`.

- [ ] **Step 7: Bring the spec in line with what PR 3 built**

Edit `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`:

- **Status line:** `Approved. Guard and testing sections reviewed with PR 1. PRs 1 to 3 settled the details recorded below.`
- **Preflight paragraph** ("Restore what Preflight takes"): in its last sentence, after `over the DevTools protocol`, add ` (\`scripts/drive-web.mjs --fail\`)`.
- **Migration table, PR 3 row:** Scope becomes `Standalone screens: sign-in, access denied, not found, service unavailable, route placeholder, and the two session-error screens`. Main shadcn pieces becomes `Card, Button, Alert, Input, Label; Empty for the route messages`.
- **Migration table, PR 4 row:** Main shadcn pieces becomes `Table; reuses Input, Label and Button`.
- **After the "What PR 2 settled" list,** add:

  ```markdown
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

From the repository root:

```bash
git add docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md
git commit -m "docs: record what PR 3 of the shadcn migration settled" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 10: Push and open the PR against `feat/shadcn-shell-shared`**

Write `$AUDIT/pr-body.md` first. It must cover:
- that this PR is stacked on #276 and implements PR 3 of the spec, with links to the spec and this plan;
- what moved: the four route messages onto `RouteMessage` (Empty), and sign-in and the two session-error screens onto `AuthCard` (Card, Alert, Input, Label, Button);
- the deviations from the spec's table: Input and Label a PR early, Empty added, and the session-error screens included. Give each one its reason;
- the legacy field-rule scoping and why it's needed;
- what was deleted: the sign-in and session-error CSS, and the bare `h1` in the phone-width rule;
- the behaviour and look changes: the route screens lose the gradient title and hero graphic; sign-in and the session errors become a centred card. Copy, test IDs and flows are unchanged;
- the new `drive-web.mjs` flags;
- configuration impact: none. No new dependencies, no migrations;
- validation: the commands above with the test count, the audit's `CLEAN` result, the 32px field heights, and the phone-width probes;
- a `## Screenshots` heading listing the before/after pairs in `$AUDIT/shots/`. `gh` can't upload images, so tell the user to drag them into the PR description;
- a final line: `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.

Then, from the repository root:

```bash
export AUDIT=<this session's scratchpad>/audit3
gh auth status
git push -u origin feat/shadcn-standalone-screens
gh pr create --base feat/shadcn-shell-shared --head feat/shadcn-standalone-screens \
  --title "feat(web): standalone screens on shadcn" --body-file "$AUDIT/pr-body.md"
```

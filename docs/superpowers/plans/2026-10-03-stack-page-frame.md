# Stack page frame (PR 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/stacks/:id` the stack's one page: a header, the stack's templates on the left, and the selected template's panel on the right, with its Runs tab and runs drawn to openplan UI.

**Architecture:** `StackPage` becomes the layout route at `/stacks/:stackId`: header, template list, and the panel as its `<Outlet/>`. Every existing template URL (`templates/:id/runs`, `runs/:n`, `variables`, `credentials`, `settings`, `upgrade`, `templates/new`) renders into that outlet, so no link breaks. `TemplatePanel` replaces `StackTemplateDetailShell` and hands the template to its tabs through a React context. The Runs tab and the run view are rebuilt on openplan UI. Variables, Credentials, Settings, Change revision and Add template keep their current content inside the new panel until PR 2.

**Tech Stack:** React 19, react-router-dom 6 (data routers), TanStack Query 5, Tailwind v4 with the tokens in `web/src/styles/theme.css`, shadcn/ui (base-nova), lucide-react, Vitest and Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-03-stack-page-design.md`. Read "The page", "Routes", "The template panel" (header, tabs, content, refreshing, missing; Runs; A run) and "Run status". The Variables, Credentials, Settings, Change revision and Add template sections are PR 2.

## Global Constraints

- **Branch** `feat/stack-page`, from `main`. The spec is already committed on it. PR base is `main`.
- **Design system: openplan UI.** Only theme tokens and Tailwind scale steps. `src/styles/tailwind.guard.test.ts` bans arbitrary values (`w-[37px]`, `grid-cols-[…]`) and palette colours. A width the scale lacks is written as a scale step (`w-51.5` = 206px). Spacing: N × 4px.
- **No status dots.** Every state is a lucide icon and a word, through `StatusLabel` (`web/src/shared/StatusLabel.tsx`) or a Chip. `StatusBadge` and `statusGlyph` leave every screen this plan touches.
- **Buttons:** `buttonClass(variant, size)` from `web/src/shared/buttonClass.ts`. A link (`<Link>`) when it navigates, a `<button>` when it acts. `pointer-coarse:h-11` on every button and link styled as one.
- **Type steps:** `text-page-title`, `text-panel-title`, `text-meta` (13px), `text-sm` (14), `text-xs` (12). Titles add `tracking-title`. Names in mono: `font-mono`.
- **Copy:** sentence case; status words lowercase; " · " joins metadata (an aria-hidden `<span className="text-separator">·</span>`); times are absolute (`formatDateTime`, `formatTimestamp` in `web/src/shared/formatTimestamp.ts`).
- **Tests:** Vitest. jsdom files start with `// @vitest-environment jsdom`. Seed the query cache with `staleTime: Infinity, retry: false`. Find elements by role, label, text or test ID, never by class. The two phone-layout assertions in Task 5 are the one allowed exception. Run single files with `npx vitest run <path>` from `web/`.
- **Commits:** `feat(web): …`, `refactor(web): …`, `test(web): …`, each ending with the line `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A URL naming a template the stack no longer has** (destroyed, or a stale link): the panel says "That template is not installed on this stack." and the list stays usable. Test in Task 5.
2. **The filter hides the selected template:** the panel keeps showing it, and nothing navigates. Test in Task 5.
3. **A plan finishes while the person sits on Variables:** the panel header and the list chips update without a reload, because the refresh effect now lives in `TemplatePanel`. Test in Task 4.
4. **A viewer without `canOperate` or `canApprove`:** no Add template row, Plan and Apply disabled with "Starting a run requires operator access.", no Auto apply, and no Approve on the run. Tests in Tasks 5, 6 and 7.
5. **Long template names and a 375px phone:** list rows truncate the name and keep their chips, the runs table scrolls inside its own frame, and the page never scrolls sideways. Checked in the browser in Task 8.

---

## File map

| File | Responsibility |
| --- | --- |
| `web/src/shared/StatusLabel.tsx` (modify) | adds the `StatusIndicator` type |
| `web/src/features/runs/runIndicator.ts` (from `runStatusLabel.ts`) | a run's word, icon and tone |
| `web/src/features/runs/RunStatusLabel.tsx` (new) | `StatusLabel` for a run |
| `web/src/features/stacks/templateSelection.ts` (new) | default template, current tab, tab paths |
| `web/src/features/stacks/StackTemplateStatusLabel.tsx` (modify) | waiting read from `pending_plan_run_id` too |
| `web/src/shared/SearchField.tsx`, `Chip.tsx`, `listItemClass.ts` (new) | openplan UI pieces shared by `/stacks` and the stack page |
| `web/src/features/stacks/StackMeta.tsx`, `NoTemplatesState.tsx` (new) | slug and tags; the no-templates empty state |
| `web/src/features/stacks/stackTemplateContext.ts` (new) | the template a panel is about, for its tabs |
| `web/src/features/runs/useRefreshStackOnRunChange.ts` (new) | refetch the stack when a run settles or waits |
| `web/src/features/stacks/TemplateTabs.tsx`, `TemplatePanel.tsx` (new) | the panel's header, tabs and content |
| `web/src/features/stacks/StackTemplateList.tsx`, `StackPage.tsx`, `StackIndexPanel.tsx`, `AddTemplatePanel.tsx`, `StackSectionLayout.tsx` (new) | the page, its list, its index, the add wrapper, Environment and Access |
| `web/src/shared/ErrorLine.tsx` (new) | a failure in one destructive line |
| `web/src/features/runs/TemplateRunActions.tsx`, `TemplateRunHistory.tsx`, `RunDetailScreen.tsx`, `RunLogsPanel.tsx` (modify) | the Runs tab and the run, on openplan UI |
| `web/src/features/runs/TemplateRunNotices.tsx`, `WaitingRunActions.tsx` (new) | waiting and failed cards; Approve and Discard |
| `web/src/shared/LogSteps.tsx` (modify) | log rows on `canvas` |
| `web/src/app/router.tsx` (modify) | the new tree |
| Deleted | `StackDetailShell.tsx` (+ test), `StackTemplateDetailShell.tsx`, `StackTemplateListScreen.tsx`, `RoutePlaceholder.tsx` (+ test), `RouteTabs.tsx` (+ test) |

---

### Task 1: Run status as an icon and a word

**Files:**
- Modify: `web/src/shared/StatusLabel.tsx`
- Rename: `web/src/features/runs/runStatusLabel.ts` → `web/src/features/runs/runIndicator.ts`
- Rename: `web/src/features/runs/runStatusLabel.test.ts` → `web/src/features/runs/runIndicator.test.ts`
- Create: `web/src/features/runs/RunStatusLabel.tsx`
- Modify: `web/src/features/stacks/StackTemplateStatusLabel.tsx` (use the shared type)
- Modify: `web/src/features/runs/TemplateRunHistory.tsx`, `web/src/features/runs/RunDetailScreen.tsx` (status cell and header)
- Modify: `web/src/features/runs/TemplateRunHistory.test.tsx`, `web/src/features/runs/RunDetailScreen.test.tsx` (expected words)

**Interfaces:**
- Produces: `export interface StatusIndicator { label: string; icon: LucideIcon; tone: StatusLabelTone; strong: boolean }` in `StatusLabel.tsx`.
- Produces: `export type RunFields = Pick<TemplateRun, "operation" | "status" | "step" | "plan_summary" | "auto_approve">`, `export function runIndicator(run: RunFields): StatusIndicator`, and `export function runProgressTag(run: Pick<TemplateRun, "status" | "step">): string` (unchanged) in `runIndicator.ts`.
- Produces: `export default function RunStatusLabel(props: { run: RunFields } & Omit<ComponentProps<"span">, "children">)`.

- [ ] **Step 1: Move the files**

```bash
cd web
git mv src/features/runs/runStatusLabel.ts src/features/runs/runIndicator.ts
git mv src/features/runs/runStatusLabel.test.ts src/features/runs/runIndicator.test.ts
```

- [ ] **Step 2: Write the failing tests**

Replace the whole of `web/src/features/runs/runIndicator.test.ts` with:

```ts
import { Ban, Check, Equal, FileText, Hourglass, LoaderCircle, TriangleAlert } from "lucide-react";
import { describe, expect, it } from "vitest";
import type { TemplateRun } from "../../api/types";
import { runIndicator, runProgressTag } from "./runIndicator";

const counts = { add: 1, change: 0, destroy: 0 };

function label(
  operation: TemplateRun["operation"],
  status: TemplateRun["status"],
  planned: boolean,
  autoApprove = false,
  step: TemplateRun["step"] = ""
): string {
  return runIndicator({ operation, status, step, plan_summary: planned ? counts : null, auto_approve: autoApprove }).label;
}

describe("runIndicator words", () => {
  it.each([
    ["apply", "running", false, "planning"],
    ["destroy", "queued", false, "planning destroy"],
    ["apply", "waiting_approval", true, "waiting for approval"],
    ["destroy", "waiting_approval", true, "destroy waiting for approval"],
    ["apply", "approved", true, "approved"],
    ["destroy", "approved", true, "destroy approved"],
    ["apply", "running", true, "applying"],
    ["destroy", "running", true, "destroying"],
    ["apply", "completed", false, "no changes"],
    ["destroy", "completed", false, "nothing to destroy"],
    ["apply", "completed", true, "applied"],
    ["destroy", "completed", true, "destroyed"],
    ["apply", "failed", false, "plan failed"],
    ["destroy", "failed", false, "destroy plan failed"],
    ["apply", "failed", true, "apply failed"],
    ["destroy", "failed", true, "destroy failed"],
    ["apply", "canceled", true, "discarded"],
    ["destroy", "canceled", true, "destroy discarded"]
  ] as const)("%s %s (planned: %s) reads %s", (operation, status, planned, expected) => {
    expect(label(operation, status, planned)).toBe(expected);
  });

  // A plan run applies nothing, so no label of its says it did, even once
  // its plan has counts.
  it.each([
    ["queued", false, "planning"],
    ["running", true, "planning"],
    ["completed", true, "plan finished"],
    ["completed", false, "no changes"],
    ["failed", true, "plan failed"],
    ["canceled", false, "canceled"]
  ] as const)("plan run %s (planned: %s) reads %s", (status, planned, expected) => {
    expect(label("plan", status, planned)).toBe(expected);
  });

  // An auto-approved apply never plans on its own, so it is applying from the
  // start, before it has any counts.
  it.each([
    ["queued", false, "applying"],
    ["running", false, "applying"],
    ["completed", true, "applied"],
    ["failed", false, "apply failed"],
    ["canceled", false, "canceled"]
  ] as const)("auto-approved apply %s (counts: %s) reads %s", (status, planned, expected) => {
    expect(label("apply", status, planned, true)).toBe(expected);
  });

  // A running run says what it is doing; a failed one says what it was doing.
  // Planning and applying add nothing the headline does not already say.
  it.each([
    ["plan", "running", false, false, "fetching_source", "planning · fetching source"],
    ["apply", "running", true, false, "waiting_for_executor", "applying · waiting for an executor"],
    ["destroy", "running", true, false, "restoring_plan", "destroying · restoring saved plan"],
    ["apply", "running", false, true, "initializing", "applying · initializing"],
    ["apply", "running", false, false, "planning", "planning"],
    ["apply", "running", true, false, "applying", "applying"],
    ["apply", "failed", false, false, "fetching_source", "plan failed while fetching source"],
    ["destroy", "failed", true, false, "waiting_for_executor", "destroy failed while waiting for an executor"],
    ["apply", "failed", true, false, "applying", "apply failed"],
    ["apply", "waiting_approval", true, false, "saving_plan", "waiting for approval"],
    ["apply", "completed", true, false, "applying", "applied"]
  ] as const)("%s %s (planned: %s, auto: %s) on %s reads %s", (operation, status, planned, autoApprove, step, expected) => {
    expect(label(operation, status, planned, autoApprove, step)).toBe(expected);
  });

  // A backend newer than this client can send a step outside the union (API
  // responses are cast, not validated). It reads exactly like no step.
  it("reads an unknown step as its headline, running or failed", () => {
    const unknownStep = "cloning" as TemplateRun["step"];
    expect(label("apply", "running", false, false, unknownStep)).toBe("planning");
    expect(label("apply", "failed", false, false, unknownStep)).toBe("plan failed");
  });
});

describe("runIndicator look", () => {
  // Only what waits on a person or broke is coloured, and only that carries
  // weight; a run still working is grey, like the destroying template state.
  it.each([
    ["apply", "waiting_approval", true, false, Hourglass, "attention", true],
    ["apply", "completed", true, false, Check, "settled", false],
    ["destroy", "completed", true, false, Check, "settled", false],
    ["plan", "completed", true, false, FileText, "settled", false],
    ["apply", "completed", false, false, Equal, "settled", false],
    ["destroy", "completed", false, false, Equal, "settled", false],
    ["apply", "completed", false, true, Check, "settled", false],
    ["apply", "failed", true, false, TriangleAlert, "failed", true],
    ["apply", "canceled", true, false, Ban, "idle", false],
    ["apply", "running", true, false, LoaderCircle, "settled", false],
    ["apply", "queued", false, false, LoaderCircle, "settled", false],
    ["apply", "approved", true, false, LoaderCircle, "settled", false]
  ] as const)("%s %s (planned: %s, auto: %s) is drawn as it should be", (operation, status, planned, autoApprove, icon, tone, strong) => {
    const indicator = runIndicator({ operation, status, step: "", plan_summary: planned ? counts : null, auto_approve: autoApprove });
    expect({ icon: indicator.icon, tone: indicator.tone, strong: indicator.strong }).toEqual({ icon, tone, strong });
  });
});

// Logs are refetched when this tag changes. Status alone stays running for a
// whole run, so the step has to be part of it.
describe("runProgressTag", () => {
  it("changes when the step changes within one status", () => {
    expect(runProgressTag({ status: "running", step: "initializing" })).not.toBe(runProgressTag({ status: "running", step: "planning" }));
  });

  it("changes when the status changes on the same step", () => {
    expect(runProgressTag({ status: "running", step: "applying" })).not.toBe(runProgressTag({ status: "completed", step: "applying" }));
  });
});
```

- [ ] **Step 3: Run the tests and see them fail**

Run: `npx vitest run src/features/runs/runIndicator.test.ts`
Expected: FAIL, `runIndicator` is not exported.

- [ ] **Step 4: Add the shared type to `web/src/shared/StatusLabel.tsx`**

Add, after the `StatusLabelTone` type:

```ts
/** A state as a StatusLabel draws it: its word, its icon, its tone, and
    whether it needs a person now. */
export interface StatusIndicator {
  label: string;
  icon: LucideIcon;
  tone: StatusLabelTone;
  strong: boolean;
}
```

- [ ] **Step 5: Write `web/src/features/runs/runIndicator.ts`**

Replace the whole file with:

```ts
import { Ban, Check, Equal, FileText, Hourglass, LoaderCircle, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { TemplateRun, TemplateRunStep } from "../../api/types";
import type { StatusIndicator } from "../../shared/StatusLabel";

export type RunFields = Pick<TemplateRun, "operation" | "status" | "step" | "plan_summary" | "auto_approve">;

// runHeadline says where a run is, in words, with the operation folded in so
// no separate Type is needed: "destroy waiting for approval" rather than
// destroy + waiting_approval. Every destroy label says destroy, since the
// label is the only place the runs table names the operation. The words are
// lowercase, as openplan UI's status words are in rows and beside a title.
//
// A saved plan that someone discarded ends canceled, and reads as discarded;
// nothing else ends canceled, since a running run cannot be stopped.
//
// A plan run only ever plans, so it reads as planning until it ends. An
// auto-approved apply run never plans on its own, so it reads as applying
// throughout. Any other run has plan counts only once a plan with changes has
// finished, so one that has them and is still working is applying, and one
// that failed with them failed applying.
function runHeadline(run: RunFields): string {
  if (run.operation === "plan") {
    return planRunHeadline(run);
  }
  if (run.auto_approve) {
    return autoApprovedRunHeadline(run);
  }

  const destroy = run.operation === "destroy";
  const planned = run.plan_summary !== null && run.plan_summary !== undefined;

  switch (run.status) {
    case "waiting_approval":
      return destroy ? "destroy waiting for approval" : "waiting for approval";
    case "approved":
      return destroy ? "destroy approved" : "approved";
    case "completed":
      if (!planned) {
        return destroy ? "nothing to destroy" : "no changes";
      }
      return destroy ? "destroyed" : "applied";
    case "failed":
      if (!planned) {
        return destroy ? "destroy plan failed" : "plan failed";
      }
      return destroy ? "destroy failed" : "apply failed";
    case "canceled":
      return destroy ? "destroy discarded" : "discarded";
    default:
      if (!planned) {
        return destroy ? "planning destroy" : "planning";
      }
      return destroy ? "destroying" : "applying";
  }
}

function planRunHeadline(run: Pick<TemplateRun, "status" | "plan_summary">): string {
  switch (run.status) {
    case "completed":
      return run.plan_summary ? "plan finished" : "no changes";
    case "failed":
      return "plan failed";
    case "canceled":
      return "canceled";
    default:
      return "planning";
  }
}

function autoApprovedRunHeadline(run: Pick<TemplateRun, "status">): string {
  switch (run.status) {
    case "completed":
      return "applied";
    case "failed":
      return "apply failed";
    case "canceled":
      return "canceled";
    default:
      return "applying";
  }
}

const STEP_LABELS: Record<TemplateRunStep, string> = {
  waiting_for_executor: "waiting for an executor",
  preparing_workspace: "preparing workspace",
  fetching_source: "fetching source",
  restoring_plan: "restoring saved plan",
  initializing: "initializing",
  selecting_workspace: "selecting workspace",
  planning: "planning",
  saving_plan: "saving plan",
  applying: "applying"
};

// Steps the headline already names: "planning · planning" says nothing.
const HEADLINE_STEPS = new Set<TemplateRunStep>(["planning", "applying"]);

// A running run adds the step it is on, and a failed one the step it failed
// on, so a slow clone reads as a clone and a failed one says so.
function runLabel(run: RunFields, headline: string): string {
  if (run.step === "" || HEADLINE_STEPS.has(run.step)) {
    return headline;
  }
  // A backend newer than this client can send a step outside the union; read
  // it as if there were none rather than rendering "undefined" or throwing.
  const step = STEP_LABELS[run.step];
  if (!step) {
    return headline;
  }
  if (run.status === "running") {
    return `${headline} · ${step}`;
  }
  if (run.status === "failed") {
    return `${headline} while ${step}`;
  }
  return headline;
}

// A finished run's icon says what it left behind: nothing changed, a plan to
// read, or changes made.
const COMPLETED_ICONS: Record<string, LucideIcon> = {
  "no changes": Equal,
  "nothing to destroy": Equal,
  "plan finished": FileText
};

/**
 * openplan UI's status for one run. Only a plan waiting for approval and a
 * failure are coloured; a run still working stays grey, like a template that
 * is destroying.
 */
export function runIndicator(run: RunFields): StatusIndicator {
  const headline = runHeadline(run);
  const label = runLabel(run, headline);
  switch (run.status) {
    case "waiting_approval":
      return { label, icon: Hourglass, tone: "attention", strong: true };
    case "failed":
      return { label, icon: TriangleAlert, tone: "failed", strong: true };
    case "canceled":
      return { label, icon: Ban, tone: "idle", strong: false };
    case "completed":
      return { label, icon: COMPLETED_ICONS[headline] ?? Check, tone: "settled", strong: false };
    default:
      return { label, icon: LoaderCircle, tone: "settled", strong: false };
  }
}

// runProgressTag changes whenever a run moves: a new status, or a new step
// within one. Queries that must refresh as a run progresses, such as its
// logs, key on it.
export function runProgressTag(run: Pick<TemplateRun, "status" | "step">): string {
  return `${run.status}:${run.step}`;
}
```

- [ ] **Step 6: Run the tests and see them pass**

Run: `npx vitest run src/features/runs/runIndicator.test.ts`
Expected: PASS.

- [ ] **Step 7: Create `web/src/features/runs/RunStatusLabel.tsx`**

```tsx
import type { ComponentProps } from "react";
import StatusLabel from "../../shared/StatusLabel";
import { runIndicator } from "./runIndicator";
import type { RunFields } from "./runIndicator";

// One run's state as openplan UI draws it: an icon and a word.
export default function RunStatusLabel({ run, ...props }: { run: RunFields } & Omit<ComponentProps<"span">, "children">) {
  const { label, icon, tone, strong } = runIndicator(run);
  return (
    <StatusLabel icon={icon} tone={tone} strong={strong} {...props}>
      {label}
    </StatusLabel>
  );
}
```

- [ ] **Step 8: Use the shared type in `web/src/features/stacks/StackTemplateStatusLabel.tsx`**

Delete the local `interface Indicator { … }` block. Import the type with `import type { StatusIndicator, StatusLabelTone } from "../../shared/StatusLabel";` (replacing the `StatusLabelTone` import). Change `Record<StatusTone, Omit<Indicator, "label">>` to `Record<StatusTone, Omit<StatusIndicator, "label">>` and the return type of `stackTemplateIndicator` to `StatusIndicator`. Drop the `LucideIcon` import if nothing else uses it.

- [ ] **Step 9: Point the two callers at `RunStatusLabel`**

In `web/src/features/runs/TemplateRunHistory.tsx`, replace the status cell:

```tsx
      <TableCell>
        <RunStatusLabel run={run} data-testid={`template-run-status-${run.id}`} />
      </TableCell>
```

Then remove the `StatusBadge`, `statusTone` and `runStatusLabel` imports, the `const tone = statusTone(run.status);` line, and add `import RunStatusLabel from "./RunStatusLabel";`.

In `web/src/features/runs/RunDetailScreen.tsx`, replace the `<StatusBadge …>{runStatusLabel(run)}</StatusBadge>` in the header with `<RunStatusLabel run={run} data-testid="run-detail-status" />`. Change the import of `./runStatusLabel` to `import { runProgressTag } from "./runIndicator";`, add `import RunStatusLabel from "./RunStatusLabel";`, and remove the `StatusBadge` and `statusTone` imports.

- [ ] **Step 10: Update the two tests for the new words**

In `web/src/features/runs/TemplateRunHistory.test.tsx`, in "lays each run out in run, type, status, changes, actor, and time columns", change `expect(cells[1].textContent).toContain("Applied");` to `expect(cells[1].textContent).toBe("applied");`.

In `web/src/features/runs/RunDetailScreen.test.tsx`, in "renders the run summary and stacks its logs…", change `toContain("No changes")` to `toBe("no changes")`. The `data-tone` assertion stays: `StatusLabel` sets `data-tone` too.

- [ ] **Step 11: Run the run tests and the type check**

Run: `npx vitest run src/features/runs src/features/stacks/StackTemplateStatusLabel.test.tsx && npx tsc -b`
Expected: PASS, and no type errors. `rg runStatusLabel src` prints nothing.

- [ ] **Step 12: Commit**

```bash
git add -A src/shared/StatusLabel.tsx src/features/runs src/features/stacks/StackTemplateStatusLabel.tsx
git commit -m "feat(web): draw run states as an icon and a word

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Which template, which tab

**Files:**
- Create: `web/src/features/stacks/templateSelection.ts`
- Create: `web/src/features/stacks/templateSelection.test.ts`
- Modify: `web/src/features/stacks/StackTemplateStatusLabel.tsx`
- Modify: `web/src/features/stacks/StackTemplateStatusLabel.test.tsx`

**Interfaces:**
- Produces: `export type TemplateTab = "runs" | "variables" | "credentials" | "settings"`, `export function defaultStackTemplate(templates: StackTemplate[]): StackTemplate | null`, `export function templateTabOf(pathname: string): TemplateTab`, and `export function stackTemplatePath(stackId: string, stackTemplateId: string, tab?: TemplateTab): string`.
- Produces: `stackTemplateIndicator(stackTemplate, attention)` and `stackTemplateActivity(stackTemplate, attention)` keep their signatures. Both now treat `pending_plan_run_id !== ""` as waiting, with or without an attention item.

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/stacks/templateSelection.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { StackTemplate } from "../../api/types";
import { defaultStackTemplate, stackTemplatePath, templateTabOf } from "./templateSelection";

function template(id: string, overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id,
    stack_id: "stack_1",
    component_key: id,
    source_template_id: "source_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "",
    source_ref: "main",
    workspace_name: id,
    display_name: id,
    config: {},
    last_applied_run_id: "",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "matches",
    created_by: "user_1",
    lifecycle: "active",
    ...overrides
  };
}

describe("defaultStackTemplate", () => {
  it("opens on the first template with a plan waiting for approval", () => {
    const failed = template("cdn", { lifecycle: "failed" });
    const waiting = template("eks", { pending_plan_run_id: "run_14" });
    expect(defaultStackTemplate([template("network"), failed, waiting])?.id).toBe("eks");
  });

  it("falls back to the first whose destroy failed", () => {
    expect(defaultStackTemplate([template("network"), template("cdn", { lifecycle: "failed" })])?.id).toBe("cdn");
  });

  it("falls back to the first template", () => {
    expect(defaultStackTemplate([template("network"), template("eks")])?.id).toBe("network");
  });

  it("has nothing to open on a stack with no templates", () => {
    expect(defaultStackTemplate([])).toBeNull();
  });
});

describe("templateTabOf", () => {
  it.each([
    ["/stacks/s1", "runs"],
    ["/stacks/s1/templates/new", "runs"],
    ["/stacks/s1/templates/t1", "runs"],
    ["/stacks/s1/templates/t1/runs", "runs"],
    ["/stacks/s1/templates/t1/runs/14", "runs"],
    ["/stacks/s1/templates/t1/variables", "variables"],
    ["/stacks/s1/templates/t1/credentials", "credentials"],
    ["/stacks/s1/templates/t1/settings", "settings"],
    ["/stacks/s1/templates/t1/upgrade", "settings"]
  ] as const)("%s belongs to %s", (pathname, tab) => {
    expect(templateTabOf(pathname)).toBe(tab);
  });
});

describe("stackTemplatePath", () => {
  it("names a template's tab, Runs by default", () => {
    expect(stackTemplatePath("s1", "t1")).toBe("/stacks/s1/templates/t1/runs");
    expect(stackTemplatePath("s1", "t1", "variables")).toBe("/stacks/s1/templates/t1/variables");
  });
});
```

In `web/src/features/stacks/StackTemplateStatusLabel.test.tsx`, add a row to the `stackTemplateIndicator` table:

```ts
    ["waiting for approval", template({ live_state: "differs", pending_plan_run_id: "run_14" }), undefined, Hourglass, "attention", true]
```

and a test to `stackTemplateActivity`:

```ts
  it("dates a waiting plan from the template's own pending plan when there is no attention item", () => {
    expect(stackTemplateActivity(template({ pending_plan_run_id: "run_14", pending_plan_at: "2026-10-03T09:30:00Z" }), undefined)).toMatch(
      /^Planned 3 Oct, \d\d:\d\d$/
    );
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `npx vitest run src/features/stacks/templateSelection.test.ts src/features/stacks/StackTemplateStatusLabel.test.tsx`
Expected: FAIL. `templateSelection` is missing, and the new pending-plan cases read "changed" and "Never applied".

- [ ] **Step 3: Create `web/src/features/stacks/templateSelection.ts`**

```ts
import { matchPath } from "react-router-dom";
import type { StackTemplate } from "../../api/types";

/** The tabs of a template's panel, each its own route below the template. */
export type TemplateTab = "runs" | "variables" | "credentials" | "settings";

/**
 * The template the stack's page opens on when the URL names none: the first
 * with a plan waiting for approval, else the first whose destroy failed, else
 * the first. What needs a person is what someone most likely came for.
 */
export function defaultStackTemplate(templates: StackTemplate[]): StackTemplate | null {
  return (
    templates.find((stackTemplate) => stackTemplate.pending_plan_run_id !== "") ??
    templates.find((stackTemplate) => stackTemplate.lifecycle === "failed") ??
    templates[0] ??
    null
  );
}

/**
 * The tab a path belongs to. A run belongs to Runs and Change revision to
 * Settings; anywhere else on the stack's page, Runs.
 */
export function templateTabOf(pathname: string): TemplateTab {
  const match = matchPath("/stacks/:stackId/templates/:stackTemplateId/:section/*", pathname);
  switch (match?.params.section) {
    case "variables":
      return "variables";
    case "credentials":
      return "credentials";
    case "settings":
    case "upgrade":
      return "settings";
    default:
      return "runs";
  }
}

/** Where a template's tab lives. */
export function stackTemplatePath(stackId: string, stackTemplateId: string, tab: TemplateTab = "runs"): string {
  return `/stacks/${stackId}/templates/${stackTemplateId}/${tab}`;
}
```

- [ ] **Step 4: Read waiting from the template in `StackTemplateStatusLabel.tsx`**

Add, above `stackTemplateIndicator`:

```ts
// A plan waits while the template has one pending. The attention list says
// so too, but it polls slowly; the template moves with the stack query, which
// the stack's page refreshes as its runs settle.
function isWaiting(stackTemplate: StackTemplate, attention: AttentionItem | undefined): boolean {
  return attention?.kind === "waiting_approval" || stackTemplate.pending_plan_run_id !== "";
}
```

In `stackTemplateIndicator`, change `if (attention?.kind === "waiting_approval") {` to `if (isWaiting(stackTemplate, attention)) {`. In `stackTemplateActivity`, replace the first block with:

```ts
  if (isWaiting(stackTemplate, attention)) {
    const at = (attention?.kind === "waiting_approval" ? attention.at : "") || stackTemplate.pending_plan_at;
    return at ? `Planned ${formatDateTime(at)}` : "Planned";
  }
```

- [ ] **Step 5: Run the tests and see them pass**

Run: `npx vitest run src/features/stacks/templateSelection.test.ts src/features/stacks/StackTemplateStatusLabel.test.tsx src/features/stacks/StacksListScreen.test.tsx`
Expected: PASS. `/stacks` still passes; its fixtures have no pending plan.

- [ ] **Step 6: Commit**

```bash
git add src/features/stacks/templateSelection.ts src/features/stacks/templateSelection.test.ts src/features/stacks/StackTemplateStatusLabel.tsx src/features/stacks/StackTemplateStatusLabel.test.tsx
git commit -m "feat(web): choose a stack's default template and its tab from the URL

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Share the split view's pieces

`/stacks` already draws SearchField, ListItem, Chip, the slug-and-tags line and the no-templates empty state. The stack page needs all of them, so they move out first. Nothing changes on screen.

**Files:**
- Create: `web/src/shared/SearchField.tsx`, `web/src/shared/Chip.tsx`, `web/src/shared/listItemClass.ts`
- Create: `web/src/features/stacks/StackMeta.tsx`, `web/src/features/stacks/NoTemplatesState.tsx`
- Modify: `web/src/features/stacks/StacksListScreen.tsx`, `web/src/features/stacks/StackPreview.tsx`

**Interfaces:**
- Produces: `SearchField({ label, value, onChange, testId? }: { label: string; value: string; onChange: (value: string) => void; testId?: string })`.
- Produces: `Chip({ tone, icon, children }: { tone: "warning" | "destructive"; icon: LucideIcon; children: ReactNode })`.
- Produces: `listItemClass(selected: boolean): string`.
- Produces: `StackMeta({ stack }: { stack: Pick<Stack, "slug" | "tags"> })`.
- Produces: `NoTemplatesState({ stackId, heading }: { stackId: string; heading: "h2" | "h4" })`, carrying `data-testid` from its caller through `testId?: string`.

- [ ] **Step 1: Create `web/src/shared/SearchField.tsx`**

```tsx
import { Search } from "lucide-react";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { cn } from "@/lib/utils";

// openplan UI's SearchField on shadcn's InputGroup: the glass 11px from the
// edge, the text 35px in, and focus drawn as the 2px ring outline every other
// control has rather than the group's translucent ring.
const searchFieldClass = cn(
  "h-9 bg-canvas has-[>[data-align=inline-start]]:[&>input]:pl-2",
  "has-[[data-slot=input-group-control]:focus-visible]:border-input has-[[data-slot=input-group-control]:focus-visible]:ring-0",
  "has-[[data-slot=input-group-control]:focus-visible]:outline-solid has-[[data-slot=input-group-control]:focus-visible]:outline-2",
  "has-[[data-slot=input-group-control]:focus-visible]:outline-offset-2 has-[[data-slot=input-group-control]:focus-visible]:outline-ring"
);

/** A filter box at the top of a list. Its label is also its placeholder, so
    the visible text and the accessible name say the same thing. */
export default function SearchField({
  label,
  value,
  onChange,
  testId
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  testId?: string;
}) {
  return (
    <InputGroup className={searchFieldClass}>
      <InputGroupAddon className="pl-2.5">
        <Search aria-hidden="true" className="text-subtle-foreground" />
      </InputGroupAddon>
      <InputGroupInput
        type="search"
        aria-label={label}
        placeholder={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="placeholder:text-subtle-foreground"
        data-testid={testId}
      />
    </InputGroup>
  );
}
```

- [ ] **Step 2: Create `web/src/shared/Chip.tsx`**

```tsx
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const FILLS = { warning: "bg-warning-soft", destructive: "bg-destructive-soft" } as const;

// openplan UI's Chip: a small filled marker on a parent row saying something
// inside it needs a person. Never for a settled state.
export default function Chip({ tone, icon: Icon, children }: { tone: keyof typeof FILLS; icon: LucideIcon; children: ReactNode }) {
  return (
    <Badge variant={tone} className={cn("gap-1.25 rounded-sm", FILLS[tone])}>
      <Icon aria-hidden="true" strokeWidth={2.25} />
      {children}
    </Badge>
  );
}
```

- [ ] **Step 3: Create `web/src/shared/listItemClass.ts`**

```ts
import { cn } from "@/lib/utils";

/**
 * openplan UI's ListItem: a selectable row in a split view's list, at least
 * 56px tall. Selected, it takes the soft primary fill and border; otherwise
 * it tints on hover. The focus ring is drawn inside, where the list's frame
 * cannot clip it.
 */
export function listItemClass(selected: boolean): string {
  return cn(
    "flex min-h-14 items-center justify-between gap-3 rounded-lg border border-transparent px-3 py-2 text-foreground transition-colors focus-visible:-outline-offset-2",
    selected ? "border-primary/35 bg-primary-soft" : "hover:bg-primary-tint"
  );
}
```

- [ ] **Step 4: Create `web/src/features/stacks/StackMeta.tsx`**

```tsx
import type { Stack } from "../../api/types";

// The line under a stack's name: its slug, then its tags as written, never
// coloured by their value.
export default function StackMeta({ stack }: { stack: Pick<Stack, "slug" | "tags"> }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="font-mono text-xs text-muted-foreground">{stack.slug}</span>
      {Object.entries(stack.tags).map(([key, value]) => (
        <span key={key} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-xs text-tag-foreground">
          {key}: {value}
        </span>
      ))}
    </div>
  );
}
```

- [ ] **Step 5: Create `web/src/features/stacks/NoTemplatesState.tsx`**

```tsx
import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// openplan UI's EmptyState inside a panel, for a stack with no templates: what
// is missing, what to do, and Add template for someone who may.
export default function NoTemplatesState({ stackId, heading: Heading, testId }: { stackId: string; heading: "h2" | "h4"; testId?: string }) {
  return (
    <Empty className="gap-3 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid={testId}>
      <EmptyHeader className="gap-3">
        <Heading className="text-sm font-medium">No templates in this stack yet</Heading>
        <EmptyDescription className="text-meta">Add a template to plan and apply its infrastructure here.</EmptyDescription>
      </EmptyHeader>
      <RequireCapability capability="canOperate" stackId={stackId}>
        <Link to={`/stacks/${stackId}/templates/new`} className={cn(buttonClass("primary"), "pointer-coarse:h-11")}>
          <Plus data-icon="inline-start" aria-hidden="true" />
          Add template
        </Link>
      </RequireCapability>
    </Empty>
  );
}
```

- [ ] **Step 6: Use them on `/stacks`**

In `web/src/features/stacks/StacksListScreen.tsx`:
- delete `searchFieldClass` and its comment; replace the `<InputGroup …>…</InputGroup>` block with `<SearchField label="Filter stacks" value={filter} onChange={setFilter} testId="stacks-filter" />`;
- in `StackRow`, replace the `className={cn(…)}` on the `Link` with `className={listItemClass(selected)}`;
- replace each chip's `<Badge …>…</Badge>` with `<Chip tone="warning" icon={Hourglass}>{waitingChipLabel(attention.waiting)}</Chip>` and `<Chip tone="destructive" icon={TriangleAlert}>{failedChipLabel(attention.failed)}</Chip>`;
- fix the imports: add `SearchField`, `Chip` and `listItemClass`; remove `Search`, `Badge` and the `input-group` import.

In `web/src/features/stacks/StackPreview.tsx`:
- replace the slug-and-tags `<div className="flex flex-wrap items-center gap-2">…</div>` with `<StackMeta stack={stack} />`, and delete the `tags` constant;
- replace the `<Empty …>…</Empty>` empty state with `<NoTemplatesState stackId={stack.id} heading="h4" testId="stack-preview-empty" />`;
- remove the now-unused imports (`Plus`, `Empty`, `EmptyDescription`, `EmptyHeader`).

- [ ] **Step 7: Run the `/stacks` tests and the type check**

Run: `npx vitest run src/features/stacks/StacksListScreen.test.tsx src/features/stacks/StackAttentionScreen.test.tsx && npx tsc -b`
Expected: PASS with no edits to the tests, since the markup, text and test IDs are unchanged.

- [ ] **Step 8: Commit**

```bash
git add src/shared/SearchField.tsx src/shared/Chip.tsx src/shared/listItemClass.ts src/features/stacks/StackMeta.tsx src/features/stacks/NoTemplatesState.tsx src/features/stacks/StacksListScreen.tsx src/features/stacks/StackPreview.tsx
git commit -m "refactor(web): share the split view's pieces between stack pages

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The template panel

`TemplatePanel` replaces `StackTemplateDetailShell`. It draws the header, the underline tabs and the content, provides the template to the tabs through a context, and owns the stack refresh. The router swaps the element in place, so the app keeps working inside the old `StackDetailShell` until Task 5.

**Files:**
- Create: `web/src/features/stacks/stackTemplateContext.ts`
- Create: `web/src/features/runs/useRefreshStackOnRunChange.ts`
- Create: `web/src/features/stacks/TemplateTabs.tsx`
- Create: `web/src/features/stacks/TemplatePanel.tsx`
- Modify: `web/src/features/stacks/TemplateRunsTab.tsx`, `TemplateVariablesTab.tsx`, `TemplateCredentialsTab.tsx`, `TemplateSettingsTab.tsx` (use the context)
- Modify: `web/src/features/runs/TemplateRunActions.tsx` (drop the effect that moved)
- Modify: `web/src/app/router.tsx` (element swap)
- Delete: `web/src/features/stacks/StackTemplateDetailShell.tsx`
- Test: `web/src/features/stacks/StackTemplatePages.test.tsx`

**Interfaces:**
- Consumes: `templateTabOf`, `stackTemplatePath`, `TemplateTab` (Task 2); `stackTemplateIndicator`, `stackTemplateActivity` (Task 2).
- Produces: `export interface StackTemplateContextValue { stackId: string; stackTemplate: StackTemplate }`, `export const StackTemplateContext`, and `export function useStackTemplate(): StackTemplateContextValue`.
- Produces: `export function useRefreshStackOnRunChange(stackId: string, stackTemplateId: string): void`.
- Produces: `TemplateTabs({ stackId, stackTemplateId })` and `TemplatePanel({ stackTemplateId?, children? }: { stackTemplateId?: string; children?: ReactNode })`. With no `children`, it renders `<Outlet />`. With no `stackTemplateId`, it reads the `:stackTemplateId` param. It is `data-testid="template-panel"`, and a missing template is `data-testid="stack-template-missing"`.

- [ ] **Step 1: Point the tests at the panel and write the new ones (failing)**

In `web/src/features/stacks/StackTemplatePages.test.tsx`:

1. Replace `import StackTemplateDetailShell from "./StackTemplateDetailShell";` with `import TemplatePanel from "./TemplatePanel";`, and in `renderAt` use `element={<TemplatePanel />}` on the `/stacks/:stackId/templates/:stackTemplateId` route. Leave the list route and its describe alone; Task 5 removes them.
2. In `seedDefaultData`, and in the two tests that seed only the stack ("shows a loading state…" and `mockVariablesFailure`), add `queryClient.setQueryData(queryKeys.attention("tenant_123"), []);` so the panel's attention read stays off the network.
3. Rename `describe("StackTemplateDetailShell", …)` to `describe("TemplatePanel", …)`. In it, every `screen.getByRole("tab", { name: X })` becomes `screen.getByRole("link", { name: X })`, and every `queryByRole("tab", …)` becomes `queryByRole("link", …)`. Inside the panel, the only links named Runs, Variables, Credentials and Settings are the tabs.
4. Add these tests to that describe:

```tsx
  it("names the template with its state, its ref and what last happened", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(
      queryKeys.stack("tenant_123", "stack_1"),
      stackView(allAllowed, [
        stackTemplate({ display_name: "eks-cluster", source_ref: "v1.4.0", pending_plan_run_id: "run_14", pending_plan_at: "2026-10-03T09:30:00Z" })
      ])
    );

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/runs");

    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-status-st_1").textContent).toBe("waiting for approval");
    expect(screen.getByText("v1.4.0")).toBeTruthy();
    expect(screen.getByText(/^Planned 3 Oct, \d\d:\d\d$/)).toBeTruthy();
  });

  it("marks Settings as the current tab while changing the revision", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    render(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <MemoryRouter initialEntries={["/stacks/stack_1/templates/st_1/upgrade"]}>
            <Routes>
              <Route path="/stacks/:stackId/templates/:stackTemplateId" element={<TemplatePanel />}>
                <Route path="upgrade" element={<p>change revision</p>} />
              </Route>
            </Routes>
          </MemoryRouter>
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(screen.getByRole("link", { name: "Settings" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: "Runs" }).getAttribute("aria-current")).toBeNull();
  });

  // The stack's states change when a run settles, and only the runs query
  // polls. The panel refreshes the stack itself, so the header and the list
  // stay current on every tab, not just Runs.
  it("refreshes the stack when the template's latest run settles, on any tab", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), [runFor("st_1", { status: "completed" })]);
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockImplementation(
        async () =>
          new Response(JSON.stringify(stackView(allAllowed, [stackTemplate()])), { status: 200, headers: { "content-type": "application/json" } })
      );

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");

    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/stacks/stack_1"))).toBe(true));
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `npx vitest run src/features/stacks/StackTemplatePages.test.tsx`
Expected: FAIL, `./TemplatePanel` cannot be resolved.

- [ ] **Step 3: Create `web/src/features/stacks/stackTemplateContext.ts`**

```ts
import { createContext, useContext } from "react";
import type { StackTemplate } from "../../api/types";

export interface StackTemplateContextValue {
  stackId: string;
  stackTemplate: StackTemplate;
}

// The template a panel is about, for the tabs inside it. A React context
// rather than the outlet's: the stack's own index draws the default
// template's Runs tab without a route of its own, so there is no outlet there
// to carry it.
export const StackTemplateContext = createContext<StackTemplateContextValue | null>(null);

export function useStackTemplate(): StackTemplateContextValue {
  const value = useContext(StackTemplateContext);
  if (value === null) {
    throw new Error("useStackTemplate must be used inside a TemplatePanel");
  }
  return value;
}
```

- [ ] **Step 4: Create `web/src/features/runs/useRefreshStackOnRunChange.ts`**

Move the effect out of `TemplateRunActions` into this hook:

```ts
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useTemplateRunsQuery } from "../../api/queries";
import { tenantID } from "../../config";

// plan_state, live_state and the pending plan live on the stack template, but
// what changes them is a run finishing or starting to wait, and only the runs
// query polls. Without this the stack's page would keep the states it loaded:
// the panel's header and the list's chips. The panel calls it, so it runs on
// every tab.
export function useRefreshStackOnRunChange(stackId: string, stackTemplateId: string): void {
  const queryClient = useQueryClient();
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplateId);
  const latestRun = runsQuery.status === "success" ? runsQuery.data[0] ?? null : null;
  const settledRun = latestRun && isTerminalRunStatus(latestRun.status) ? `${latestRun.id}:${latestRun.status}` : "";
  const waitingRun = latestRun?.status === "waiting_approval" ? latestRun.id : "";

  useEffect(() => {
    if (settledRun === "" && waitingRun === "") {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackId) });
  }, [settledRun, waitingRun, stackId, queryClient]);
}
```

In `web/src/features/runs/TemplateRunActions.tsx`, delete the `latestRun`, `settledRun`, `waitingRun` constants and the `useEffect` with its comment. Remove `useEffect` from the React import if nothing else uses it, and update the file's leading comment: the stack refresh now lives in `useRefreshStackOnRunChange`.

- [ ] **Step 5: Create `web/src/features/stacks/TemplateTabs.tsx`**

```tsx
import { Link, useLocation } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { cn } from "@/lib/utils";
import { stackTemplatePath, templateTabOf } from "./templateSelection";
import type { TemplateTab } from "./templateSelection";

// openplan UI's tabs as underline links, one per route below the template.
// The route decides the current tab, so a run keeps Runs lit and Change
// revision keeps Settings lit. Credentials shows only to people who may
// manage access, the capability its route requires.
export default function TemplateTabs({ stackId, stackTemplateId }: { stackId: string; stackTemplateId: string }) {
  const current = templateTabOf(useLocation().pathname);

  const tab = (value: TemplateTab, label: string) => (
    <Link
      to={stackTemplatePath(stackId, stackTemplateId, value)}
      aria-current={value === current ? "page" : undefined}
      className={cn(
        "-mb-px inline-flex h-10 items-center border-b-2 text-sm font-medium transition-colors focus-visible:-outline-offset-2 pointer-coarse:h-11",
        value === current ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
      )}
    >
      {label}
    </Link>
  );

  return (
    <nav aria-label="Template sections" className="flex flex-wrap items-center gap-x-6">
      {tab("runs", "Runs")}
      {tab("variables", "Variables")}
      <RequireCapability capability="canManageAccess" stackId={stackId}>
        {tab("credentials", "Credentials")}
      </RequireCapability>
      {tab("settings", "Settings")}
    </nav>
  );
}
```

- [ ] **Step 6: Create `web/src/features/stacks/TemplatePanel.tsx`**

```tsx
import type { ReactNode } from "react";
import { Outlet, useParams } from "react-router-dom";
import { useAttentionQuery, useStackQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { useRefreshStackOnRunChange } from "../runs/useRefreshStackOnRunChange";
import { attentionByStackTemplate } from "./attention";
import { StackTemplateContext } from "./stackTemplateContext";
import StackTemplateStatusLabel, { stackTemplateActivity } from "./StackTemplateStatusLabel";
import { findSelectedStackTemplate, stackTemplateLabel } from "./stackWorkflow";
import TemplateTabs from "./TemplateTabs";

// The right side of the stack's page: one template, its state and its tabs.
// It is the template's own page, inside the stack's. The route below it draws
// the tab into <Outlet />, except on the stack's index, which passes the
// default template's Runs tab as children.
//
// The stack's route guard has already loaded the stack, so the template is
// read from cache. The content is keyed on the template, so a tab's local
// state, such as unsaved variable edits, never carries over to another.
export default function TemplatePanel({ stackTemplateId, children }: { stackTemplateId?: string; children?: ReactNode }) {
  const params = useParams<{ stackId: string; stackTemplateId: string }>();
  const stackId = params.stackId ?? "";
  const id = stackTemplateId ?? params.stackTemplateId ?? "";
  const templates = useStackQuery(tenantID, stackId).data?.templates ?? [];
  const stackTemplate = findSelectedStackTemplate(templates, id);
  // The attention list dates a failed destroy, which the template cannot.
  const attention = attentionByStackTemplate(useAttentionQuery(tenantID).data ?? []).get(id);
  useRefreshStackOnRunChange(stackId, stackTemplate?.id ?? "");

  if (!stackTemplate) {
    return (
      <p className="px-7 py-6 text-meta text-muted-foreground" data-testid="stack-template-missing">
        That template is not installed on this stack.
      </p>
    );
  }

  const activity = stackTemplateActivity(stackTemplate, attention);
  return (
    <StackTemplateContext.Provider value={{ stackId, stackTemplate }}>
      <div className="flex min-w-0 flex-col" data-testid="template-panel">
        <div className="flex flex-col gap-5 border-b border-divider px-7 pt-6">
          <div className="flex min-w-0 flex-col gap-2">
            <h2 className="font-heading text-panel-title font-semibold tracking-title wrap-anywhere">{stackTemplateLabel(stackTemplate)}</h2>
            <div className="flex flex-wrap items-center gap-2 text-meta text-muted-foreground">
              <StackTemplateStatusLabel stackTemplate={stackTemplate} attention={attention} />
              <span aria-hidden="true" className="text-separator">
                ·
              </span>
              <span className="font-mono text-xs">{stackTemplate.source_ref}</span>
              {activity && (
                <>
                  <span aria-hidden="true" className="text-separator">
                    ·
                  </span>
                  <span>{activity}</span>
                </>
              )}
            </div>
          </div>
          <TemplateTabs stackId={stackId} stackTemplateId={stackTemplate.id} />
        </div>
        <div key={stackTemplate.id} className="flex min-w-0 flex-col gap-5 px-7 pt-5 pb-7">
          {children ?? <Outlet />}
        </div>
      </div>
    </StackTemplateContext.Provider>
  );
}
```

- [ ] **Step 7: Use the context in the four tabs**

In each of `TemplateRunsTab.tsx`, `TemplateVariablesTab.tsx`, `TemplateCredentialsTab.tsx` and `TemplateSettingsTab.tsx`, replace `import { useStackTemplateOutlet } from "./StackTemplateDetailShell";` with `import { useStackTemplate } from "./stackTemplateContext";`, and `useStackTemplateOutlet()` with `useStackTemplate()`. Update each file's leading comment if it names the shell.

- [ ] **Step 8: Swap the element in `web/src/app/router.tsx` and delete the old shell**

Replace `import StackTemplateDetailShell from "../features/stacks/StackTemplateDetailShell";` with `import TemplatePanel from "../features/stacks/TemplatePanel";` and the route's `element: <StackTemplateDetailShell />` with `element: <TemplatePanel />`. Then:

```bash
git rm src/features/stacks/StackTemplateDetailShell.tsx
```

- [ ] **Step 9: Run the tests and see them pass**

Run: `npx vitest run src/features/stacks src/features/runs src/app && npx tsc -b`
Expected: PASS. `StackDetailShell.test.tsx` still passes: it reads the old stack shell, which hides its own tabs on a template's page.

A test that seeds a settled or waiting latest run now makes the panel refetch the stack. If one of them fails because that unmocked fetch errors, stub it in that test with `vi.spyOn(globalThis, "fetch")`: answer URLs ending `/stacks/stack_1` with the seeded `stackView`, and leave anything else pending (`new Promise(() => {})`).

- [ ] **Step 10: Commit**

```bash
git add -A src/features/stacks src/features/runs src/app/router.tsx
git commit -m "feat(web): show a template in a panel with underline tabs

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: The stack page

**Files:**
- Create: `web/src/features/stacks/StackTemplateList.tsx`
- Create: `web/src/features/stacks/StackPage.tsx`
- Create: `web/src/features/stacks/StackIndexPanel.tsx`
- Create: `web/src/features/stacks/AddTemplatePanel.tsx`
- Create: `web/src/features/stacks/StackSectionLayout.tsx`
- Create: `web/src/features/stacks/StackPage.test.tsx`
- Modify: `web/src/app/router.tsx`, `web/src/app/router.test.tsx`
- Modify: `web/src/features/stacks/StackTemplatePages.test.tsx` (drop the list's describe and route)
- Modify: `web/src/features/stacks/StackPreview.tsx` (Templates links to the stack)
- Modify: `web/src/dev/StyleGuide.tsx` (a hint names RouteTabs)
- Delete: `StackDetailShell.tsx`, `StackDetailShell.test.tsx`, `StackTemplateListScreen.tsx`, `web/src/app/RoutePlaceholder.tsx`, `RoutePlaceholder.test.tsx`, `web/src/shared/RouteTabs.tsx`, `RouteTabs.test.tsx`

**Interfaces:**
- Consumes: `defaultStackTemplate`, `templateTabOf`, `stackTemplatePath` (Task 2); `SearchField`, `Chip`, `listItemClass`, `StackMeta`, `NoTemplatesState` (Task 3); `TemplatePanel` (Task 4); `TemplateRunsTab` (existing).
- Produces: `StackPage` (layout route, `data-testid="stack-page"`; columns `stack-list-column` and `stack-panel-column`); `StackTemplateList({ stackId, templates, selectedId, adding, attention })`; `StackIndexPanel`; `AddTemplatePanel`; `StackSectionLayout`. Test IDs: `stack-template-link-<id>`, `add-stack-template-link`, `stack-templates-none`, `stack-templates-filter-empty`, `stack-empty`.

- [ ] **Step 1: Write the failing page tests**

Create `web/src/features/stacks/StackPage.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, redirect, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { StackCapabilities } from "../../auth/types";
import { queryKeys } from "../../api/queryKeys";
import type { AttentionItem, StackTemplate, StackView } from "../../api/types";
import StackIndexPanel from "./StackIndexPanel";
import StackPage from "./StackPage";
import TemplatePanel from "./TemplatePanel";

// The stack's page through the same route shape as router.tsx, with each
// template tab stubbed, so these tests are about the frame: the header, the
// list, the selection and the phone layout. The tabs have their own tests.

const TENANT = "tenant_123";
const allAllowed: StackCapabilities = { canView: true, canOperate: true, canApprove: true, canManageAccess: true };

function template(id: string, overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id,
    stack_id: "stack_1",
    component_key: id,
    source_template_id: "source_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "rev_1",
    source_ref: "v1.0.0",
    workspace_name: id,
    display_name: id,
    config: {},
    last_applied_run_id: "",
    last_applied_at: "2026-09-22T10:00:00Z",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "matches",
    created_by: "user_1",
    lifecycle: "active",
    ...overrides
  };
}

const network = template("network");
const eks = template("eks-cluster", { source_ref: "v1.4.0", pending_plan_run_id: "run_14", pending_plan_at: "2026-10-03T09:30:00Z" });
const cdn = template("cloudfront", { lifecycle: "failed" });

function view(templates: StackTemplate[], capabilities: StackCapabilities = allAllowed): StackView {
  return {
    stack: {
      id: "stack_1",
      tenant_id: TENANT,
      name: "Payments",
      slug: "payments",
      tags: { env: "production" },
      default_credential_ids: [],
      created_by: "user_1",
      created_at: "2026-07-19T00:00:00Z",
      effectiveCapabilities: capabilities
    },
    templates
  };
}

const failedItem: AttentionItem = {
  kind: "destroy_failed",
  at: "2026-09-29T08:00:00Z",
  stack: { id: "stack_1", name: "Payments", slug: "payments" },
  stack_template: { id: "cloudfront", workspace_name: "cloudfront", display_name: "cloudfront" },
  run: null
};

function seed(templates: StackTemplate[], capabilities: StackCapabilities = allAllowed): QueryClient {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(queryKeys.stack(TENANT, "stack_1"), view(templates, capabilities));
  queryClient.setQueryData(queryKeys.attention(TENANT), [failedItem]);
  for (const stackTemplate of templates) {
    queryClient.setQueryData(queryKeys.templateRuns(TENANT, stackTemplate.id), []);
  }
  return queryClient;
}

function authValue(): AuthContextValue {
  return {
    me: { sub: "user_1", tenantID: TENANT, displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

function renderPage(queryClient: QueryClient, path: string) {
  const router = createMemoryRouter(
    [
      {
        path: "/stacks/:stackId",
        element: <StackPage />,
        children: [
          { index: true, element: <StackIndexPanel /> },
          { path: "templates", loader: ({ params }) => redirect(`/stacks/${params.stackId}`) },
          { path: "templates/new", element: <p data-testid="add-template-content">add</p> },
          {
            path: "templates/:stackTemplateId",
            element: <TemplatePanel />,
            children: [
              { path: "runs", element: <p data-testid="runs-content">runs</p> },
              { path: "runs/:runNumber", element: <p data-testid="run-content">run</p> },
              { path: "variables", element: <p data-testid="variables-content">variables</p> },
              { path: "settings", element: <p data-testid="settings-content">settings</p> },
              { path: "upgrade", element: <p data-testid="upgrade-content">upgrade</p> }
            ]
          }
        ]
      }
    ],
    { initialEntries: [path] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

beforeEach(() => {
  // Everything is seeded; anything else stays pending rather than reaching out.
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("StackPage header", () => {
  it("titles the page with the stack, a trail back to Stacks, its slug and its tags", () => {
    renderPage(seed([network]), "/stacks/stack_1");

    expect(screen.getByRole("heading", { level: 1, name: "Payments" })).toBeTruthy();
    expect(within(screen.getByRole("navigation", { name: "Breadcrumb" })).getByRole("link", { name: "Stacks" }).getAttribute("href")).toBe("/stacks");
    expect(screen.getByText("payments")).toBeTruthy();
    expect(screen.getByText("env: production")).toBeTruthy();
  });

  it("offers Environment and Access only to people who may manage access", () => {
    renderPage(seed([network]), "/stacks/stack_1");
    expect(screen.getByRole("link", { name: "Environment" }).getAttribute("href")).toBe("/stacks/stack_1/environment");
    expect(screen.getByRole("link", { name: "Access" }).getAttribute("href")).toBe("/stacks/stack_1/access");
    cleanup();

    renderPage(seed([network], { ...allAllowed, canManageAccess: false }), "/stacks/stack_1");
    expect(screen.queryByRole("link", { name: "Environment" })).toBeNull();
    expect(screen.queryByRole("link", { name: "Access" })).toBeNull();
  });
});

describe("StackPage selection", () => {
  it("opens on the first template with a plan waiting, without changing the URL", () => {
    const router = renderPage(seed([network, cdn, eks]), "/stacks/stack_1");

    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-link-eks-cluster").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("stack-template-link-network").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("template-runs-tab")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/stacks/stack_1");
  });

  it("falls back to a failed destroy, then to the first template", () => {
    renderPage(seed([network, cdn]), "/stacks/stack_1");
    expect(screen.getByRole("heading", { level: 2, name: "cloudfront" })).toBeTruthy();
    cleanup();

    renderPage(seed([network, template("rds")]), "/stacks/stack_1");
    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
  });

  it("selects the template in the URL", () => {
    renderPage(seed([network, eks]), "/stacks/stack_1/templates/network/variables");

    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
    expect(screen.getByTestId("stack-template-link-network").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("variables-content")).toBeTruthy();
  });

  // Picking another template keeps the tab; from a run it lands on Runs, and
  // from Change revision on Settings.
  it.each([
    ["/stacks/stack_1/templates/network/variables", "/stacks/stack_1/templates/eks-cluster/variables"],
    ["/stacks/stack_1/templates/network/runs/3", "/stacks/stack_1/templates/eks-cluster/runs"],
    ["/stacks/stack_1/templates/network/upgrade", "/stacks/stack_1/templates/eks-cluster/settings"],
    ["/stacks/stack_1/templates/new", "/stacks/stack_1/templates/eks-cluster/runs"]
  ])("from %s, another template's row opens %s", (from, to) => {
    renderPage(seed([network, eks]), from);
    expect(screen.getByTestId("stack-template-link-eks-cluster").getAttribute("href")).toBe(to);
  });

  it("says so when the URL names a template the stack does not have, and keeps the list", () => {
    renderPage(seed([network, eks]), "/stacks/stack_1/templates/gone/runs");

    expect(screen.getByTestId("stack-template-missing").textContent).toBe("That template is not installed on this stack.");
    expect(screen.getByTestId("stack-template-link-network")).toBeTruthy();
  });

  it("sends the old template list to the stack's page", async () => {
    const router = renderPage(seed([network]), "/stacks/stack_1/templates");
    await waitFor(() => expect(router.state.location.pathname).toBe("/stacks/stack_1"));
  });
});

describe("StackPage list", () => {
  it("shows each template's ref and what last happened, and chips only for what needs a person", () => {
    renderPage(seed([network, eks, cdn]), "/stacks/stack_1");

    const eksRow = screen.getByTestId("stack-template-link-eks-cluster");
    expect(eksRow.textContent).toContain("v1.4.0");
    expect(eksRow.textContent).toMatch(/Planned 3 Oct, \d\d:\d\d/);
    expect(within(eksRow).getByText("plan to approve")).toBeTruthy();

    const cdnRow = screen.getByTestId("stack-template-link-cloudfront");
    expect(within(cdnRow).getByText("destroy failed")).toBeTruthy();
    expect(cdnRow.textContent).toMatch(/Failed 29 Sept?, \d\d:\d\d/);

    const networkRow = screen.getByTestId("stack-template-link-network");
    expect(networkRow.textContent).toMatch(/Applied 22 Sept? 2026/);
    expect(within(networkRow).queryByText(/plan to approve|destroy failed/)).toBeNull();
  });

  it("narrows the list as the person types, keeps the selection, and says when nothing matches", () => {
    renderPage(seed([network, eks]), "/stacks/stack_1");
    const filter = screen.getByRole("searchbox", { name: "Filter templates" });

    fireEvent.change(filter, { target: { value: "net" } });
    expect(screen.getByTestId("stack-template-link-network")).toBeTruthy();
    expect(screen.queryByTestId("stack-template-link-eks-cluster")).toBeNull();
    // The filter narrows the list, not the selection.
    expect(screen.getByRole("heading", { level: 2, name: "eks-cluster" })).toBeTruthy();

    fireEvent.change(filter, { target: { value: "zzz" } });
    expect(screen.getByTestId("stack-templates-filter-empty").textContent).toBe("No templates match this filter.");
  });

  it("ends the list with Add template for operators, selected while adding", () => {
    renderPage(seed([network]), "/stacks/stack_1");
    const add = screen.getByTestId("add-stack-template-link");
    expect(add.getAttribute("href")).toBe("/stacks/stack_1/templates/new");
    expect(add.textContent).toBe("Add template");
    cleanup();

    renderPage(seed([network]), "/stacks/stack_1/templates/new");
    expect(screen.getByTestId("add-stack-template-link").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("add-stack-template-link").textContent).toBe("New template");
    expect(screen.getByTestId("stack-template-link-network").getAttribute("aria-current")).toBeNull();
    cleanup();

    renderPage(seed([network], { ...allAllowed, canOperate: false }), "/stacks/stack_1");
    expect(screen.queryByTestId("add-stack-template-link")).toBeNull();
  });

  it("says a stack with no templates has none, and offers to add one", () => {
    renderPage(seed([]), "/stacks/stack_1");

    expect(screen.getByTestId("stack-templates-none").textContent).toBe("No templates in this stack yet.");
    expect(screen.getByTestId("stack-empty")).toBeTruthy();
    expect(screen.getAllByRole("link", { name: /Add template/ })).toHaveLength(2);
  });
});

// On a phone the page shows the list or the panel, never both. This is CSS,
// so these two tests read the classes that switch it.
describe("StackPage on a phone", () => {
  it("shows only the list on the stack's own path", () => {
    renderPage(seed([network]), "/stacks/stack_1");

    expect(screen.getByTestId("stack-list-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("stack-panel-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.queryByRole("link", { name: "Templates" })).toBeNull();
  });

  it("shows only the panel on a template's path, with a way back to the list", () => {
    renderPage(seed([network]), "/stacks/stack_1/templates/network/runs");

    expect(screen.getByTestId("stack-list-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("stack-panel-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByRole("link", { name: "Templates" }).getAttribute("href")).toBe("/stacks/stack_1");
  });
});
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `npx vitest run src/features/stacks/StackPage.test.tsx`
Expected: FAIL, `./StackIndexPanel` and `./StackPage` cannot be resolved.

- [ ] **Step 3: Create `web/src/features/stacks/StackTemplateList.tsx`**

```tsx
import { Hourglass, Plus, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import type { AttentionItem, StackTemplate } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import Chip from "../../shared/Chip";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import { cn } from "@/lib/utils";
import { stackTemplateActivity } from "./StackTemplateStatusLabel";
import { stackTemplatePath, templateTabOf } from "./templateSelection";
import { stackTemplateLabel } from "./stackWorkflow";

// The left side of the stack's page: its templates in the API's order,
// narrowed as the person types, each a link to the same tab of that template.
// Add template ends the list for people who may add one.
//
// Rows push a history entry, unlike /stacks: a selection here is a working
// page, and on a phone its own screen, so Back returns to the list.
export default function StackTemplateList({
  stackId,
  templates,
  selectedId,
  adding,
  attention
}: {
  stackId: string;
  templates: StackTemplate[];
  /** The template the panel shows, if any. */
  selectedId: string | null;
  /** On templates/new, where Add template is the current row. */
  adding: boolean;
  /** Each template's attention item, which dates a failed destroy. */
  attention: Map<string, AttentionItem>;
}) {
  const [filter, setFilter] = useState("");
  const tab = templateTabOf(useLocation().pathname);
  const query = filter.trim().toLowerCase();
  const visible = query === "" ? templates : templates.filter((stackTemplate) => stackTemplateLabel(stackTemplate).toLowerCase().includes(query));

  return (
    <div className="flex min-w-0 flex-col">
      <div className="border-b border-divider p-3">
        <SearchField label="Filter templates" value={filter} onChange={setFilter} testId="stack-templates-filter" />
      </div>
      <div className="flex flex-col gap-0.5 p-2">
        {templates.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="stack-templates-none">
            No templates in this stack yet.
          </p>
        ) : visible.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="stack-templates-filter-empty">
            No templates match this filter.
          </p>
        ) : (
          <ul className="flex flex-col gap-0.5" aria-label="Templates">
            {visible.map((stackTemplate) => (
              <li key={stackTemplate.id}>
                <StackTemplateRow
                  stackTemplate={stackTemplate}
                  to={stackTemplatePath(stackId, stackTemplate.id, tab)}
                  selected={!adding && stackTemplate.id === selectedId}
                  attention={attention.get(stackTemplate.id)}
                />
              </li>
            ))}
          </ul>
        )}
        <RequireCapability capability="canOperate" stackId={stackId}>
          <Link
            to={`/stacks/${stackId}/templates/new`}
            aria-current={adding ? "true" : undefined}
            className={cn(listItemClass(adding), "min-h-11 justify-start gap-2 text-sm font-medium", adding ? "text-primary-strong" : "text-primary")}
            data-testid="add-stack-template-link"
          >
            <Plus aria-hidden="true" className="size-4 shrink-0" />
            {adding ? "New template" : "Add template"}
          </Link>
        </RequireCapability>
      </div>
    </div>
  );
}

// One template: its name, then its ref and what last happened, and a chip
// only when something on it needs a person.
function StackTemplateRow({
  stackTemplate,
  to,
  selected,
  attention
}: {
  stackTemplate: StackTemplate;
  to: string;
  selected: boolean;
  attention: AttentionItem | undefined;
}) {
  const label = stackTemplateLabel(stackTemplate);
  const activity = stackTemplateActivity(stackTemplate, attention);
  const waiting = stackTemplate.pending_plan_run_id !== "";
  const failed = stackTemplate.lifecycle === "failed";

  return (
    <Link to={to} aria-current={selected ? "true" : undefined} className={listItemClass(selected)} data-testid={`stack-template-link-${stackTemplate.id}`}>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate font-mono text-meta leading-label font-medium", selected && "text-primary-strong")} title={label}>
          {label}
        </span>
        <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span className="shrink-0 font-mono">{stackTemplate.source_ref}</span>
          {activity && (
            <>
              <span aria-hidden="true" className="text-separator">
                ·
              </span>
              <span className="truncate">{activity}</span>
            </>
          )}
        </span>
      </span>
      {(waiting || failed) && (
        <span className="flex shrink-0 flex-col items-end gap-1">
          {waiting && (
            <Chip tone="warning" icon={Hourglass}>
              plan to approve
            </Chip>
          )}
          {failed && (
            <Chip tone="destructive" icon={TriangleAlert}>
              destroy failed
            </Chip>
          )}
        </span>
      )}
    </Link>
  );
}
```

- [ ] **Step 4: Create `web/src/features/stacks/StackIndexPanel.tsx`**

```tsx
import { useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import { tenantID } from "../../config";
import NoTemplatesState from "./NoTemplatesState";
import TemplatePanel from "./TemplatePanel";
import TemplateRunsTab from "./TemplateRunsTab";
import { defaultStackTemplate } from "./templateSelection";

// /stacks/:stackId itself: the default template's panel on its Runs tab,
// drawn here rather than redirected to, so the URL stays the stack's and a
// phone can show the list at it. A stack with no templates says so.
export default function StackIndexPanel() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const templates = useStackQuery(tenantID, stackId).data?.templates ?? [];
  const selected = defaultStackTemplate(templates);

  if (!selected) {
    return (
      <div className="p-7">
        <NoTemplatesState stackId={stackId} heading="h2" testId="stack-empty" />
      </div>
    );
  }
  return (
    <TemplatePanel stackTemplateId={selected.id}>
      <TemplateRunsTab />
    </TemplatePanel>
  );
}
```

- [ ] **Step 5: Create `web/src/features/stacks/AddTemplatePanel.tsx`**

```tsx
import AddStackTemplateScreen from "./AddStackTemplateScreen";

// templates/new in the stack's panel. The screen inside keeps its own look
// until it is rebuilt on openplan UI; this gives it the panel's padding.
export default function AddTemplatePanel() {
  return (
    <div className="flex min-w-0 flex-col px-7 pt-6 pb-7">
      <AddStackTemplateScreen />
    </div>
  );
}
```

- [ ] **Step 6: Create `web/src/features/stacks/StackPage.tsx`**

```tsx
import { ArrowLeft, KeyRound, Users } from "lucide-react";
import { Link, Outlet, useMatch, useParams } from "react-router-dom";
import { useAttentionQuery, useStackQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import PageHeader from "../../shared/PageHeader";
import { cn } from "@/lib/utils";
import { attentionByStackTemplate } from "./attention";
import StackMeta from "./StackMeta";
import StackTemplateList from "./StackTemplateList";
import { defaultStackTemplate } from "./templateSelection";

const sectionLinkClass = cn(buttonClass("outline", "lg"), "pointer-coarse:h-11");

// /stacks/:stackId: the stack's one page. Its templates on the left; on the
// right, the selected template's panel, which is the route below rendered
// into <Outlet />. With no template in the URL, the index draws the default
// one (see defaultStackTemplate) without changing the URL.
//
// On a phone the page shows the list or the panel, never both: the list on
// the stack's own path, the panel on a template's, with a link back. Each
// column hides below md on the paths where it gives way; nothing measures
// the screen.
//
// The canView guard above this route has loaded the stack.
export default function StackPage() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackView = useStackQuery(tenantID, stackId).data;
  const attention = attentionByStackTemplate((useAttentionQuery(tenantID).data ?? []).filter((item) => item.stack.id === stackId));
  const templateMatch = useMatch("/stacks/:stackId/templates/:stackTemplateId/*");

  if (!stackView) {
    return null;
  }

  const { stack, templates } = stackView;
  const onPanel = templateMatch !== null;
  const adding = templateMatch?.params.stackTemplateId === "new";
  const selectedId = onPanel ? (adding ? null : templateMatch.params.stackTemplateId ?? null) : defaultStackTemplate(templates)?.id ?? null;

  return (
    <section data-testid="stack-page">
      <PageHeader
        title={stack.name}
        trail={[{ label: "Stacks", to: "/stacks" }]}
        action={
          <RequireCapability capability="canManageAccess" stackId={stackId}>
            <div className="flex flex-wrap items-center gap-2">
              <Link to={`/stacks/${stackId}/environment`} className={sectionLinkClass}>
                <KeyRound data-icon="inline-start" aria-hidden="true" />
                Environment
              </Link>
              <Link to={`/stacks/${stackId}/access`} className={sectionLinkClass}>
                <Users data-icon="inline-start" aria-hidden="true" />
                Access
              </Link>
            </div>
          </RequireCapability>
        }
      >
        <StackMeta stack={stack} />
      </PageHeader>
      <div className="flex flex-col overflow-clip rounded-panel border bg-card md:flex-row">
        <div
          className={cn("min-w-0 flex-col md:flex md:w-90 md:shrink-0 md:border-r", onPanel ? "hidden" : "flex")}
          data-testid="stack-list-column"
        >
          <StackTemplateList stackId={stackId} templates={templates} selectedId={selectedId} adding={adding} attention={attention} />
        </div>
        <div className={cn("min-w-0 flex-1 flex-col md:flex", onPanel ? "flex" : "hidden")} data-testid="stack-panel-column">
          {onPanel && (
            <Link
              to={`/stacks/${stackId}`}
              className="flex min-h-11 items-center gap-1.5 self-start px-7 pt-4 text-meta font-medium text-primary hover:underline md:hidden"
            >
              <ArrowLeft aria-hidden="true" className="size-3.5" />
              Templates
            </Link>
          )}
          <Outlet />
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 7: Run the page tests and see them pass**

Run: `npx vitest run src/features/stacks/StackPage.test.tsx`
Expected: PASS. If "Failed 29 Sept" fails on your ICU (some print "Sep"), the regex already allows both.

- [ ] **Step 8: Create `web/src/features/stacks/StackSectionLayout.tsx`**

```tsx
import { matchPath, Outlet, useLocation, useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";

// /stacks/:stackId/environment and /access keep their own pages until they
// are redesigned: the shared breadcrumb back to the stack, then the screen.
export default function StackSectionLayout() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stack = useStackQuery(tenantID, stackId).data?.stack;
  const section = matchPath("/stacks/:stackId/access", useLocation().pathname) ? "Access" : "Environment";

  return (
    <section data-testid="stack-section-layout">
      <Breadcrumb
        items={[
          { label: "Stacks", to: "/stacks" },
          { label: stack?.name ?? stackId, to: `/stacks/${stackId}` },
          { label: section }
        ]}
      />
      <Outlet />
    </section>
  );
}
```

- [ ] **Step 9: Rewire `web/src/app/router.tsx`**

Replace the imports of `RoutePlaceholder`, `StackDetailShell` and `StackTemplateListScreen` with:

```tsx
import StackPage from "../features/stacks/StackPage";
import StackIndexPanel from "../features/stacks/StackIndexPanel";
import AddTemplatePanel from "../features/stacks/AddTemplatePanel";
import StackSectionLayout from "../features/stacks/StackSectionLayout";
```

Remove the `AddStackTemplateScreen` import; `AddTemplatePanel` renders it. Replace the whole `stacks/:stackId` route object with:

```tsx
          {
            path: "stacks/:stackId",
            element: <RequireCapability capability="canView" mode="route" />,
            children: [
              // The stack's one page: its templates, and the selected one's
              // panel, which every route below draws into.
              {
                element: <StackPage />,
                handle: canvas,
                children: [
                  { index: true, element: <StackIndexPanel /> },
                  // The old template list is the stack's page now.
                  { path: "templates", loader: ({ params }) => redirect(`/stacks/${params.stackId}`) },
                  {
                    path: "templates/new",
                    element: <RequireCapability capability="canOperate" mode="route" />,
                    children: [{ index: true, element: <AddTemplatePanel /> }]
                  },
                  // A run nests under runs/ so Runs stays lit while reading
                  // one, and Change revision under the template so Settings
                  // does; the index sends you to Runs.
                  {
                    path: "templates/:stackTemplateId",
                    element: <TemplatePanel />,
                    children: [
                      { index: true, element: <Navigate to="runs" replace /> },
                      { path: "runs", element: <TemplateRunsTab /> },
                      { path: "runs/:runNumber", element: <RunDetailScreen /> },
                      { path: "variables", element: <TemplateVariablesTab /> },
                      {
                        path: "credentials",
                        element: <RequireCapability capability="canManageAccess" mode="route" />,
                        children: [{ index: true, element: <TemplateCredentialsTab /> }]
                      },
                      { path: "settings", element: <TemplateSettingsTab /> },
                      {
                        path: "upgrade",
                        element: <RequireCapability capability="canOperate" mode="route" />,
                        children: [{ index: true, element: <UpgradeStackTemplateScreen /> }]
                      }
                    ]
                  }
                ]
              },
              // Environment and Access keep their pages, under a breadcrumb.
              {
                element: <StackSectionLayout />,
                children: [
                  {
                    path: "environment",
                    element: <RequireCapability capability="canManageAccess" mode="route" />,
                    children: [{ index: true, element: <EnvironmentScreen /> }]
                  },
                  {
                    path: "access",
                    element: <RequireCapability capability="canManageAccess" mode="route" />,
                    children: [{ index: true, element: <StackAccessScreen /> }]
                  }
                ]
              }
            ]
          },
```

Delete the comment paragraph "Routes still rendering RoutePlaceholder are reserved slots…" through its spec reference, keeping the sentence about capability guards.

- [ ] **Step 10: Point StackPreview's Templates link at the stack**

In `web/src/features/stacks/StackPreview.tsx`, change `<Link to={`${base}/templates`} className={sectionLinkClass}>` to `<Link to={base} className={sectionLinkClass}>`.

- [ ] **Step 11: Delete what nothing uses**

```bash
git rm src/features/stacks/StackDetailShell.tsx src/features/stacks/StackDetailShell.test.tsx \
  src/features/stacks/StackTemplateListScreen.tsx \
  src/app/RoutePlaceholder.tsx src/app/RoutePlaceholder.test.tsx \
  src/shared/RouteTabs.tsx src/shared/RouteTabs.test.tsx
rg -n "RouteTabs|RoutePlaceholder|StackDetailShell|StackTemplateListScreen" src
```

The last command should print only the hint in `src/dev/StyleGuide.tsx`. Change that hint from `"line variant; RouteTabs renders each tab as a link"` to `"line variant"`.

- [ ] **Step 12: Update `web/src/features/stacks/StackTemplatePages.test.tsx`**

Delete `describe("StackTemplateListScreen", …)` with all its tests, the `<Route path="/stacks/:stackId/templates" …>` line in `renderAt`, and the `StackTemplateListScreen` import. Its behaviours (links, Add template, empty, gating) are covered by `StackPage.test.tsx`.

- [ ] **Step 13: Update `web/src/app/router.test.tsx`**

1. In "renders a placeholder for every reserved screen a signed-in operator can reach", keep the `/stacks/new` block and delete everything after it (the stack-scoped placeholder loop). Rename the test "renders the create stack screen at /stacks/new".
2. Add this helper below `authValue` at the top of the file. It is the stack seeding every stack-route test there repeats:

```tsx
function stackQueryClient(
  queryKeys: typeof import("../api/queryKeys").queryKeys,
  options: { capabilities?: { canView: boolean; canOperate: boolean; canApprove: boolean; canManageAccess: boolean }; templates?: unknown[] } = {}
): QueryClient {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
    stack: {
      id: "stack_1",
      tenant_id: "tenant_123",
      name: "Stack",
      slug: "stack",
      tags: {},
      default_credential_ids: [],
      created_by: "user_123",
      created_at: "2026-07-19T00:00:00Z",
      effectiveCapabilities: options.capabilities ?? { canView: true, canOperate: true, canApprove: true, canManageAccess: true }
    },
    templates: options.templates ?? []
  });
  queryClient.setQueryData(queryKeys.attention("tenant_123"), []);
  return queryClient;
}
```

   Then replace "renders the stack template list at /stacks/:stackId/templates" with these two tests:

```tsx
  it("renders the stack's page at /stacks/:stackId", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { queryKeys } = await import("../api/queryKeys");
    const queryClient = stackQueryClient(queryKeys);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="stack-page"');
    expect(markup).toContain('data-testid="stack-empty"');
  });

  it("sends the old template list at /stacks/:stackId/templates to the stack's page", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/templates"] });
    await vi.waitFor(() => expect(testRouter.state.initialized).toBe(true));

    expect(testRouter.state.location.pathname).toBe("/stacks/stack_1");
  });
```

   The redirect is a loader, so the second test needs no rendering.
3. The panel draws its outlet only for a template the stack has, so the two upgrade tests need one. Add this constant next to the helper:

```tsx
const installedTemplate = {
  id: "st_1",
  stack_id: "stack_1",
  component_key: "vpc",
  source_template_id: "tmpl_src_1",
  desired_template_revision_id: "rev_1",
  last_applied_template_revision_id: "",
  source_ref: "main",
  workspace_name: "ws",
  display_name: "",
  config: {},
  last_applied_run_id: "",
  pending_plan_run_id: "",
  plan_state: "none",
  live_state: "never",
  created_by: "user_123",
  lifecycle: "active"
};
```

   In "renders the upgrade screen at /stacks/:stackId/templates/:stackTemplateId/upgrade", replace its query client setup with `const queryClient = stackQueryClient(queryKeys, { templates: [installedTemplate] }); queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), []);`. Replace the `upgrade-template-missing` assertion and its comment with:

```tsx
    // The panel draws the template's header, then the screen in its outlet.
    expect(markup).toContain('data-testid="template-panel"');
    expect(markup).toMatch(/data-testid="upgrade-[a-z-]+"/);
```

   In "renders AccessDenied for /stacks/:stackId/templates/:stackTemplateId/upgrade when canOperate is denied but canView is allowed", replace its query client setup with `const queryClient = stackQueryClient(queryKeys, { capabilities: { canView: true, canOperate: false, canApprove: false, canManageAccess: false }, templates: [installedTemplate] });`. Its assertion stays.
4. Add a test for Environment's breadcrumb:

```tsx
  it("renders Environment under a Stacks / stack / Environment breadcrumb", async () => {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { queryKeys } = await import("../api/queryKeys");
    const queryClient = stackQueryClient(queryKeys);
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: ["/stacks/stack_1/environment"] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue()}>
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );

    expect(markup).toContain('data-testid="stack-section-layout"');
    expect(markup).toContain('href="/stacks/stack_1"');
    expect(markup).toMatch(/<h1[^>]*aria-current="page"[^>]*>Environment<\/h1>/);
  });
```

- [ ] **Step 14: Run everything and the build**

Run: `npm test && npm run build`
Expected: PASS. `npm run build` type-checks and builds.

- [ ] **Step 15: Commit**

```bash
git add -A src
git commit -m "feat(web): make the stack's page a split view of its templates

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: The Runs tab

**Files:**
- Create: `web/src/shared/ErrorLine.tsx`
- Create: `web/src/features/runs/WaitingRunActions.tsx` (moved out of `TemplateRunHistory.tsx`)
- Create: `web/src/features/runs/TemplateRunNotices.tsx`
- Modify: `web/src/features/runs/TemplateRunActions.tsx`, `web/src/features/runs/TemplateRunHistory.tsx`
- Modify: `web/src/features/runs/RunDetailScreen.tsx` (import `WaitingRunActions` from its new file)
- Modify: `web/src/features/stacks/TemplateRunsTab.tsx`
- Test: `web/src/features/runs/TemplateRunActions.test.tsx`, `web/src/features/runs/TemplateRunHistory.test.tsx`

**Interfaces:**
- Consumes: `RunStatusLabel` (Task 1), `useStackTemplate` (Task 4), `PlanDiff` (`web/src/shared/PlanDiff.tsx`), `runInFlightReason` (`web/src/features/runs/useRunInFlight.ts`).
- Produces: `ErrorLine({ children, live?, testId? }: { children: ReactNode; live?: boolean; testId?: string })`.
- Produces: `WaitingRunActions` with the same props as today (`run, stackId, approveBusy, discardBusy, onApprove, onDiscard`).
- Produces: `runActionsNote({ canOperate, lifecycle, activeRun }: { canOperate: boolean; lifecycle: string; activeRun: TemplateRun | null }): string` from `TemplateRunActions.tsx`.
- Produces: `TemplateRunNotices({ stackId, stackTemplate })`. The waiting card is `data-testid="template-waiting-plan"` with its link `template-review-plan`. The failed card is `template-failed-destroy` with its link `template-view-failed-run`.

- [ ] **Step 1: Rewrite the run actions tests (failing)**

In `web/src/features/runs/TemplateRunActions.test.tsx`:

1. Import `TemplateRunNotices from "./TemplateRunNotices"` and render it between the two components in `actionsElement`, in the order `TemplateRunsTab` uses:

```tsx
          <TemplateRunActions stackId="stack_1" stackTemplate={stackTemplate(overrides)} />
          <TemplateRunNotices stackId="stack_1" stackTemplate={stackTemplate(overrides)} />
          <TemplateRunHistory stackId="stack_1" stackTemplateId="stpl_1" />
```

2. Replace these tests as follows, and leave the others as they are:

```tsx
  // A run in flight cannot be stopped, so nothing offers to; the toolbar
  // waits for it and says so.
  it("blocks a new plan while an older run is still active, and says why", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    seedRuns(queryClient, [
      run({ id: "newer_completed", operation: "plan", status: "completed", created_at: "2026-07-20T01:00:00Z" }),
      run({ id: "older_active", run_number: 1, operation: "plan", status: "queued", created_at: "2026-07-20T00:00:00Z" })
    ]);

    renderActions(queryClient);

    expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(true);
    expect(screen.getByTestId("template-run-actions-note").textContent).toBe("Wait for run #1 before starting another run.");
    expect(screen.getByTestId("template-run-row-older_active").querySelector("button")).toBeNull();
  });

  it("disables Plan with a reason when canOperate is denied", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, { ...allAllowed, canOperate: false });
    seedRuns(queryClient, []);

    renderActions(queryClient);

    expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(true);
    expect(isDisabled(screen.getByRole("button", { name: /^Apply$/ }))).toBe(true);
    expect(screen.getByTestId("template-run-actions-note").textContent).toBe("Starting a run requires operator access.");
  });

  it("says a template whose destroy failed cannot start runs, and links the failed run", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    seedRuns(queryClient, [run({ id: "run_destroy", run_number: 7, operation: "destroy", status: "failed", plan_summary: { add: 0, change: 0, destroy: 14 } })]);

    renderActions(queryClient, { lifecycle: "failed" });

    expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(true);
    expect(screen.getByTestId("template-run-actions-note").textContent).toBe("A template whose destroy failed cannot start runs.");
    expect(screen.getByTestId("template-failed-destroy").textContent).toContain("The destroy run stopped before it finished. Some resources may still exist.");
    expect(screen.getByTestId("template-view-failed-run").getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs/7");
  });

  // Auto apply is an approval given in advance, so it is offered only to
  // someone who could approve. Starting an Apply, which only saves a plan
  // for someone else to approve, stays.
  it("hides auto apply when canApprove is denied", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, { ...allAllowed, canApprove: false });
    seedRuns(queryClient, []);

    renderActions(queryClient);

    expect(screen.queryByTestId("template-run-auto-approve")).toBeNull();
    expect(button(/^Apply$/)).toBeTruthy();
  });

  // Approving happens on the run, after reading its plan; the Runs tab says a
  // plan waits and links to it.
  it("shows a plan waiting for approval above the runs, linking to it, and offers no approval in the table", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    seedRuns(queryClient, [
      run({
        id: "run_waiting",
        run_number: 2,
        operation: "apply",
        status: "waiting_approval",
        trigger_actor_display_name: "Priya Shah",
        plan_summary: { add: 2, change: 1, destroy: 0 }
      }),
      run({ id: "run_done", operation: "apply", status: "completed" })
    ]);

    renderActions(queryClient, { pending_plan_run_id: "run_waiting", pending_plan_at: "2026-10-03T09:30:00Z" });

    const card = screen.getByTestId("template-waiting-plan");
    expect(card.textContent).toContain("Plan waiting for approval");
    expect(card.textContent).toMatch(/Run #2.*Planned by Priya Shah.*3 Oct, \d\d:\d\d/);
    expect(within(card).getByRole("img", { name: "2 to add, 1 to change, 0 to destroy" })).toBeTruthy();
    expect(screen.getByTestId("template-review-plan").getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs/2");
    expect(button(/^Approve$/)).toBeNull();
    expect(button(/Discard/)).toBeNull();
  });

  it("names a waiting destroy plan as one", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    seedRuns(queryClient, [run({ id: "run_destroy", operation: "destroy", status: "waiting_approval", plan_summary: { add: 0, change: 0, destroy: 3 } })]);

    renderActions(queryClient);

    expect(screen.getByTestId("template-waiting-plan").textContent).toContain("Destroy plan waiting for approval");
  });
```

3. Replace "walks apply → approve from persisted history…" with:

```tsx
  it("walks apply to a plan waiting for approval from persisted history, without a page reload", async () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    let runsState: TemplateRun[] = [];

    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (url.endsWith("/stack-templates/stpl_1/runs") && method === "GET") {
        return jsonResponse(runsState);
      }
      if (url.endsWith("/stack-templates/stpl_1/runs") && method === "POST") {
        const created = run({ id: "run_plan_1", run_number: 1, operation: "apply", status: "waiting_approval", plan_summary: { add: 2, change: 0, destroy: 0 } });
        runsState = [created];
        return jsonResponse(created, 201);
      }
      throw new Error(`unexpected fetch: ${url} ${method}`);
    });

    renderActions(queryClient);
    await waitFor(() => expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(false));

    fireEvent.click(screen.getByRole("button", { name: /^Apply$/ }));

    await waitFor(() => expect(screen.getByTestId("template-review-plan").getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs/1"));
    expect(screen.getByTestId("template-run-status-run_plan_1").textContent).toBe("waiting for approval");
    expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(true);
  });
```

4. In "starts a plan, an apply, and an auto-approved apply", change any `"Auto Apply"` label query to `"Auto apply"`. Add `within` to the `@testing-library/react` import.

In `web/src/features/runs/TemplateRunHistory.test.tsx`, replace the three column tests with:

```tsx
  it("lays each run out in run, status, changes, started by and time columns", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, [
      run({
        id: "run_plan_1",
        run_number: 12,
        operation: "apply",
        status: "completed",
        trigger_actor: "CiQ3YzRiMmYwZS0zZDFhLTRlOGItOWY2Yy0yYTVkOGUxYjBjNDcSBWxvY2Fs",
        trigger_actor_display_name: "Ada Lovelace",
        plan_summary: { add: 3, change: 1, destroy: 0 }
      })
    ]);

    renderHistory(queryClient);

    expect(screen.getAllByRole("columnheader").map((header) => header.textContent)).toEqual(["Run", "Status", "Changes", "Started by", "Time"]);
    const cells = within(screen.getByTestId("template-run-row-run_plan_1")).getAllByRole("cell");
    expect(cells).toHaveLength(5);
    expect(cells[0].textContent).toBe("#12");
    expect(cells[1].textContent).toBe("applied");
    expect(within(cells[2]).getByRole("img", { name: "3 to add, 1 to change, 0 to destroy" })).toBeTruthy();
    expect(cells[3].textContent).toBe("Ada Lovelace");
    expect(cells[4].querySelector("time")?.getAttribute("datetime")).toBe("2026-07-20T00:00:00Z");
  });

  // Approving and discarding happen on the run, after reading its plan.
  it("offers no actions on any row, even a plan waiting for approval", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, [run({ id: "run_apply_1", run_number: 2, operation: "apply", status: "waiting_approval" })]);

    renderHistory(queryClient);

    expect(screen.getAllByRole("columnheader")).toHaveLength(5);
    expect(screen.getByTestId("template-run-row-run_apply_1").querySelector("button")).toBeNull();
  });

  it("shows an empty state that says how to start", () => {
    const queryClient = testQueryClient();
    seedRuns(queryClient, []);

    renderHistory(queryClient);

    expect(screen.getByTestId("template-run-history-empty").textContent).toContain("No runs yet");
    expect(screen.getByTestId("template-run-history-empty").textContent).toContain("Plan to see what this template would create.");
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `npx vitest run src/features/runs/TemplateRunActions.test.tsx src/features/runs/TemplateRunHistory.test.tsx`
Expected: FAIL, `./TemplateRunNotices` cannot be resolved.

- [ ] **Step 3: Create `web/src/shared/ErrorLine.tsx`**

```tsx
import { TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

// A failure said in one line, destructive text after a triangle, in place of
// an alert box. live announces it: true for something that just happened,
// such as a refused request; false for a record, such as a run's error.
export default function ErrorLine({ children, live = true, testId }: { children: ReactNode; live?: boolean; testId?: string }) {
  return (
    <p role={live ? "alert" : undefined} className="flex items-start gap-2 text-meta text-destructive" data-testid={testId}>
      <TriangleAlert aria-hidden="true" strokeWidth={2.25} className="mt-0.5 size-3.5 shrink-0" />
      <span className="min-w-0 wrap-anywhere">{children}</span>
    </p>
  );
}
```

- [ ] **Step 4: Move `WaitingRunActions` into `web/src/features/runs/WaitingRunActions.tsx`**

Cut the `WaitingRunActionsProps` interface, the comment above `WaitingRunActions` and the function from `TemplateRunHistory.tsx` into the new file, with these imports and with Discard and Approve drawn as openplan UI buttons. The destroy confirmation stays exactly as it is.

```tsx
import { Loader2, Trash2 } from "lucide-react";
import type { TemplateRun } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
```

In the function, Discard becomes:

```tsx
        <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} disabled={discardBusy} onClick={onDiscard}>
          {discardBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
          Discard
        </button>
```

and the non-destroy Approve becomes:

```tsx
          <button type="button" className={cn(buttonClass("primary"), "pointer-coarse:h-11")} disabled={approveBusy} onClick={onApprove}>
            {approveBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
            Approve
          </button>
```

In `RunDetailScreen.tsx`, change `import { WaitingRunActions } from "./TemplateRunHistory";` to `import WaitingRunActions from "./WaitingRunActions";`. Export it as default from the new file.

- [ ] **Step 5: Rewrite `web/src/features/runs/TemplateRunHistory.tsx`**

```tsx
import { Link } from "react-router-dom";
import { useTemplateRunsQuery } from "../../api/queries";
import type { TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { formatDateTime } from "../../shared/formatTimestamp";
import PlanDiff from "../../shared/PlanDiff";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import RunStatusLabel from "./RunStatusLabel";

// 8px either side of a column boundary makes the 16px between columns, and
// 16px at the row's ends. Each column's width is its content's (run 64,
// status 190, changes 110, time 110) plus that padding.
const cellClass = "px-2 py-2 first:pl-4 last:pr-4";

interface TemplateRunHistoryProps {
  stackId: string;
  stackTemplateId: string;
}

// Every run recorded for a template, newest first: its number (the link to
// it), where it is, what its plan would change, who started it and when. The
// rows take no actions: a plan is approved or discarded on its run, after
// someone has read it, and a run planning or applying cannot be stopped.
export default function TemplateRunHistory({ stackId, stackTemplateId }: TemplateRunHistoryProps) {
  const runs = useTemplateRunsQuery(tenantID, stackTemplateId).data ?? [];

  return (
    <div className="min-w-0" data-testid="template-run-history">
      {runs.length === 0 ? (
        <Empty className="gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="template-run-history-empty">
          <EmptyHeader className="gap-2">
            <h3 className="text-sm font-medium">No runs yet</h3>
            <EmptyDescription className="text-meta">Plan to see what this template would create.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        // Fixed columns in a frame that scrolls sideways on a phone, so a
        // status that changes length never moves the columns after it.
        <div className="overflow-x-auto rounded-lg border">
          <Table className="min-w-2xl">
            <colgroup>
              <col className="w-22" />
              <col className="w-51.5" />
              <col className="w-31.5" />
              <col />
              <col className="w-33.5" />
            </colgroup>
            <TableHeader>
              <TableRow className="border-divider hover:bg-transparent">
                {["Run", "Status", "Changes", "Started by", "Time"].map((heading) => (
                  <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-xs font-medium text-muted-foreground")}>
                    {heading}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((run) => (
                <RunRow key={run.id} run={run} to={`/stacks/${stackId}/templates/${stackTemplateId}/runs/${run.run_number}`} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function RunRow({ run, to }: { run: TemplateRun; to: string }) {
  return (
    <TableRow className="h-12 border-divider hover:bg-transparent" data-testid={`template-run-row-${run.id}`}>
      <TableCell className={cellClass}>
        <Link
          to={to}
          className="font-mono text-meta text-foreground hover:text-primary hover:underline focus-visible:-outline-offset-2 pointer-coarse:py-3"
          data-testid={`template-run-history-${run.id}`}
        >
          #{run.run_number}
        </Link>
      </TableCell>
      <TableCell className={cellClass}>
        <RunStatusLabel run={run} data-testid={`template-run-status-${run.id}`} />
      </TableCell>
      <TableCell className={cellClass} data-testid={`template-run-summary-${run.id}`}>
        {run.plan_summary && <PlanDiff summary={run.plan_summary} />}
      </TableCell>
      <TableCell className={cn(cellClass, "truncate text-meta text-muted-foreground")} title={run.trigger_actor_display_name}>
        {run.trigger_actor_display_name}
      </TableCell>
      <TableCell className={cn(cellClass, "text-meta text-muted-foreground")}>
        <time dateTime={run.created_at} title={run.created_at}>
          {formatDateTime(run.created_at)}
        </time>
      </TableCell>
    </TableRow>
  );
}
```

- [ ] **Step 6: Create `web/src/features/runs/TemplateRunNotices.tsx`**

```tsx
import { Hourglass, TriangleAlert } from "lucide-react";
import { Link } from "react-router-dom";
import { useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import { formatDateTime } from "../../shared/formatTimestamp";
import PlanDiff from "../../shared/PlanDiff";
import { cn } from "@/lib/utils";

const separator = (
  <span aria-hidden="true" className="text-separator">
    ·
  </span>
);

// What on a template waits on a person, above its runs: a plan waiting for
// approval, which Review plan opens, and a destroy that stopped before it
// finished, which View run opens. Nothing when neither is true.
export default function TemplateRunNotices({ stackId, stackTemplate }: { stackId: string; stackTemplate: StackTemplate }) {
  const runs = useTemplateRunsQuery(tenantID, stackTemplate.id).data ?? [];
  const waiting = runs.find((run) => run.status === "waiting_approval") ?? null;
  const failed = stackTemplate.lifecycle === "failed";
  const failedRun = failed ? runs.find((run) => run.operation === "destroy" && run.status === "failed") ?? null : null;
  const runPath = (run: TemplateRun) => `/stacks/${stackId}/templates/${stackTemplate.id}/runs/${run.run_number}`;

  if (!waiting && !failed) {
    return null;
  }

  return (
    <>
      {waiting && (
        <div className="flex flex-col gap-4 rounded-lg border px-5 py-4 md:flex-row md:items-center md:gap-6" data-testid="template-waiting-plan">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="flex items-center gap-2 text-sm leading-label font-medium">
              <Hourglass aria-hidden="true" className="size-4 shrink-0 text-warning" />
              {waiting.operation === "destroy" ? "Destroy plan waiting for approval" : "Plan waiting for approval"}
            </span>
            <span className="flex flex-wrap items-center gap-x-1.5 text-meta text-muted-foreground">
              <span>Run #{waiting.run_number}</span>
              {separator}
              <span>Planned by {waiting.trigger_actor_display_name}</span>
              {separator}
              <time dateTime={stackTemplate.pending_plan_at || waiting.created_at}>
                {formatDateTime(stackTemplate.pending_plan_at || waiting.created_at)}
              </time>
            </span>
          </div>
          {waiting.plan_summary && <PlanDiff summary={waiting.plan_summary} />}
          <Link to={runPath(waiting)} className={cn(buttonClass("primary"), "shrink-0 self-start pointer-coarse:h-11 md:self-auto")} data-testid="template-review-plan">
            Review plan
          </Link>
        </div>
      )}
      {failed && (
        <div className="flex flex-col gap-4 rounded-lg border px-5 py-4 md:flex-row md:items-center md:gap-6" data-testid="template-failed-destroy">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="flex items-center gap-2 text-sm leading-label font-medium">
              <TriangleAlert aria-hidden="true" className="size-4 shrink-0 text-destructive" />
              Destroy failed
            </span>
            <span className="text-meta text-muted-foreground">The destroy run stopped before it finished. Some resources may still exist.</span>
          </div>
          {failedRun && (
            <Link to={runPath(failedRun)} className={cn(buttonClass("outline"), "shrink-0 self-start pointer-coarse:h-11 md:self-auto")} data-testid="template-view-failed-run">
              View run
            </Link>
          )}
        </div>
      )}
    </>
  );
}
```

- [ ] **Step 7: Rewrite `web/src/features/runs/TemplateRunActions.tsx`**

```tsx
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { FileSearch, Loader2, Play } from "lucide-react";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useStartTemplateRunMutation, useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import { isRunInFlightError } from "./runErrors";
import { runInFlightReason } from "./useRunInFlight";

interface TemplateRunActionsProps {
  stackId: string;
  stackTemplate: StackTemplate;
}

/** The sentence beside Plan and Apply: what they do, or why they cannot be used now. */
export function runActionsNote({
  canOperate,
  lifecycle,
  activeRun
}: {
  canOperate: boolean;
  lifecycle: string;
  activeRun: TemplateRun | null;
}): string {
  if (!canOperate) {
    return "Starting a run requires operator access.";
  }
  if (lifecycle === "failed") {
    return "A template whose destroy failed cannot start runs.";
  }
  if (activeRun) {
    return `${runInFlightReason(activeRun, "starting another run")}.`;
  }
  return "Plan shows what would change. Apply saves a plan that waits for approval.";
}

// The toolbar of a template's Runs tab: a line that explains the buttons,
// then Plan and Apply, which work the way the Terraform CLI's do. Plan only
// shows what would change. Apply saves a plan that waits for approval, on its
// own run, where approving it applies it; Apply with Auto apply checked
// applies straight away. Destroy lives on the Settings tab.
//
// Run state comes from the server's run history (useTemplateRunsQuery), not
// local state, so it is the same for everyone who can view the stack. The
// stack refresh when a run settles is the panel's: useRefreshStackOnRunChange.
export default function TemplateRunActions({ stackId, stackTemplate }: TemplateRunActionsProps) {
  const [errorMessage, setErrorMessage] = useState("");
  const [autoApprove, setAutoApprove] = useState(false);
  const queryClient = useQueryClient();
  const canOperate = useStackCapabilities(stackId)?.canOperate === true;

  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  const runsReady = runsQuery.status === "success";
  const activeRun = runsReady ? runsQuery.data.find((candidate) => !isTerminalRunStatus(candidate.status)) ?? null : null;

  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const startingOperation = startRunMutation.isPending ? startRunMutation.variables?.body.operation : undefined;
  const disabled = !canOperate || !runsReady || activeRun !== null || stackTemplate.lifecycle !== "active" || startingOperation !== undefined;

  async function startRun(operation: "plan" | "apply") {
    setErrorMessage("");
    try {
      const body = operation === "apply" && autoApprove ? { operation, auto_approve: true } : { operation };
      await startRunMutation.mutateAsync({ stackTemplateID: stackTemplate.id, body });
      await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
      if (isRunInFlightError(error)) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
      }
    }
  }

  return (
    <div className="flex flex-col gap-3" data-testid="template-run-actions">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <p className="text-meta text-muted-foreground" data-testid="template-run-actions-note">
          {runActionsNote({ canOperate, lifecycle: stackTemplate.lifecycle, activeRun })}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {/* Auto apply is an approval given in advance, so it is offered
              only to someone who could approve a plan. */}
          <RequireCapability capability="canApprove" stackId={stackId}>
            <label
              className="flex h-8 cursor-pointer items-center gap-2 px-1 text-meta whitespace-nowrap pointer-coarse:h-11"
              data-testid="template-run-auto-approve"
            >
              <input className="size-4 accent-primary" type="checkbox" checked={autoApprove} onChange={(event) => setAutoApprove(event.target.checked)} />
              Auto apply
            </label>
          </RequireCapability>
          <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} disabled={disabled} onClick={() => void startRun("plan")}>
            {startingOperation === "plan" ? (
              <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
            ) : (
              <FileSearch data-icon="inline-start" aria-hidden="true" />
            )}
            Plan
          </button>
          <button type="button" className={cn(buttonClass("primary"), "pointer-coarse:h-11")} disabled={disabled} onClick={() => void startRun("apply")}>
            {startingOperation === "apply" ? (
              <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
            ) : (
              <Play data-icon="inline-start" aria-hidden="true" />
            )}
            Apply
          </button>
        </div>
      </div>
      {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
    </div>
  );
}
```

- [ ] **Step 8: Compose the tab in `web/src/features/stacks/TemplateRunsTab.tsx`**

```tsx
import TemplateRunActions from "../runs/TemplateRunActions";
import TemplateRunHistory from "../runs/TemplateRunHistory";
import TemplateRunNotices from "../runs/TemplateRunNotices";
import { useStackTemplate } from "./stackTemplateContext";

// A template's Runs tab, and the stack's index when no template is in the
// URL: the toolbar that starts a run, what waits on a person, then every run.
export default function TemplateRunsTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  return (
    <section className="flex min-w-0 flex-col gap-5" data-testid="template-runs-tab">
      <TemplateRunActions stackId={stackId} stackTemplate={stackTemplate} />
      <TemplateRunNotices stackId={stackId} stackTemplate={stackTemplate} />
      <TemplateRunHistory stackId={stackId} stackTemplateId={stackTemplate.id} />
    </section>
  );
}
```

- [ ] **Step 9: Run the tests and see them pass**

Run: `npx vitest run src/features/runs src/features/stacks && npx tsc -b`
Expected: PASS. If `StackTemplatePages.test.tsx` "renders the run actions above the history…" fails on `template-run-history`, check that it still exists. The wrapper keeps that test ID in both states.

- [ ] **Step 10: Commit**

```bash
git add -A src
git commit -m "feat(web): rebuild the Runs tab on openplan UI

Approve and Discard leave the runs table and stay on the run, behind
Review plan, so a plan is read before it is approved.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: A run in the panel

**Files:**
- Modify: `web/src/features/runs/RunDetailScreen.tsx`
- Modify: `web/src/features/runs/RunLogsPanel.tsx`
- Modify: `web/src/shared/LogSteps.tsx`
- Test: `web/src/features/runs/RunDetailScreen.test.tsx`, `web/src/shared/LogSteps.test.tsx` (run as is)

**Interfaces:**
- Consumes: `RunStatusLabel` (Task 1), `WaitingRunActions`, `ErrorLine` (Task 6), `PlanDiff`.
- Produces: test IDs unchanged: `run-detail-screen`, `run-detail-status`, `run-detail-missing`, `run-detail-loading`, `run-detail-error`, `run-detail-retry`, `run-logs-panel`, `log-scroll-area-<phase>`. New: the crumb `nav` named "Run".

- [ ] **Step 1: Update the run tests (failing)**

In `web/src/features/runs/RunDetailScreen.test.tsx`:

1. In "renders the run summary and stacks its logs…": replace `expect(screen.getByTestId("run-logs-panel").getAttribute("data-slot")).toBe("card");` with `expect(within(screen.getByTestId("run-logs-panel")).getByRole("heading", { name: "Logs" })).toBeTruthy();`.
2. In "offers Approve and Discard for a plan waiting for approval…": replace `expect(screen.getByText("+2 ~0 -1")).toBeTruthy();` with `expect(screen.getByRole("img", { name: "2 to add, 0 to change, 1 to destroy" })).toBeTruthy();`.
3. Add:

```tsx
  it("leads with a trail back to the template's runs and titles the run", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(queryKeys.templateRun("tenant_123", "run_1"), run({ run_number: 1 }));
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    const trail = screen.getByRole("navigation", { name: "Run" });
    expect(within(trail).getByRole("link", { name: "Runs" }).getAttribute("href")).toBe("/stacks/stack_1/templates/stpl_1/runs");
    expect(within(trail).getByText("Run #1").getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("heading", { level: 3, name: "Run #1" })).toBeTruthy();
  });

  it("says an unfinished run has not finished", () => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    queryClient.setQueryData(
      queryKeys.templateRun("tenant_123", "run_1"),
      run({ status: "waiting_approval", completed_at: "0001-01-01T00:00:00Z", plan_summary: { add: 1, change: 0, destroy: 0 } })
    );
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([]));

    renderScreen(queryClient);

    expect(screen.getByText("Finished").nextElementSibling?.textContent).toBe("Not finished");
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run: `npx vitest run src/features/runs/RunDetailScreen.test.tsx`
Expected: FAIL: there's no "Run" navigation, "Finished" is "Completed", and the log panel has no Logs heading role inside a section.

- [ ] **Step 3: Rewrite the render of `web/src/features/runs/RunDetailScreen.tsx`**

Keep the hooks, `runAction`, `handleApprove` and `handleDiscard` as they are. Add the imports `Link` (react-router-dom), `PlanDiff`, `ErrorLine` and `RunStatusLabel`. Remove `Alert`, `AlertDescription`, `Card` and `planSummaryLabel`. Add these two helpers below the component:

```tsx
// The trail above a run: back to its template's runs, then the run itself.
function RunTrail({ stackId, stackTemplateId, runNumber }: { stackId: string; stackTemplateId: string; runNumber: string }) {
  return (
    <nav aria-label="Run" className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <Link to={`/stacks/${stackId}/templates/${stackTemplateId}/runs`} className="hover:text-foreground hover:underline">
        Runs
      </Link>
      <span aria-hidden="true" className="text-separator">
        /
      </span>
      <span aria-current="page" className="text-foreground">
        Run #{runNumber}
      </span>
    </nav>
  );
}

function Fact({ term, mono = false, children }: { term: string; mono?: boolean; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className={cn("text-meta wrap-anywhere", mono && "font-mono")}>{children}</dd>
    </div>
  );
}
```

Each early return (`run-detail-missing`, `run-detail-error`, `run-detail-loading`) becomes a `<section className="flex min-w-0 flex-col gap-5" data-testid=…>` that starts with `<RunTrail stackId={stackId} stackTemplateId={stackTemplateId} runNumber={runNumber} />`. The error state's `Alert` becomes `<ErrorLine live={false}>Something went wrong while loading the run.</ErrorLine>`, and its retry `Button` becomes `<button type="button" className={cn(buttonClass("outline"), "self-start pointer-coarse:h-11")} …>` with the same `onClick` and test ID. The loading line keeps its spinner with `text-meta`.

The main return becomes:

```tsx
  return (
    <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-screen">
      <RunTrail stackId={stackId} stackTemplateId={stackTemplateId} runNumber={runNumber} />
      {run && (
        <>
          <header className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex min-w-0 flex-wrap items-center gap-3">
              <h3 className="font-heading text-lg leading-title font-semibold tracking-title">Run #{run.run_number}</h3>
              <RunStatusLabel run={run} data-testid="run-detail-status" />
            </div>
            {canApprove && (
              <div className="flex flex-wrap gap-2">
                <WaitingRunActions
                  run={run}
                  stackId={stackId}
                  approveBusy={approveRunMutation.isPending}
                  discardBusy={discardRunMutation.isPending}
                  onApprove={handleApprove}
                  onDiscard={handleDiscard}
                />
              </div>
            )}
          </header>
          {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
          <dl className="grid min-w-0 gap-x-6 gap-y-4 rounded-lg border px-5 py-4 sm:grid-cols-3">
            <Fact term="Started">
              <time dateTime={run.created_at} title={run.created_at}>
                {formatDateTime(run.created_at)}
              </time>
            </Fact>
            <Fact term="Finished">
              {hasCompleted(run.completed_at) ? (
                <time dateTime={run.completed_at} title={run.completed_at}>
                  {formatDateTime(run.completed_at ?? "")}
                </time>
              ) : (
                "Not finished"
              )}
            </Fact>
            {run.plan_summary && (
              <Fact term="Changes">
                <PlanDiff summary={run.plan_summary} />
              </Fact>
            )}
            <Fact term="Started by">{run.trigger_actor_display_name}</Fact>
            <Fact term="Source" mono>
              {run.selected_ref} @ {run.resolved_commit_sha.slice(0, 7)}
            </Fact>
          </dl>
          {run.error_summary && <ErrorLine live={false}>{run.error_summary}</ErrorLine>}
        </>
      )}
      <RunLogsPanel
        key={runId}
        runId={runId}
        logs={logsQuery.data}
        failed={logsQuery.isError}
        finished={Boolean(run && isTerminalRunStatus(run.status))}
      />
    </section>
  );
```

Add `import type { ReactNode } from "react";` and the `buttonClass` and `cn` imports. Update the file's leading comment: the run opens inside the template's panel, under a trail back to Runs.

- [ ] **Step 4: Rewrite the frame of `web/src/features/runs/RunLogsPanel.tsx`**

Keep the open-state logic and `RunLogBody`. Replace the `Card` frame:

```tsx
  const headingId = useId();
  …
  return (
    <section aria-labelledby={headingId} className="overflow-hidden rounded-lg border" data-testid="run-logs-panel">
      <h4 id={headingId} className="flex items-center gap-2 border-b border-divider px-4 py-3 text-sm font-semibold">
        <SquareTerminal className="size-4 text-subtle-foreground" aria-hidden="true" />
        Logs
      </h4>
      {content}
    </section>
  );
```

The two message paragraphs become `<p className="px-4 py-3 text-meta text-muted-foreground">`. Import `useId` from React and remove the `Card` imports.

- [ ] **Step 5: Restyle `web/src/shared/LogSteps.tsx`**

The list drops its own frame, because its panel draws one. The rows take the board's look:

```tsx
export function LogSteps({ children }: { children: ReactNode }) {
  return <ol className="divide-y divide-divider">{children}</ol>;
}
```

In `LogStep`, the trigger's class becomes `"group flex min-h-10 w-full items-center gap-2 px-4 text-left font-mono text-meta outline-none transition-colors hover:bg-canvas focus-visible:inset-ring-2 focus-visible:inset-ring-ring pointer-coarse:min-h-11"` and the chevron's `"size-3.5 shrink-0 text-subtle-foreground transition-transform group-aria-expanded:rotate-90"`. The content changes from `className="bg-foreground"` to `className="border-t border-divider bg-canvas"`, and the `pre` to `className="m-0 py-3 pr-4 pl-9.5 font-mono text-xs leading-relaxed whitespace-pre text-code-foreground"`. Update the comment: logs read on the canvas, not inverted.

- [ ] **Step 6: Run the tests and see them pass**

Run: `npx vitest run src/features/runs src/shared/LogSteps.test.tsx src/dev && npx tsc -b`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add -A src
git commit -m "feat(web): open a run inside the template's panel

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Verify, look at it, open PR 1

**Files:** none new.

- [ ] **Step 1: Full checks**

Run from `web/`: `npm test && npm run build`
Expected: every test passes; the build succeeds. Then `rg -n "StatusBadge|statusGlyph" src/features/stacks src/features/runs`. Expected: no hits outside `AddStackTemplateScreen.tsx` and `UpgradeStackTemplateScreen.tsx`, which are PR 2.

- [ ] **Step 2: Bring up the stack and the UI**

Follow `docs/development.md` to start the local stack, then `cd web && npm run dev` (it must be `localhost:5173`). Start headless Chrome as the header of `scripts/drive-web.mjs` shows, and export `OPENPLAN_USER` and `OPENPLAN_PASS` from `docs/authentication.md`. Make sure one stack has at least two templates, one of them with a plan waiting for approval; use the UI's Apply to make one.

- [ ] **Step 3: Desktop screenshots**

From the repo root, with `<id>` a stack id and `<tid>` a template id:

```bash
node scripts/drive-web.mjs --goto /stacks/<id> --shot /tmp/stack-page.png
node scripts/drive-web.mjs --goto /stacks/<id>/templates/<tid>/runs/1 --shot /tmp/stack-run.png
node scripts/drive-web.mjs --goto /stacks/<id>/templates/<tid>/variables --shot /tmp/stack-variables.png
```

Compare with the canvas boards "J · prod (clickable)" and "J · prod, run inside the panel". Check: the header, the list with chips, the selected row, the tabs with the current one underlined, the waiting card, the runs table, and the run's trail, facts and logs.

- [ ] **Step 4: Phone screenshots**

Restart Chrome with `--window-size=375,812`, then:

```bash
node scripts/drive-web.mjs --goto /stacks/<id> --shot /tmp/stack-phone-list.png --probe 'document.documentElement.scrollWidth <= innerWidth'
node scripts/drive-web.mjs --goto /stacks/<id>/templates/<tid>/runs --shot /tmp/stack-phone-panel.png --probe 'document.documentElement.scrollWidth <= innerWidth'
```

Expected: the list alone, then the panel alone with "Templates" at its top, and both probes print `true` (no sideways scroll). Stop Chrome afterwards.

- [ ] **Step 5: Push and open PR 1**

```bash
gh auth status
git push -u origin feat/stack-page
gh pr create --base main --title "feat(web): make the stack's page a split view of its templates" --body-file - <<'EOF'
## What changes

`/stacks/:id` is now the stack's one page, built to openplan UI. At the top: a breadcrumb, the name, the slug and tags, and Environment and Access. Below that, the stack's templates sit on the left, with a filter, chips for a plan to approve or a failed destroy, and Add template. The selected template sits on the right, with its tabs.

- The page opens on the first template with a waiting plan, then a failed destroy, otherwise the first. Picking another template keeps the current tab.
- The Runs tab and the run are rebuilt: icon-and-word run states, a waiting-plan card with **Review plan**, a failed-destroy card, and the run inside the panel under "Runs / Run #N".
- **Behaviour change:** Approve and Discard leave the runs table and stay on the run, so a plan is read before it is approved.
- On a phone the page shows the list or the panel, never both.
- Every existing URL still resolves. `/stacks/:id/templates` redirects to the stack.
- Removed: the stack's tab row, the Overview placeholder, the template list page, and the template's own page shell.

Variables, Credentials, Settings, Change revision and Add template keep their current content inside the new panel; PR 2 rebuilds them.

Spec: `docs/superpowers/specs/2026-10-03-stack-page-design.md`
Plan: `docs/superpowers/plans/2026-10-03-stack-page-frame.md`

No configuration or migration impact.

## Validation

- `npm test`
- `npm run build`
- Browser check at desktop width and at 375px (screenshots below)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
```

Attach the four screenshots to the PR as a comment, or by editing the body in the browser.

- [ ] **Step 6: After it merges: record the run states in openplan UI**

Ask the user before publishing. Then update the "openplan UI" artifact (https://claude.ai/artifact/4hCc6LddTF31cNeaqHwZn8):
- add the run rows from the spec's "Run status" table to the status tables in `project/README.md` and `project/components/StatusLabel/README.md`;
- in `project/code.md`, list `/stacks/:id` under Pages, and map TemplatePanel (`web/src/features/stacks/TemplatePanel.tsx`), the run StatusLabel (`web/src/features/runs/RunStatusLabel.tsx`) and the underline tabs (`web/src/features/stacks/TemplateTabs.tsx`).

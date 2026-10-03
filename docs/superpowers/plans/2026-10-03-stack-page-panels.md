# Stack page panel views (PR 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the five views that still draw their pre-redesign markup inside the stack page's panel (Variables, Credentials, Settings, Change revision, Add template) on openplan UI, and move every lock reason into one module.

**Architecture:** One module, `features/runs/lockReasons.ts`, decides why a template's controls are locked: Plan and Apply, Save variables, Change revision and Destroy. Every reason is a sentence ending with a full stop. Shared form pieces (`shared/fieldClass.ts`, `VariableFields`, an `icon` button variant) give Variables, Credentials, Change revision and Add template the same 36px mono fields. Settings becomes two bordered sections (`SettingsSection`), Change revision reads its template from the panel's context and gains a crumb row (`PanelTrail`, extracted from the run view), and Add template draws its own header in place of the template header and absorbs PR 1's `AddTemplatePanel` stopgap.

**Tech Stack:** React 19, react-router-dom 6 (data routers), TanStack Query 5, Tailwind v4 with the tokens in `web/src/styles/theme.css`, shadcn/ui (base-nova, on Base UI), lucide-react, Vitest and Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-03-stack-page-design.md`. PR 2 covers "Variables", "Credentials", "Settings", "Change revision (upgrade)", "Add template (templates/new)" and "Empty stack". The design boards are the claude.ai canvas https://claude.ai/artifact/74qBEVqi1VAWDrEtFcnbhh, file `project/StackPage.dc.html`: its markup holds the spacing, copy and colours, and its `renderVals()` holds the states. Read it with the Artifact tool (`action: "read"`), never by web fetch.

## Global Constraints

- **Branch** `feat/stack-page-panels`, from `feat/stack-page` (PR #293, open). Open PR 2 against `feat/stack-page` while #293 is open, and retarget it to `main` once #293 merges. If #293 changes under review, rebase onto it.
- **Design system: openplan UI.** Only theme tokens and Tailwind scale steps. `src/styles/tailwind.guard.test.ts` bans arbitrary values (`w-[37px]`, `grid-cols-[…]`) and palette colours; arbitrary variants (`data-[size=default]:h-9`) are allowed. A width the scale lacks is a scale step (`w-45` = 180px, `basis-50` = 200px). Spacing: N × 4px.
- **No status dots.** Every state is a lucide icon and a word, through `StatusLabel` (`web/src/shared/StatusLabel.tsx`) or a Chip. `StatusBadge`, `statusGlyph` and `toneTextClass` leave every screen this plan touches.
- **Buttons:** `buttonClass(variant, size)` from `web/src/shared/buttonClass.ts`. A `<Link>` when it navigates, a `<button type="button">` when it acts. `pointer-coarse:h-11` on every button and link styled as one (`pointer-coarse:size-11` on an icon button).
- **Fields:** inputs and select triggers take `inputClass` / `selectTriggerClass` from `web/src/shared/fieldClass.ts` (Task 2): 36px, 13px mono. A sans field label takes `fieldLabelClass` (13px, 500).
- **Type steps:** `text-panel-title`, `text-meta` (13px), `text-sm` (14), `text-xs` (12). Titles add `tracking-title`. Section headings (`h3`) are `text-sm font-semibold`. Names in mono: `font-mono`.
- **Copy:** sentence case; status words lowercase; " · " joins metadata; dates are absolute (`formatTimestamp` → "12 Aug 2026").
- **Notes and reasons end with a full stop.** PR 1's Runs toolbar notes already do. This plan makes `runInFlightReason` return the full stop itself, so every caller gets it, and every other note ("Destroy in progress.", "Editing requires operator access.") gets one too. Loading lines end with "…", not a full stop.
- **Keep every existing `data-testid` you can, and every `data-unsaved` marker.** `SessionProvider` reads `[data-unsaved='true']` to avoid navigating away from unsaved edits.
- **Tests:** Vitest. jsdom files start with `// @vitest-environment jsdom`. Seed the query cache with `staleTime: Infinity, retry: false`. Find elements by role, label, text or test ID, never by class; the existing class assertions in `EnvironmentScreen.test.tsx` (44px targets) and `buttonClass.test.ts` stay. Run single files with `npx vitest run <path>` from `web/`; for full failure output, `rtk proxy npx vitest run <path>`.
- **React Query notifies on a later tick.** After `queryClient.setQueryData(...)` inside `act()`, `await waitFor(...)` on something visible before asserting.
- **`TemplatePanel` refetches the stack** when its template's latest run settles or starts waiting (`useRefreshStackOnRunChange`). A test that seeds such a run under the panel stubs `fetch`: answer the stack URL with the seeded view and leave every other request pending.
- **Commits:** `feat(web): …`, `refactor(web): …`, `test(web): …`, `docs: …`, each ending with the line `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Decisions this plan makes

The spec and the boards leave these open; each is settled here so the tasks agree.

1. **Full stops** on every note and reason (see Global Constraints).
2. **One precedence for every lock:** operator access, then a destroy in progress, then a failed or other non-active lifecycle, then runs still loading or failed to load, then a run in flight. Variables and Change revision are locked only by operator access, a destroy in progress, and a run in flight, as today.
3. **Destroy's reasons for a lifecycle that isn't active:** "A template whose destroy failed cannot start runs." for `failed` (the Runs toolbar's sentence), "Only an active template can be destroyed." for any other.
4. **Plan and Apply say why while runs load** ("Loading runs…") and for `destroying` / `orphaned` templates, instead of the default note. Folded in from PR 1's final review at the user's request.
5. **Add template opens the new panel without a flash.** The add mutation appends the new template to the cached stack before the screen navigates, so `TemplatePanel` finds it before the refetch lands. Folded in from PR 1's final review at the user's request.
6. **Add template always shows the Revision select**, as the board does, even with one active revision, so the commit that will be installed is always on screen. Its options keep today's label: `abcdef1 · 19 Jul 2026 · latest`.
7. **Add template rows keep the ref.** The second line is the root path and the ref (`aws/eks · v1.4.0`): two source templates can share a path on different refs, and without the ref their rows would be identical. The commit moves from the row to the Revision select.
8. **Revision states read** "waiting for validation", "validating" (`loader-circle`, settled) and "failed validation" (`triangle-alert`, failed, 500). A row whose newest revision is not active shows that state, as the old pill did; a row with no active revision is disabled and the state is why.
9. **The Settings sentence's source** is the desired revision's `root_path`, or `owner/repo` when the template sits at the repository root (`.` or empty). Until the revisions query has loaded, the sentence ends after the ref.
10. **Environment keeps its write-only note** ("Values are write-only and injected only when Terraform runs. …") as its `note` prop, and its "Environment credentials" h2 goes: the breadcrumb's h1 names the page.
11. **After Change revision and Add template, the panel opens on Runs** (`stackTemplatePath(…, "runs")`), skipping the index redirect.
12. **Change revision keeps its option labels** (`templateRevisionLabel`), and its guards: no new run-in-flight guard on the direct URL.

## Review Focus

1. **A run starts while someone has unsaved variable edits:** the reason appears above the fields, the inputs lock with the typed values still in them, Save variables is disabled, and the tab stays `data-unsaved`. Test in Task 2.
2. **Add template, then the new panel opens while the stack refetch is still out:** the panel shows the template at once, never "That template is not installed on this stack." Test in Task 6.
3. **A viewer without `canOperate` opens Settings:** Change revision and Destroy both show, both disabled, each section saying why. Test in Task 4.
4. **A template at its repository's root** (`root_path` "."): Settings names `owner/repo`, never "of .". Test in Task 4.
5. **A credential name too long for the row, on a 375px phone:** the name truncates with the full name in its `title`, the delete button stays in reach, and the page never scrolls sideways. Test (the `title`) in Task 3; checked in the browser in Task 7.

---

## File map

| File | Responsibility |
| --- | --- |
| `web/src/features/runs/lockReasons.ts` (new, + test) | why each control is locked; `useLockState`; `runInFlightReason` moves here |
| `web/src/features/runs/useRunInFlight.ts` (deleted) | replaced by `lockReasons.ts` |
| `web/src/features/runs/TemplateRunActions.tsx` (modify, + test) | the toolbar note and lock from `lockReasons` |
| `web/src/shared/fieldClass.ts` (new) | 36px mono inputs and select triggers, 13px labels |
| `web/src/features/stacks/VariableFields.tsx` (modify) | one column of mono-labelled fields with descriptions |
| `web/src/features/stacks/StackTemplateConfigPanel.tsx` (modify, + test) | reason, fields, Save variables |
| `web/src/features/stacks/TemplateVariablesTab.tsx` (modify) | the Variables tab's states |
| `web/src/shared/buttonClass.ts` (modify, + test) | adds the `icon` variant |
| `web/src/features/stacks/CredentialsPanel.tsx` (modify) | note, bordered list or empty state, Name/Value/Add credential |
| `web/src/features/stacks/TemplateCredentialsTab.tsx`, `EnvironmentScreen.tsx` (modify, + test) | the panel's copy |
| `web/src/features/templates/templateWorkflow.ts` (modify, + test) | `revisionSourceLabel` |
| `web/src/features/stacks/SettingsSection.tsx` (new) | one bordered setting: text and reason at the left, button at the right |
| `web/src/features/stacks/TemplateSettingsTab.tsx` (modify) | the Revision section |
| `web/src/features/runs/TemplateDestroyPanel.tsx` (modify, + test) | the Destroy section |
| `web/src/features/stacks/PanelTrail.tsx` (new) | the crumb row of a view nested in a tab |
| `web/src/features/runs/RunDetailScreen.tsx` (modify) | uses `PanelTrail` |
| `web/src/features/stacks/UpgradeStackTemplateScreen.tsx` (modify, + test) | Change revision |
| `web/src/features/templates/revisionIndicator.ts` (new, + test) | a revision's state for StatusLabel |
| `web/src/api/queries.ts` (modify) | the add mutation puts the new template in the cached stack |
| `web/src/features/stacks/AddStackTemplateScreen.tsx` (modify, + test) | Add template, with its own header |
| `web/src/features/stacks/AddTemplatePanel.tsx` (deleted) | folded into `AddStackTemplateScreen` |
| `web/src/app/router.tsx` (modify) | `templates/new` renders `AddStackTemplateScreen` |
| `web/src/features/stacks/StackTemplatePages.test.tsx` (modify) | the tabs' tests follow the new markup |

"Empty stack" from the spec shipped in PR 1 (`NoTemplatesState`, `StackIndexPanel`); Task 7 checks it in the browser.

---

### Task 0: Commit this plan

- [ ] **Step 1: Commit**

```bash
git add docs/superpowers/plans/2026-10-03-stack-page-panels.md
git commit -m "docs: add the plan for the stack page's panel views

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 1: Why a control is locked, in one place

**Files:**
- Create: `web/src/features/runs/lockReasons.ts`
- Test: `web/src/features/runs/lockReasons.test.ts`
- Delete: `web/src/features/runs/useRunInFlight.ts`
- Modify: `web/src/features/runs/TemplateRunActions.tsx`
- Modify: `web/src/features/runs/TemplateRunActions.test.tsx`
- Modify: `web/src/features/stacks/TemplateVariablesTab.tsx` (switch to the new module; Task 2 rebuilds it)
- Modify: `web/src/features/stacks/TemplateSettingsTab.tsx` (switch to the new module; Task 4 rebuilds it)
- Modify: `web/src/features/stacks/StackTemplatePages.test.tsx` (full stops)

**Interfaces:**
- Produces, in `lockReasons.ts`:
  - `export interface LockState { canOperate: boolean; lifecycle: string; runs: "pending" | "error" | "success"; activeRun: TemplateRun | null }`
  - `export function useLockState(stackId: string, stackTemplate: Pick<StackTemplate, "id" | "lifecycle">): LockState`
  - `export function runInFlightReason(run: TemplateRun, change: string): string` (now ends with ".")
  - `export function startRunLockReason(state: LockState): string`, `variablesLockReason`, `revisionLockReason`, `destroyLockReason` (same signature; `""` when the control is free)
  - `export function runActionsNote(state: LockState): string`

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/runs/lockReasons.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { TemplateRun } from "../../api/types";
import {
  destroyLockReason,
  revisionLockReason,
  runActionsNote,
  runInFlightReason,
  startRunLockReason,
  variablesLockReason
} from "./lockReasons";
import type { LockState } from "./lockReasons";

function run(status: TemplateRun["status"]): TemplateRun {
  return {
    id: "run_7",
    tenant_id: "tenant_123",
    stack_template_id: "st_1",
    template_revision_id: "rev_1",
    source_template_id: "src_1",
    operation: "apply",
    selected_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    workspace_name: "ws",
    config_json: {},
    backend_type: "s3",
    backend_config_hash: "hash",
    status,
    step: "",
    trigger_actor: "user_123",
    trigger_actor_display_name: "user_123",
    created_at: "2026-10-03T00:00:00Z",
    error_summary: "",
    run_number: 7,
    auto_approve: false,
    plan_summary: null
  };
}

const free: LockState = { canOperate: true, lifecycle: "active", runs: "success", activeRun: null };

describe("runInFlightReason", () => {
  it("asks for a decision on a waiting plan and patience otherwise, as a sentence", () => {
    expect(runInFlightReason(run("waiting_approval"), "changing the config")).toBe("Apply or discard run #7 before changing the config.");
    expect(runInFlightReason(run("running"), "destroying")).toBe("Wait for run #7 before destroying.");
  });
});

describe("lock reasons", () => {
  it("leave every control free when nothing stands in the way", () => {
    for (const reason of [startRunLockReason, variablesLockReason, revisionLockReason, destroyLockReason]) {
      expect(reason(free)).toBe("");
    }
    expect(runActionsNote(free)).toBe("Plan shows what would change. Apply saves a plan that waits for approval.");
  });

  // Columns: what locks, then the reason for Plan and Apply, Save variables,
  // Change revision and Destroy. "" means that control stays free.
  it.each([
    [
      "missing operator access",
      { canOperate: false, lifecycle: "destroying", activeRun: run("running") },
      "Starting a run requires operator access.",
      "Editing requires operator access.",
      "Changing the revision requires operator access.",
      "Destroying requires operator access."
    ],
    [
      "a destroy in progress",
      { lifecycle: "destroying", activeRun: run("running") },
      "Destroy in progress.",
      "Destroy in progress.",
      "Destroy in progress.",
      "Destroy in progress."
    ],
    [
      "a failed destroy",
      { lifecycle: "failed" },
      "A template whose destroy failed cannot start runs.",
      "",
      "",
      "A template whose destroy failed cannot start runs."
    ],
    ["an orphaned template", { lifecycle: "orphaned" }, "Only an active template can start runs.", "", "", "Only an active template can be destroyed."],
    ["runs still loading", { runs: "pending" }, "Loading runs…", "", "", "Loading runs…"],
    [
      "runs that failed to load",
      { runs: "error" },
      "This template's runs could not be loaded.",
      "",
      "",
      "This template's runs could not be loaded."
    ],
    [
      "a plan waiting for approval",
      { activeRun: run("waiting_approval") },
      "Apply or discard run #7 before starting another run.",
      "Apply or discard run #7 before changing the config.",
      "Apply or discard run #7 before changing the revision.",
      "Apply or discard run #7 before destroying."
    ],
    [
      "a run still working",
      { activeRun: run("running") },
      "Wait for run #7 before starting another run.",
      "Wait for run #7 before changing the config.",
      "Wait for run #7 before changing the revision.",
      "Wait for run #7 before destroying."
    ]
  ] as const)("say why for %s", (_, overrides, startRun, variables, revision, destroy) => {
    const state: LockState = { ...free, ...overrides };
    expect([startRunLockReason(state), variablesLockReason(state), revisionLockReason(state), destroyLockReason(state)]).toEqual([
      startRun,
      variables,
      revision,
      destroy
    ]);
  });

  it("puts the lock in the note beside Plan and Apply, in place of what they do", () => {
    expect(runActionsNote({ ...free, runs: "pending" })).toBe("Loading runs…");
  });
});
```

In `web/src/features/runs/TemplateRunActions.test.tsx`, add the note to the existing loading test, so it reads:

```tsx
  it("keeps Plan disabled until run history has loaded, and says so", () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);

    renderActions(queryClient);

    expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(true);
    expect(screen.getByTestId("template-run-actions-note").textContent).toBe("Loading runs…");
  });
```

and add, after the failed-destroy test:

```tsx
  it.each([
    ["destroying", "Destroy in progress."],
    ["orphaned", "Only an active template can start runs."]
  ])("says why Plan and Apply are locked while the template is %s", (lifecycle, note) => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    seedRuns(queryClient, []);

    renderActions(queryClient, { lifecycle });

    expect(isDisabled(screen.getByRole("button", { name: /Plan/ }))).toBe(true);
    expect(isDisabled(screen.getByRole("button", { name: /^Apply$/ }))).toBe(true);
    expect(screen.getByTestId("template-run-actions-note").textContent).toBe(note);
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run from `web/`: `npx vitest run src/features/runs/lockReasons.test.ts src/features/runs/TemplateRunActions.test.tsx`
Expected: FAIL. `lockReasons.test.ts` cannot resolve `./lockReasons`; the new TemplateRunActions cases fail on the note text.

- [ ] **Step 3: Create `web/src/features/runs/lockReasons.ts`**

```ts
import { isTerminalRunStatus } from "../../api/polling";
import { useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import { tenantID } from "../../config";

/** What decides whether a template's controls can be used now. */
export interface LockState {
  canOperate: boolean;
  lifecycle: string;
  /** Where the template's runs query is. */
  runs: "pending" | "error" | "success";
  /** The template's unfinished run, once its runs have loaded. */
  activeRun: TemplateRun | null;
}

// A run snapshots desired state when it starts, so until it finishes or is
// discarded, nothing may change the config or the revision, and no other run
// may start. The server refuses; the controls say why before anyone tries.
export function useLockState(stackId: string, stackTemplate: Pick<StackTemplate, "id" | "lifecycle">): LockState {
  const canOperate = useStackCapabilities(stackId)?.canOperate === true;
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  const activeRun = runsQuery.status === "success" ? runsQuery.data.find((run) => !isTerminalRunStatus(run.status)) ?? null : null;
  return { canOperate, lifecycle: stackTemplate.lifecycle, runs: runsQuery.status, activeRun };
}

const DESTROYING = "Destroy in progress.";
const DESTROY_FAILED = "A template whose destroy failed cannot start runs.";
const RUNS_LOADING = "Loading runs…";
const RUNS_FAILED = "This template's runs could not be loaded.";

/** The sentence a control locked by a run shows. */
export function runInFlightReason(run: TemplateRun, change: string): string {
  const action = run.status === "waiting_approval" ? "Apply or discard" : "Wait for";
  return `${action} run #${run.run_number} before ${change}.`;
}

// Each function below returns why its control cannot be used now, or "" when
// it can. The order is the same everywhere: who you are, then what the
// template is, then whether its runs are known, then the run in flight.

/** Plan and Apply. */
export function startRunLockReason(state: LockState): string {
  if (!state.canOperate) return "Starting a run requires operator access.";
  if (state.lifecycle === "destroying") return DESTROYING;
  if (state.lifecycle === "failed") return DESTROY_FAILED;
  if (state.lifecycle !== "active") return "Only an active template can start runs.";
  if (state.runs === "pending") return RUNS_LOADING;
  if (state.runs === "error") return RUNS_FAILED;
  if (state.activeRun) return runInFlightReason(state.activeRun, "starting another run");
  return "";
}

/** The sentence beside Plan and Apply: why they are locked, else what they do. */
export function runActionsNote(state: LockState): string {
  return startRunLockReason(state) || "Plan shows what would change. Apply saves a plan that waits for approval.";
}

/** Save variables, and the fields above it. */
export function variablesLockReason(state: LockState): string {
  if (!state.canOperate) return "Editing requires operator access.";
  if (state.lifecycle === "destroying") return DESTROYING;
  if (state.activeRun) return runInFlightReason(state.activeRun, "changing the config");
  return "";
}

/** Change revision. */
export function revisionLockReason(state: LockState): string {
  if (!state.canOperate) return "Changing the revision requires operator access.";
  if (state.lifecycle === "destroying") return DESTROYING;
  if (state.activeRun) return runInFlightReason(state.activeRun, "changing the revision");
  return "";
}

/** Destroy, which starts a run, so it waits for the runs like Plan does. */
export function destroyLockReason(state: LockState): string {
  if (!state.canOperate) return "Destroying requires operator access.";
  if (state.lifecycle === "destroying") return DESTROYING;
  if (state.lifecycle === "failed") return DESTROY_FAILED;
  if (state.lifecycle !== "active") return "Only an active template can be destroyed.";
  if (state.runs === "pending") return RUNS_LOADING;
  if (state.runs === "error") return RUNS_FAILED;
  if (state.activeRun) return runInFlightReason(state.activeRun, "destroying");
  return "";
}
```

- [ ] **Step 4: Point `TemplateRunActions.tsx` at it**

In `web/src/features/runs/TemplateRunActions.tsx`:

1. Replace the imports block (lines 1–15) with:

```tsx
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { FileSearch, Loader2, Play } from "lucide-react";
import { queryKeys } from "../../api/queryKeys";
import { useStartTemplateRunMutation } from "../../api/queries";
import type { StackTemplate } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import { runActionsNote, startRunLockReason, useLockState } from "./lockReasons";
import { isRunInFlightError } from "./runErrors";
```

2. Delete the local `runActionsNote` function and its doc comment (lines 22–42).

3. Replace these lines in the component body:

```tsx
  const canOperate = useStackCapabilities(stackId)?.canOperate === true;

  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  const runsReady = runsQuery.status === "success";
  const activeRun = runsReady ? runsQuery.data.find((candidate) => !isTerminalRunStatus(candidate.status)) ?? null : null;

  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const startingOperation = startRunMutation.isPending ? startRunMutation.variables?.body.operation : undefined;
  const disabled = !canOperate || !runsReady || activeRun !== null || stackTemplate.lifecycle !== "active" || startingOperation !== undefined;
```

with:

```tsx
  const lock = useLockState(stackId, stackTemplate);

  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const startingOperation = startRunMutation.isPending ? startRunMutation.variables?.body.operation : undefined;
  const disabled = startRunLockReason(lock) !== "" || startingOperation !== undefined;
```

4. Replace `{runActionsNote({ canOperate, lifecycle: stackTemplate.lifecycle, activeRun })}` with `{runActionsNote(lock)}`.

- [ ] **Step 5: Switch the Variables and Settings tabs, and delete `useRunInFlight.ts`**

In `web/src/features/stacks/TemplateVariablesTab.tsx`:
- delete `import RequireCapability from "../../auth/RequireCapability";`;
- replace `import { runInFlightReason, useRunInFlight } from "../runs/useRunInFlight";` with `import { useLockState, variablesLockReason } from "../runs/lockReasons";`;
- delete `isDestroyingStackTemplate,` from the `./stackWorkflow` import;
- replace `const runInFlight = useRunInFlight(stackTemplate.id);` with `const lockReason = variablesLockReason(useLockState(stackId, stackTemplate));`;
- replace the `disabledReason: isDestroyingStackTemplate(stackTemplate) ? … : undefined` entry of `configPanelProps` with `disabledReason: lockReason || undefined`;
- replace the whole `<RequireCapability …>…</RequireCapability>` element with `<StackTemplateConfigPanel {...configPanelProps} />`. The reason now covers operator access, and the panel disables everything while it has one.

In `web/src/features/stacks/TemplateSettingsTab.tsx`:
- replace `import { runInFlightReason, useRunInFlight } from "../runs/useRunInFlight";` with `import { revisionLockReason, useLockState } from "../runs/lockReasons";`;
- delete `import { isDestroyingStackTemplate } from "./stackWorkflow";`;
- replace the three lines from `const destroying = …` to `const revisionLockedReason = …` with `const revisionLockedReason = revisionLockReason(useLockState(stackId, stackTemplate));`.

Then:

```bash
git rm web/src/features/runs/useRunInFlight.ts
```

- [ ] **Step 6: Give the page tests their full stops**

In `web/src/features/stacks/StackTemplatePages.test.tsx`:
- `"Apply or discard run #7 before changing the config"` → `"Apply or discard run #7 before changing the config."`
- `"Apply or discard run #7 before changing the revision"` → `"Apply or discard run #7 before changing the revision."`
- `.toBe("Destroy in progress")` → `.toBe("Destroy in progress.")`

- [ ] **Step 7: Run the tests and see them pass**

Run from `web/`: `npx vitest run src/features/runs src/features/stacks/StackTemplatePages.test.tsx`
Expected: PASS. Then `npx tsc -b --noEmit` (or `npm run build`). Expected: no errors; nothing imports `useRunInFlight`.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/runs web/src/features/stacks/TemplateVariablesTab.tsx web/src/features/stacks/TemplateSettingsTab.tsx web/src/features/stacks/StackTemplatePages.test.tsx
git commit -m "refactor(web): say why a template's controls are locked from one module

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: The Variables tab

**Files:**
- Create: `web/src/shared/fieldClass.ts`
- Modify: `web/src/features/stacks/VariableFields.tsx`
- Modify: `web/src/features/stacks/StackTemplateConfigPanel.tsx`
- Modify: `web/src/features/stacks/StackTemplateConfigPanel.test.tsx`
- Modify: `web/src/features/stacks/TemplateVariablesTab.tsx`
- Modify: `web/src/features/stacks/StackTemplatePages.test.tsx`
- Modify: `web/src/features/stacks/AddStackTemplateScreen.tsx`, `UpgradeStackTemplateScreen.tsx` (pass `emptyMessage`, now required; Tasks 5 and 6 rebuild them)
- Modify: `web/src/features/stacks/AddStackTemplateScreen.test.tsx` (the empty message gains its full stop)

**Interfaces:**
- Consumes: `variablesLockReason`, `useLockState` from Task 1.
- Produces: `export const inputClass: string`, `export const selectTriggerClass: string`, `export const fieldLabelClass: string` in `shared/fieldClass.ts`.
- Produces: `VariableFields` props `{ variables: TemplateVariable[]; variableValues: Record<string, string>; onVariableValueChange: (name: string, value: string) => void; disabled?: boolean; emptyMessage: string }` (`emptyMessage` is now required).

- [ ] **Step 1: Write the failing tests**

Replace the whole `describe` block of `web/src/features/stacks/StackTemplateConfigPanel.test.tsx` with:

```tsx
describe("StackTemplateConfigPanel", () => {
  afterEach(cleanup);

  it("offers exactly one action, Save variables", () => {
    renderPanel();

    expect(screen.getAllByRole("button")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Save variables" })).toBeTruthy();
  });

  it("reports edits to a variable value", () => {
    const props = renderPanel();

    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });

    expect(props.onVariableValueChange).toHaveBeenCalledWith("region", "eu-west-1");
  });

  it("disables save when canSave is false", () => {
    renderPanel({ canSave: false });

    expect((screen.getByRole("button", { name: "Save variables" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("puts the reason above the fields, and locks every input and the action", () => {
    renderPanel({ disabledReason: "Editing requires operator access." });

    const reason = screen.getByTestId("variables-disabled-reason");
    expect(reason.textContent).toBe("Editing requires operator access.");
    const input = screen.getByLabelText(/region/) as HTMLInputElement;
    expect(Boolean(reason.compareDocumentPosition(input) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
    expect(input.disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Save variables" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("names a required variable with an asterisk and describes it under its input", () => {
    renderPanel({ variables: [variable({ description: "Region to deploy into." })] });

    const input = screen.getByLabelText("region *");
    const describedBy = input.getAttribute("aria-describedby") ?? "";
    expect(document.getElementById(describedBy)?.textContent).toBe("Region to deploy into.");
  });

  it("describes nothing for a variable without a description", () => {
    renderPanel();

    expect(screen.getByLabelText("region *").getAttribute("aria-describedby")).toBeNull();
  });

  it("says so when the template declares no variables", () => {
    renderPanel({ variables: [], variableValues: {} });

    expect(screen.getByText("This template declares no variables.")).toBeTruthy();
  });
});
```

In `web/src/features/stacks/StackTemplatePages.test.tsx`, inside `describe("TemplateVariablesTab")`:
- replace every `actionButton(/Save config/)` with `actionButton(/Save variables/)` (also in `describe("editing while a run is in flight")`);
- in `"locks configuration when canOperate is denied"`, replace its `waitFor` line with:

```tsx
    await waitFor(() => expect(screen.getByTestId("variables-disabled-reason").textContent).toBe("Editing requires operator access."));
```

- add this test at the end of `describe("TemplateVariablesTab")`:

```tsx
  // A run that starts underneath unsaved edits locks them but keeps them:
  // whoever typed them can still read them, and SessionProvider still sees
  // the tab as unsaved.
  it("keeps unsaved edits, locked, when a run starts underneath them", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    // The waiting run makes the panel refetch the stack: answer that with the
    // seeded stack, and leave every other request pending.
    vi.spyOn(globalThis, "fetch").mockImplementation((input) =>
      String(input).endsWith("/stacks/stack_1")
        ? Promise.resolve(
            new Response(JSON.stringify(stackView(allAllowed, [stackTemplate()])), { status: 200, headers: { "content-type": "application/json" } })
          )
        : new Promise<Response>(() => {})
    );

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/variables");
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });

    act(() => {
      queryClient.setQueryData(queryKeys.templateRuns("tenant_123", "st_1"), [runFor("st_1", { status: "waiting_approval", run_number: 7 })]);
    });

    await waitFor(() => expect(screen.getByTestId("variables-disabled-reason").textContent).toBe("Apply or discard run #7 before changing the config."));
    const input = screen.getByLabelText(/region/) as HTMLInputElement;
    expect(input.value).toBe("eu-west-1");
    expect(input.disabled).toBe(true);
    expect(actionButton(/Save variables/).disabled).toBe(true);
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run from `web/`: `npx vitest run src/features/stacks/StackTemplateConfigPanel.test.tsx src/features/stacks/StackTemplatePages.test.tsx`
Expected: FAIL on "Save variables", the asterisk label's description, and the empty message.

- [ ] **Step 3: Create `web/src/shared/fieldClass.ts`**

```ts
import { cn } from "@/lib/utils";

// openplan UI's form controls, drawn on shadcn's Input and SelectTrigger:
// 36px tall, 13px mono text, a 12px inset, and focus as the 2px ring outline
// every other control has, in place of the vendored translucent ring.
const FOCUS = cn(
  "focus-visible:border-input focus-visible:ring-0",
  "focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
);

/** For shadcn's Input. md:text-meta outranks the Input's own md:text-sm. */
export const inputClass = cn("h-9 px-3 font-mono text-meta md:text-meta placeholder:text-subtle-foreground pointer-coarse:h-11", FOCUS);

/** For shadcn's SelectTrigger at its default size; its chevron keeps the right inset. */
export const selectTriggerClass = cn(
  "w-full pl-3 font-mono text-meta data-[size=default]:h-9 pointer-coarse:data-[size=default]:h-11",
  FOCUS
);

/** A sans field label above its control: 13px at weight 500. */
export const fieldLabelClass = "text-meta leading-label font-medium";
```

- [ ] **Step 4: Rewrite `web/src/features/stacks/VariableFields.tsx`**

```tsx
import { useId } from "react";
import type { TemplateVariable } from "../../api/types";
import { inputClass } from "../../shared/fieldClass";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface VariableFieldsProps {
  variables: TemplateVariable[];
  variableValues: Record<string, string>;
  onVariableValueChange: (name: string, value: string) => void;
  disabled?: boolean;
  /** What to say when the revision declares no variables. */
  emptyMessage: string;
}

/**
 * The variable fields, and nothing else: no mutations, no revision choice, no
 * actions. Shared by Variables, Add template and Change revision, so all
 * three draw variables the same way.
 *
 * One column, at most 560px: each variable's name as a mono label (" *" when
 * it is required), its input, and its description under it. Each input's id
 * comes from useId, so a label always names its own input.
 */
export default function VariableFields({ variables, variableValues, onVariableValueChange, disabled = false, emptyMessage }: VariableFieldsProps) {
  const idPrefix = useId();
  if (variables.length === 0) {
    return <p className="text-meta text-muted-foreground">{emptyMessage}</p>;
  }
  return (
    <div className="flex max-w-140 flex-col gap-4">
      {variables.map((variable, index) => {
        const id = `${idPrefix}-${index}`;
        const descriptionId = `${id}-description`;
        const description = variable.description.trim();
        return (
          <div key={variable.name} className="flex flex-col gap-1.5">
            {/* Variable names are identifiers with no spaces, so a long one
                breaks anywhere rather than widening the column. */}
            <Label htmlFor={id} className="font-mono text-meta leading-label wrap-anywhere">
              {variable.name}
              {variable.required ? " *" : ""}
            </Label>
            <Input
              id={id}
              value={variableValues[variable.name] ?? ""}
              onChange={(event) => onVariableValueChange(variable.name, event.target.value)}
              placeholder={variable.type_expression || "value"}
              disabled={disabled}
              aria-describedby={description ? descriptionId : undefined}
              className={inputClass}
            />
            {description && (
              <p id={descriptionId} className="text-xs text-muted-foreground">
                {description}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}
```

- [ ] **Step 5: Pass `emptyMessage` where it was optional**

`emptyMessage` is now required and every message is a sentence:
- `AddStackTemplateScreen.tsx`: `emptyMessage="This template declares no variables"` → `emptyMessage="This template declares no variables."`
- `UpgradeStackTemplateScreen.tsx`: `emptyMessage="This revision declares no variables"` → `emptyMessage="This revision declares no variables."`
- `AddStackTemplateScreen.test.tsx`: both `queryByText("This template declares no variables")` → `queryByText("This template declares no variables.")`. Without the full stop they would pass for the wrong reason.

- [ ] **Step 6: Rewrite `web/src/features/stacks/StackTemplateConfigPanel.tsx`**

```tsx
import { Loader2 } from "lucide-react";
import type { TemplateVariable } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import VariableFields from "./VariableFields";
import { cn } from "@/lib/utils";

interface StackTemplateConfigPanelProps {
  variables: TemplateVariable[];
  variableValues: Record<string, string>;
  onVariableValueChange: (name: string, value: string) => void;
  canSave: boolean;
  onSave: () => void;
  saveBusy: boolean;
  // When set, it leads the panel, and the inputs and the action are
  // disabled: the capability gate's denied-with-reason state (AUTH-020).
  disabledReason?: string;
}

/**
 * The installed template's variables, for its desired revision, with one
 * action. Adding and changing the revision live on their own views, so this
 * panel never switches modes.
 */
export default function StackTemplateConfigPanel({
  variables,
  variableValues,
  onVariableValueChange,
  canSave,
  onSave,
  saveBusy,
  disabledReason
}: StackTemplateConfigPanelProps) {
  const locked = Boolean(disabledReason);
  return (
    <div className="flex min-w-0 flex-col gap-5" data-testid="stack-template-config">
      {disabledReason && (
        <p className="text-meta text-muted-foreground" data-testid="variables-disabled-reason">
          {disabledReason}
        </p>
      )}
      <VariableFields
        variables={variables}
        variableValues={variableValues}
        onVariableValueChange={onVariableValueChange}
        disabled={locked}
        emptyMessage="This template declares no variables."
      />
      <button
        type="button"
        className={cn(buttonClass("primary"), "self-start pointer-coarse:h-11")}
        disabled={locked || !canSave || saveBusy}
        onClick={onSave}
      >
        {saveBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
        Save variables
      </button>
    </div>
  );
}
```

- [ ] **Step 7: Rewrite `web/src/features/stacks/TemplateVariablesTab.tsx`**

```tsx
import { useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { useTemplateRevisionVariablesQuery, useUpdateStackTemplateConfigMutation } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { useLockState, variablesLockReason } from "../runs/lockReasons";
import StackTemplateConfigPanel from "./StackTemplateConfigPanel";
import { useStackTemplate } from "./stackTemplateContext";
import { canSaveInstalledTemplateConfig, configFromVariableValues, variableValuesFromConfig } from "./stackWorkflow";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/variables — the installed
// template's configuration. Choosing another revision is a separate view
// (upgrade), reached from Settings, so this form has exactly one action.
export default function TemplateVariablesTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");

  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, stackTemplate.desired_template_revision_id);
  const variables = variablesQuery.data ?? [];
  const boundary = useQueryErrorBoundary(variablesQuery.error);
  const updateStackTemplateConfigMutation = useUpdateStackTemplateConfigMutation(tenantID, stackId);
  const lockReason = variablesLockReason(useLockState(stackId, stackTemplate));

  // Displayed values are the installed config overlaid with unsaved edits.
  const baseValues = variableValuesFromConfig(stackTemplate.config, variables);
  const variableValues: Record<string, string> = {};
  for (const variable of variables) {
    variableValues[variable.name] = editedValues[variable.name] ?? baseValues[variable.name] ?? "";
  }

  const canSaveConfig = canSaveInstalledTemplateConfig(stackTemplate, variables, variableValues);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so an in-flight
  // edit here is never wiped out by a background sign-in redirect. A lock
  // keeps the edits, so it keeps the marker too.
  const hasUnsavedConfig = Object.keys(editedValues).length > 0;

  async function handleSave() {
    if (!canSaveConfig || lockReason !== "") {
      return;
    }
    setErrorMessage("");
    try {
      await updateStackTemplateConfigMutation.mutateAsync({
        stackTemplateID: stackTemplate.id,
        body: { config: configFromVariableValues(variables, variableValues) }
      });
      setEditedValues({});
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  if (variablesQuery.status === "pending") {
    return (
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="template-variables-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables…
      </p>
    );
  }

  if (variablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <div className="flex flex-col items-start gap-3" data-testid="template-variables-error">
        <ErrorLine live={false}>Something went wrong while loading the template's variables.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="template-variables-retry"
          onClick={() => void variablesQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3" data-testid="template-variables-tab" data-unsaved={hasUnsavedConfig ? "true" : undefined}>
      <StackTemplateConfigPanel
        variables={variables}
        variableValues={variableValues}
        onVariableValueChange={(name, value) => setEditedValues((current) => ({ ...current, [name]: value }))}
        canSave={canSaveConfig}
        onSave={() => void handleSave()}
        saveBusy={updateStackTemplateConfigMutation.isPending}
        disabledReason={lockReason || undefined}
      />
      {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
    </div>
  );
}
```

- [ ] **Step 8: Run the tests and see them pass**

Run from `web/`: `npx vitest run src/features/stacks`
Expected: PASS, including `AddStackTemplateScreen.test.tsx` and `UpgradeStackTemplateScreen.test.tsx` (their `getByLabelText(/region/)` still finds "region *").

- [ ] **Step 9: Commit**

```bash
git add web/src/shared/fieldClass.ts web/src/features/stacks
git commit -m "feat(web): rebuild the Variables tab on openplan UI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Credentials, here and on Environment

**Files:**
- Modify: `web/src/shared/buttonClass.ts`, `web/src/shared/buttonClass.test.ts`
- Modify: `web/src/features/stacks/CredentialsPanel.tsx`
- Modify: `web/src/features/stacks/TemplateCredentialsTab.tsx`
- Modify: `web/src/features/stacks/EnvironmentScreen.tsx`
- Modify: `web/src/features/stacks/EnvironmentScreen.test.tsx`
- Modify: `web/src/features/stacks/StackTemplatePages.test.tsx`

**Interfaces:**
- Consumes: `inputClass`, `fieldLabelClass` from Task 2.
- Produces: `buttonClass("icon")`: a 32px square ghost button in `muted-foreground`, no side padding, the ring outline on focus.
- Produces: `CredentialsPanel` props `{ note: string; emptyTitle: string; emptyDescription: string; credentials: CredentialMetadata[]; loading: boolean; busy: boolean; onCreate: (name: string, value: string) => Promise<void>; onDelete: (id: string) => Promise<void> }`.

- [ ] **Step 1: Write the failing tests**

In `web/src/shared/buttonClass.test.ts`, change the focus test's loop to `for (const variant of ["primary", "outline", "section", "icon"] as const)`, and add:

```ts
  it("draws an icon button 32px square in muted text, with no side padding", () => {
    const icon = classes(buttonClass("icon"));
    expect(icon).toEqual(expect.arrayContaining(["size-8", "text-muted-foreground"]));
    expect(icon).not.toContain("px-3");
  });
```

In `web/src/features/stacks/EnvironmentScreen.test.tsx`:
- replace every `"Environment credential name"` and `/Environment credential name/` with `"Name"`, and every `"Environment credential value"` and `/Environment credential value/` with `"Value"`;
- replace every `{ name: "Add" }` and `{ name: /Add/ }` with `{ name: "Add credential" }`;
- replace every `screen.findByRole("cell", { name: "TF_VAR_TEST" })` and `screen.getByRole("cell", { name: "TF_VAR_TEST" })` with `screen.findByText("TF_VAR_TEST")` and `screen.getByText("TF_VAR_TEST")`;
- delete the test `"titles the panel with an h2 that sets its own type"`: the panel has no heading now; the breadcrumb's h1 names the page;
- replace the tests `"lists the credentials in a table, one row each, with a delete button"` and `"says so when no credentials are configured"` with:

```tsx
  // Midday times: the date is drawn in the runner's time zone, which the
  // test config does not pin, and midnight UTC is the day before in America.
  it("lists the credentials, one row each, with when each was added and a delete button", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { ...credential, created_at: "2026-07-19T12:00:00Z" },
      { ...credential, id: "credential_2", name: "AWS_ACCESS_KEY_ID", created_at: "2026-08-12T12:00:00Z" }
    ]);

    renderScreen(queryClient);

    const rows = within(screen.getByRole("list")).getAllByRole("listitem");
    expect(rows.map((row) => row.textContent)).toEqual(["TF_VAR_TESTAdded 19 Jul 2026", "AWS_ACCESS_KEY_IDAdded 12 Aug 2026"]);
    expect(within(rows[1]).getByRole("button", { name: "Delete AWS_ACCESS_KEY_ID" })).toBeTruthy();
  });

  // A name longer than the row truncates, and its title keeps the whole name
  // readable on hover.
  it("keeps a long credential name whole in its title", () => {
    const longName = "TF_VAR_A_VERY_LONG_CREDENTIAL_NAME_THAT_WILL_NOT_FIT_ON_A_PHONE_ROW";
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [{ ...credential, name: longName }]);

    renderScreen(queryClient);

    expect(screen.getByText(longName).getAttribute("title")).toBe(longName);
  });

  it("says so when no credentials are configured, in place of the list", () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), []);

    renderScreen(queryClient);

    expect(screen.getByText("No credentials in this environment")).toBeTruthy();
    expect(screen.getByText("Credentials added here are available to every template in this stack.")).toBeTruthy();
    expect(screen.queryByRole("list")).toBeNull();
  });
```

In `web/src/features/stacks/StackTemplatePages.test.tsx`, replace `describe("TemplateCredentialsTab")`'s first test with:

```tsx
  it("shows only this template's credentials, under the note on what they override", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.stackCredentials("tenant_123", "stack_1"), [
      { id: "stack_credential", name: "STACK_ONLY", scope: "stack", created_at: "2026-07-19T00:00:00Z" }
    ]);
    queryClient.setQueryData(queryKeys.stackTemplateCredentials("tenant_123", "st_1"), [
      { id: "template_credential", name: "TEMPLATE_ONLY", scope: "stack_template", created_at: "2026-07-19T00:00:00Z" }
    ]);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/credentials");

    expect(screen.getByText("TEMPLATE_ONLY")).toBeTruthy();
    expect(screen.queryByText("STACK_ONLY")).toBeNull();
    expect(screen.getByText("Overrides the stack environment for this template only.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Delete TEMPLATE_ONLY" })).toBeTruthy();
  });

  it("says its runs use the stack environment when it has no credentials of its own", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.stackTemplateCredentials("tenant_123", "st_1"), []);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/credentials");

    expect(screen.getByText("No credentials for this template")).toBeTruthy();
    expect(screen.getByText("Its runs use the stack environment.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add credential" })).toBeTruthy();
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run from `web/`: `npx vitest run src/shared/buttonClass.test.ts src/features/stacks/EnvironmentScreen.test.tsx src/features/stacks/StackTemplatePages.test.tsx`
Expected: FAIL: no `icon` variant, no "Name"/"Value" labels, no list.

- [ ] **Step 3: Add the `icon` variant to `web/src/shared/buttonClass.ts`**

Replace everything from `const SHARED = cn(` to the end of the file with:

```ts
const PADDING = "px-3 has-data-[icon=inline-start]:pl-2.5 has-data-[icon=inline-end]:pr-2.5";
const FOCUS = "focus-visible:ring-0 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring";

// The icon variant is a lone icon in a row, such as a credential's delete: a
// 32px square with no fill until hovered, its icon in muted text. It takes
// an aria-label, since it has no words.
const VARIANTS = {
  primary: cn(buttonVariants({ variant: "default" }), PADDING, FOCUS, "focus-visible:border-transparent"),
  outline: cn(buttonVariants({ variant: "outline" }), PADDING, FOCUS, "focus-visible:border-border"),
  section: cn(
    buttonVariants({ variant: "outline" }),
    PADDING,
    FOCUS,
    "focus-visible:border-border px-2.5 text-meta has-data-[icon=inline-start]:pl-2.5"
  ),
  icon: cn(buttonVariants({ variant: "ghost", size: "icon" }), FOCUS, "focus-visible:border-transparent text-muted-foreground")
};

/** The classes of an openplan UI button: 36px tall at "lg", 32px otherwise. */
export function buttonClass(variant: keyof typeof VARIANTS = "primary", size: "default" | "lg" = "default"): string {
  return cn(VARIANTS[variant], size === "lg" && "h-9");
}
```

Also change the comment above it from "Two things differ" through the section variant's note so it ends: "The section variant is the outline button in a preview panel's section nav: 10px at both sides and 13px text. The icon variant is described below."

- [ ] **Step 4: Rewrite `web/src/features/stacks/CredentialsPanel.tsx`**

```tsx
import { useId, useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import type { CredentialMetadata } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

interface CredentialsPanelProps {
  /** What these credentials are for, above the list. */
  note: string;
  /** The empty state's title and its line, shown in place of an empty list. */
  emptyTitle: string;
  emptyDescription: string;
  credentials: CredentialMetadata[];
  loading: boolean;
  busy: boolean;
  onCreate: (name: string, value: string) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

/**
 * Write-only credentials for a stack or for one of its templates: a note on
 * what they are for, the list (the name, when it was added, and delete), and
 * the form that adds one. A value never comes back from the server, so the
 * list has only names. The copy comes from the caller: the template's tab and
 * the stack's Environment page share this panel.
 */
export default function CredentialsPanel({ note, emptyTitle, emptyDescription, credentials, loading, busy, onCreate, onDelete }: CredentialsPanelProps) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const nameId = useId();
  const valueId = useId();

  /** Validates the form, sends the value once, then clears it from local state. */
  async function submit() {
    if (!name.trim() || !value) {
      setError("Name and value are required");
      return;
    }
    setError("");
    const sentName = name;
    const sentValue = value;
    try {
      await onCreate(name.trim(), value);
      // The fields stay editable while the request is out. Clear only what
      // still holds what was sent, so the next credential, half typed, stays.
      setName((current) => (current === sentName ? "" : current));
      setValue((current) => (current === sentValue ? "" : current));
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Request failed");
    }
  }

  /** Deletes one credential, and says why in the same line when that fails. */
  async function remove(id: string) {
    setError("");
    try {
      await onDelete(id);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Request failed");
    }
  }

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted. A half-typed
  // credential secret is the worst instance of the loss this guard exists to
  // prevent: the value never reaches the server until it is added, so losing
  // it means retyping a password from scratch.
  const hasUnsavedCredential = name !== "" || value !== "";

  return (
    <div className="flex min-w-0 flex-col gap-5" data-unsaved={hasUnsavedCredential ? "true" : undefined}>
      <p className="text-meta text-muted-foreground">{note}</p>
      {loading ? (
        <p className="flex items-center gap-2 text-meta text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading credentials…
        </p>
      ) : credentials.length === 0 ? (
        <Empty className="gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-7">
          <EmptyHeader className="gap-2">
            <p className="text-sm font-medium">{emptyTitle}</p>
            <EmptyDescription className="text-meta">{emptyDescription}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="divide-y divide-divider overflow-hidden rounded-lg border">
          {credentials.map((credential) => (
            <li key={credential.id} className="flex min-h-12 items-center gap-4 py-2 pr-2 pl-4">
              <span className="min-w-0 flex-1 truncate font-mono text-meta" title={credential.name}>
                {credential.name}
              </span>
              <span className="shrink-0 text-meta text-muted-foreground sm:w-45">Added {formatTimestamp(credential.created_at)}</span>
              <button
                type="button"
                className={cn(buttonClass("icon"), "pointer-coarse:size-11")}
                disabled={busy}
                onClick={() => void remove(credential.id)}
                aria-label={`Delete ${credential.name}`}
              >
                <Trash2 aria-hidden="true" className="size-3.5" strokeWidth={2.25} />
              </button>
            </li>
          ))}
        </ul>
      )}
      {/* Name, Value and Add credential on one row, wrapping on a phone. */}
      <div className="flex flex-wrap items-end gap-2">
        <div className="flex min-w-0 flex-1 basis-50 flex-col gap-1.5">
          <Label htmlFor={nameId} className={fieldLabelClass}>
            Name
          </Label>
          <Input id={nameId} placeholder="AWS_ACCESS_KEY_ID" value={name} onChange={(event) => setName(event.target.value)} className={inputClass} />
        </div>
        <div className="flex min-w-0 flex-1 basis-50 flex-col gap-1.5">
          <Label htmlFor={valueId} className={fieldLabelClass}>
            Value
          </Label>
          <Input
            id={valueId}
            type="password"
            placeholder="Secret value"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            className={inputClass}
          />
        </div>
        <button type="button" className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")} disabled={busy} onClick={() => void submit()}>
          Add credential
        </button>
      </div>
      {error && <ErrorLine>{error}</ErrorLine>}
    </div>
  );
}
```

- [ ] **Step 5: Give the two callers their copy**

In `web/src/features/stacks/TemplateCredentialsTab.tsx`, replace the `title` and `subtitle` props with:

```tsx
        note="Overrides the stack environment for this template only."
        emptyTitle="No credentials for this template"
        emptyDescription="Its runs use the stack environment."
```

and change its wrapper's class to `flex min-w-0 flex-col` (it was `grid min-w-0 grid-cols-1 content-start gap-6`).

In `web/src/features/stacks/EnvironmentScreen.tsx`, replace `title="Environment credentials"` with:

```tsx
        note="Values are write-only and injected only when Terraform runs. Use TF_VAR_NAME for Terraform variables; provider credentials keep their provider-specific names."
        emptyTitle="No credentials in this environment"
        emptyDescription="Credentials added here are available to every template in this stack."
```

- [ ] **Step 6: Run the tests and see them pass**

Run from `web/`: `npx vitest run src/shared src/features/stacks`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/shared/buttonClass.ts web/src/shared/buttonClass.test.ts web/src/features/stacks
git commit -m "feat(web): rebuild credentials on openplan UI, for the tab and Environment

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: The Settings tab

**Files:**
- Modify: `web/src/features/templates/templateWorkflow.ts`, `web/src/features/templates/templateWorkflow.test.ts`
- Create: `web/src/features/stacks/SettingsSection.tsx`
- Modify: `web/src/features/stacks/TemplateSettingsTab.tsx`
- Modify: `web/src/features/runs/TemplateDestroyPanel.tsx`
- Modify: `web/src/features/runs/TemplateDestroyPanel.test.tsx`
- Modify: `web/src/features/stacks/StackTemplatePages.test.tsx`

**Interfaces:**
- Consumes: `useLockState`, `revisionLockReason`, `destroyLockReason` from Task 1; `buttonClass` from Task 3; `stackTemplatePath` from `templateSelection.ts`.
- Produces: `export function revisionSourceLabel(revision: Pick<TemplateRevision, "root_path" | "repo_owner" | "repo_name">): string` in `templateWorkflow.ts`.
- Produces: `export default function SettingsSection(props: { title: string; description: ReactNode; reason: string; reasonTestId: string; error?: ReactNode; action: ReactNode; testId: string })`.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/templates/templateWorkflow.test.ts`, add `revisionSourceLabel` to the import from `./templateWorkflow`, and add:

```ts
describe("revisionSourceLabel", () => {
  it("names a revision by its root path", () => {
    expect(revisionSourceLabel(templateRevision({ root_path: "aws/eks", repo_owner: "acme", repo_name: "infra-modules" }))).toBe("aws/eks");
  });

  it("names the repository for a template at its root", () => {
    for (const rootPath of [".", "", "  "]) {
      expect(revisionSourceLabel(templateRevision({ root_path: rootPath, repo_owner: "acme", repo_name: "infra-modules" }))).toBe("acme/infra-modules");
    }
  });
});
```

In `web/src/features/runs/TemplateDestroyPanel.test.tsx`:
- in `"never offers auto-approve on a destroy"`, replace the two `data-slot` assertions with `expect(screen.getByRole("region", { name: "Destroy" })).toBeTruthy();`;
- in `"keeps destroy disabled until run history has loaded"`, add `expect(screen.getByTestId("template-destroy-disabled-reason").textContent).toBe("Loading runs…");`;
- in `"disables destroy with a reason when canOperate is denied"`, replace its last line with `expect(screen.getByTestId("template-destroy-disabled-reason").textContent).toBe("Destroying requires operator access.");`;
- add, at the end of the `describe`:

```tsx
  it.each([
    [{ lifecycle: "destroying" }, [], "Destroy in progress."],
    [{ lifecycle: "failed" }, [], "A template whose destroy failed cannot start runs."],
    [{}, [run({ status: "waiting_approval", run_number: 7 })], "Apply or discard run #7 before destroying."]
  ] as const)("disables destroy and says why (%o)", (overrides, runs, reason) => {
    const queryClient = testQueryClient();
    seedCapabilities(queryClient, allAllowed);
    seedRuns(queryClient, [...runs]);

    renderPanel(queryClient, overrides);

    expect(isDisabled(screen.getByRole("button", { name: /^Destroy$/ }))).toBe(true);
    expect(screen.getByTestId("template-destroy-disabled-reason").textContent).toBe(reason);
  });
```

In `web/src/features/stacks/StackTemplatePages.test.tsx`, replace the whole `describe("TemplateSettingsTab")` with:

```tsx
describe("TemplateSettingsTab", () => {
  it("offers changing the revision above destroy", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    const revisionAction = screen.getByRole("region", { name: "Revision" });
    expect(screen.getByTestId("change-stack-template-revision-link").getAttribute("href")).toBe("/stacks/stack_1/templates/st_1/upgrade");
    expect(precedes(revisionAction, screen.getByRole("region", { name: "Destroy" }))).toBe(true);
  });

  it("names the revision it runs and where that comes from", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision({ root_path: "aws/eks" })]);
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(allAllowed, [stackTemplate({ source_ref: "v1.4.0" })]));

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    expect(screen.getByText(/^This template runs revision/).textContent).toBe("This template runs revision v1.4.0 of aws/eks.");
  });

  it("names the repository for a template at its root", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    expect(screen.getByText(/^This template runs revision/).textContent).toBe("This template runs revision main of hashicorp/vpc.");
  });

  it("does not wait for the tenant revision list before offering a revision change", () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.removeQueries({ queryKey: queryKeys.templateRevisions("tenant_123") });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    expect(screen.getByTestId("change-stack-template-revision-link").getAttribute("href")).toBe("/stacks/stack_1/templates/st_1/upgrade");
    expect(screen.getByText(/^This template runs revision/).textContent).toBe("This template runs revision main.");
  });

  // Both settings show to every viewer. One that cannot be used is disabled,
  // and its section says why.
  it("shows both settings to a viewer without operator access, each disabled with why", async () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient, { ...allAllowed, canOperate: false });

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    await waitFor(() =>
      expect(screen.getByTestId("upgrade-disabled-reason").textContent).toBe("Changing the revision requires operator access.")
    );
    const changeRevision = screen.getByTestId("change-stack-template-revision-link") as HTMLButtonElement;
    expect(changeRevision.tagName).toBe("BUTTON");
    expect(changeRevision.disabled).toBe(true);
    expect(screen.getByTestId("template-destroy-disabled-reason").textContent).toBe("Destroying requires operator access.");
    expect(actionButton(/^Destroy$/).disabled).toBe(true);
  });

  it("disables revision selection while the template is destroying", () => {
    const queryClient = testQueryClient();
    seedDefaultData(queryClient);
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView(allAllowed, [stackTemplate({ lifecycle: "destroying" })]));

    renderAt(queryClient, "/stacks/stack_1/templates/st_1/settings");

    const changeRevisionControl = screen.getByTestId("change-stack-template-revision-link") as HTMLButtonElement;
    expect(changeRevisionControl.tagName).toBe("BUTTON");
    expect(changeRevisionControl.disabled).toBe(true);
    expect(screen.getByTestId("upgrade-disabled-reason").textContent).toBe("Destroy in progress.");
  });
});
```

- [ ] **Step 2: Run the tests and see them fail**

Run from `web/`: `npx vitest run src/features/templates/templateWorkflow.test.ts src/features/runs/TemplateDestroyPanel.test.tsx src/features/stacks/StackTemplatePages.test.tsx`
Expected: FAIL: no `revisionSourceLabel`, no "Revision"/"Destroy" regions, Change revision hidden from non-operators.

- [ ] **Step 3: Add `revisionSourceLabel` to `web/src/features/templates/templateWorkflow.ts`**

After `templateRootPathLabel`:

```ts
/**
 * Where a revision's code lives, as a person would name it: its root path,
 * or the repository for a template that sits at the repository's root.
 */
export function revisionSourceLabel(revision: Pick<TemplateRevision, "root_path" | "repo_owner" | "repo_name">): string {
  const rootPath = revision.root_path.trim();
  return rootPath === "" || rootPath === "." ? `${revision.repo_owner}/${revision.repo_name}` : rootPath;
}
```

- [ ] **Step 4: Create `web/src/features/stacks/SettingsSection.tsx`**

```tsx
import { useId } from "react";
import type { ReactNode } from "react";

// One setting on a template's Settings tab: a bordered row with what it does
// at the left and its button at the right, the button wrapping under the text
// on a phone. A locked button's reason sits under the text, so a disabled
// button never stands unexplained.
export default function SettingsSection({
  title,
  description,
  reason,
  reasonTestId,
  error,
  action,
  testId
}: {
  title: string;
  description: ReactNode;
  /** Why the action is locked, or "" when it is free. */
  reason: string;
  reasonTestId: string;
  /** A refused request, as an ErrorLine. */
  error?: ReactNode;
  action: ReactNode;
  testId: string;
}) {
  const headingId = useId();
  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-lg border px-5 py-4"
      data-testid={testId}
    >
      <div className="flex min-w-0 flex-1 basis-60 flex-col gap-1">
        <h3 id={headingId} className="text-sm font-semibold">
          {title}
        </h3>
        <p className="text-meta text-muted-foreground wrap-anywhere">{description}</p>
        {reason && (
          <p className="text-meta text-muted-foreground" data-testid={reasonTestId}>
            {reason}
          </p>
        )}
        {error}
      </div>
      {action}
    </section>
  );
}
```

- [ ] **Step 5: Rewrite `web/src/features/stacks/TemplateSettingsTab.tsx`**

```tsx
import { Link } from "react-router-dom";
import { useTemplateRevisionsQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import TemplateDestroyPanel from "../runs/TemplateDestroyPanel";
import { revisionLockReason, useLockState } from "../runs/lockReasons";
import { revisionSourceLabel } from "../templates/templateWorkflow";
import SettingsSection from "./SettingsSection";
import { useStackTemplate } from "./stackTemplateContext";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/settings — the actions that
// change what the template is rather than run it: choosing another revision,
// and destroying it. They live here, a tab away from Plan, so the
// irreversible one is never under the cursor of routine work.
export default function TemplateSettingsTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  const reason = revisionLockReason(useLockState(stackId, stackTemplate));
  // Read only to name where the revision comes from. The sentence stands
  // without it while the tenant's revisions load.
  const revision =
    useTemplateRevisionsQuery(tenantID).data?.find((candidate) => candidate.id === stackTemplate.desired_template_revision_id) ?? null;
  const actionClass = cn(buttonClass("outline"), "pointer-coarse:h-11");

  return (
    <div className="flex min-w-0 flex-col gap-5" data-testid="template-settings-tab">
      <SettingsSection
        title="Revision"
        testId="stack-template-revision-action"
        description={
          <>
            This template runs revision <span className="font-mono text-code-foreground">{stackTemplate.source_ref}</span>
            {revision && (
              <>
                {" "}
                of <span className="font-mono text-code-foreground">{revisionSourceLabel(revision)}</span>
              </>
            )}
            .
          </>
        }
        reason={reason}
        reasonTestId="upgrade-disabled-reason"
        action={
          reason ? (
            <button type="button" disabled className={actionClass} data-testid="change-stack-template-revision-link">
              Change revision
            </button>
          ) : (
            <Link
              to={`/stacks/${stackId}/templates/${stackTemplate.id}/upgrade`}
              className={actionClass}
              data-testid="change-stack-template-revision-link"
            >
              Change revision
            </Link>
          )
        }
      />
      <TemplateDestroyPanel stackId={stackId} stackTemplate={stackTemplate} />
    </div>
  );
}
```

- [ ] **Step 6: Rewrite `web/src/features/runs/TemplateDestroyPanel.tsx`**

```tsx
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useStartTemplateRunMutation } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import SettingsSection from "../stacks/SettingsSection";
import { stackTemplatePath } from "../stacks/templateSelection";
import { destroyLockReason, useLockState } from "./lockReasons";
import { isRunInFlightError } from "./runErrors";

interface TemplateDestroyPanelProps {
  stackId: string;
  stackTemplate: StackTemplate;
}

// Destroy lives on the Settings tab, a tab away from Plan, because it is the
// one operation that cannot be undone.
//
// Destroy here only plans the destroy: it shows what would be destroyed and
// destroys nothing. Destroying happens when that plan is approved, on its
// run, which is why the red "Destroy N resources" confirmation lives there. A
// destroy is never auto-approved, so that confirmation is always the
// irreversible click.
export default function TemplateDestroyPanel({ stackId, stackTemplate }: TemplateDestroyPanelProps) {
  const [errorMessage, setErrorMessage] = useState("");
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const reason = destroyLockReason(useLockState(stackId, stackTemplate));
  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const destroyBusy = startRunMutation.isPending;

  async function handleDestroy() {
    // Re-read the runs at click time: a run may have started since render.
    const currentRuns = queryClient.getQueryData<TemplateRun[]>(queryKeys.templateRuns(tenantID, stackTemplate.id));
    const currentRunActive = currentRuns?.some((candidate) => !isTerminalRunStatus(candidate.status)) ?? true;
    if (reason !== "" || currentRunActive) {
      return;
    }
    setErrorMessage("");
    try {
      await startRunMutation.mutateAsync({ stackTemplateID: stackTemplate.id, body: { operation: "destroy" } });
      await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
      // The plan, and the button that destroys, are on the Runs tab.
      navigate(stackTemplatePath(stackId, stackTemplate.id, "runs"));
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
      if (isRunInFlightError(error)) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
      }
    }
  }

  return (
    <SettingsSection
      title="Destroy"
      testId="template-destroy-panel"
      description="Plans the removal of everything this template manages. Nothing is removed until that plan is approved."
      reason={reason}
      reasonTestId="template-destroy-disabled-reason"
      error={errorMessage ? <ErrorLine testId="template-destroy-error">{errorMessage}</ErrorLine> : null}
      action={
        <button
          type="button"
          className={cn(buttonClass("outline"), "text-destructive hover:text-destructive pointer-coarse:h-11")}
          disabled={reason !== "" || destroyBusy}
          onClick={() => void handleDestroy()}
        >
          {destroyBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
          Destroy
        </button>
      }
    />
  );
}
```

- [ ] **Step 7: Run the tests and see them pass**

Run from `web/`: `npx vitest run src/features/templates src/features/runs src/features/stacks`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/templates web/src/features/runs web/src/features/stacks
git commit -m "feat(web): rebuild the Settings tab as two sections, each saying why it is locked

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Change revision

**Files:**
- Create: `web/src/features/stacks/PanelTrail.tsx`
- Modify: `web/src/features/runs/RunDetailScreen.tsx`
- Modify: `web/src/features/stacks/UpgradeStackTemplateScreen.tsx`
- Modify: `web/src/features/stacks/UpgradeStackTemplateScreen.test.tsx`

**Interfaces:**
- Consumes: `useStackTemplate` (`stackTemplateContext.ts`), `stackTemplatePath` (`templateSelection.ts`), `fieldLabelClass`, `selectTriggerClass` (Task 2), `buttonClass`, `ErrorLine`.
- Produces: `export default function PanelTrail(props: { name: string; parent: { label: string; to: string }; current: string })`.

- [ ] **Step 1: Write the failing tests**

In `web/src/features/stacks/UpgradeStackTemplateScreen.test.tsx`:

1. Add `import { StackTemplateContext } from "./stackTemplateContext";` to the imports.
2. Replace `targetSelect` with `const targetSelect = () => screen.getByRole("combobox", { name: "Revision" });`.
3. Replace `renderScreen` with:

```tsx
// The screen renders inside the template's panel, which provides the template
// through context; the panel's own tests cover a template the stack lacks.
function renderScreen(queryClient: QueryClient, template: StackTemplate = stackTemplate()) {
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <MemoryRouter initialEntries={["/stacks/stack_1/templates/st_1/upgrade"]}>
          <Routes>
            <Route
              path="/stacks/:stackId/templates/:stackTemplateId/upgrade"
              element={
                <StackTemplateContext.Provider value={{ stackId: "stack_1", stackTemplate: template }}>
                  <UpgradeStackTemplateScreen />
                </StackTemplateContext.Provider>
              }
            />
            <Route path="/stacks/:stackId/templates/:stackTemplateId/:tab" element={<LocationProbe />} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
```

4. In `"upgrades with a config built only from the target revision's variables"`, expect `"/stacks/stack_1/templates/st_1/runs"` instead of `"/stacks/stack_1/templates/st_1"`.
5. In `"guards the direct-URL path when the installed template is mid-destroy"`, delete the `queryClient.setQueryData(queryKeys.stack(…), stackView([stackTemplate({ lifecycle: "destroying" })]))` call and render with `renderScreen(queryClient, stackTemplate({ lifecycle: "destroying" }));`.
6. Delete the test `"renders not found when the stack template id is not installed"`.
7. Add, at the end of the `describe`:

```tsx
  it("leads with a crumb back to Settings", () => {
    const queryClient = testQueryClient();
    seedUpgradeable(queryClient);

    renderScreen(queryClient);

    const trail = screen.getByRole("navigation", { name: "Change revision" });
    expect(within(trail).getByRole("link", { name: "Settings" }).getAttribute("href")).toBe("/stacks/stack_1/templates/st_1/settings");
    expect(within(trail).getByText("Change revision").getAttribute("aria-current")).toBe("page");
    // The panel's header already names the template.
    expect(screen.queryByRole("heading", { name: "vpc" })).toBeNull();
  });

  it("goes back to Settings on Cancel", async () => {
    const queryClient = testQueryClient();
    seedUpgradeable(queryClient);

    renderScreen(queryClient);
    await userEvent.setup().click(screen.getByRole("link", { name: "Cancel" }));

    expect(screen.getByTestId("location").textContent).toBe("/stacks/stack_1/templates/st_1/settings");
  });

  it("states what the change does to the config before the fields", () => {
    const queryClient = testQueryClient();
    seedUpgradeable(queryClient);

    renderScreen(queryClient);

    const added = screen.getByTestId("upgrade-added-new_var");
    expect(added.textContent).toBe("new_var is new in this revision.");
    expect(screen.getByTestId("upgrade-removed-legacy_flag").textContent).toBe("legacy_flag is no longer used and will be dropped.");
    expect(Boolean(added.compareDocumentPosition(screen.getByLabelText(/region/)) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run from `web/`: `npx vitest run src/features/stacks/UpgradeStackTemplateScreen.test.tsx`
Expected: FAIL: the screen still reads the stack from the URL, its select is "Revision to apply", and it has no crumb or Cancel.

- [ ] **Step 3: Create `web/src/features/stacks/PanelTrail.tsx`**

```tsx
import { Link } from "react-router-dom";

// The crumb row at the top of a view nested in a tab, such as a run under
// Runs or Change revision under Settings: the tab as a link back, then this
// view as plain text, the current page. `name` names the navigation.
export default function PanelTrail({ name, parent, current }: { name: string; parent: { label: string; to: string }; current: string }) {
  return (
    <nav aria-label={name} className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <Link to={parent.to} className="hover:text-foreground hover:underline">
        {parent.label}
      </Link>
      <span aria-hidden="true" className="text-separator">
        /
      </span>
      <span aria-current="page" className="text-foreground">
        {current}
      </span>
    </nav>
  );
}
```

- [ ] **Step 4: Use it in `web/src/features/runs/RunDetailScreen.tsx`**

- Replace `import { Link, useParams } from "react-router-dom";` with `import { useParams } from "react-router-dom";`, and add `import PanelTrail from "../stacks/PanelTrail";` and `import { stackTemplatePath } from "../stacks/templateSelection";`.
- Replace `const trail = <RunTrail stackId={stackId} stackTemplateId={stackTemplateId} runNumber={runNumber} />;` with:

```tsx
  const trail = (
    <PanelTrail name="Run" parent={{ label: "Runs", to: stackTemplatePath(stackId, stackTemplateId, "runs") }} current={`Run #${runNumber}`} />
  );
```

- Delete the `RunTrail` function.

- [ ] **Step 5: Rewrite `web/src/features/stacks/UpgradeStackTemplateScreen.tsx`**

```tsx
import { useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useTemplateRevisionVariablesQuery, useTemplateRevisionsQuery, useUpgradeStackTemplateMutation } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, selectTriggerClass } from "../../shared/fieldClass";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { templateRevisionLabel } from "../templates/templateWorkflow";
import PanelTrail from "./PanelTrail";
import { useStackTemplate } from "./stackTemplateContext";
import {
  configFromVariableValues,
  isDestroyingStackTemplate,
  partitionUpgradeVariables,
  upgradeCandidateRevisions,
  variableValuesFromConfig
} from "./stackWorkflow";
import { stackTemplatePath } from "./templateSelection";
import VariableFields from "./VariableFields";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/upgrade — moving the template
// to another revision of the same source template, under Settings. The
// candidates are filtered to the valid ones and what the change does to the
// config is stated outright. The panel around it names the template and
// handles one the stack does not have.
export default function UpgradeStackTemplateScreen() {
  const { stackId, stackTemplate } = useStackTemplate();
  const navigate = useNavigate();
  const [chosenTargetID, setChosenTargetID] = useState("");
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");

  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const candidates = upgradeCandidateRevisions(templateRevisionsQuery.data ?? [], stackTemplate);
  const targetRevision = candidates.find((revision) => revision.id === chosenTargetID) ?? candidates[0] ?? null;

  const currentVariablesQuery = useTemplateRevisionVariablesQuery(tenantID, stackTemplate.desired_template_revision_id);
  const targetVariablesQuery = useTemplateRevisionVariablesQuery(tenantID, targetRevision?.id ?? "");
  const partition = partitionUpgradeVariables(currentVariablesQuery.data ?? [], targetVariablesQuery.data ?? []);

  // Every query feeding the diff belongs here. A failed variables fetch leaves
  // data undefined, which the partition above reads as "that revision declares
  // no variables": a confident and wrong diff. Only treating it as an error
  // stops that.
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error ?? currentVariablesQuery.error ?? targetVariablesQuery.error);

  // useTemplateRevisionVariablesQuery keeps the previous data while it fetches
  // a new key, so after the target changes, status reads "success" while data
  // still holds the previous target's variables. Only isFetching sees that.
  // Left ungated, the notes and the posted config would come from the stale
  // target while target_template_revision_id already names the new one.
  const targetVariablesRefreshing = targetRevision !== null && targetVariablesQuery.isFetching;

  // The config sent covers the target revision's variables only, so anything
  // the new revision dropped is excluded structurally.
  const targetVariables = [...partition.added, ...partition.carried];
  const baseValues = variableValuesFromConfig(stackTemplate.config, targetVariables);
  const variableValues: Record<string, string> = {};
  for (const variable of targetVariables) {
    variableValues[variable.name] = editedValues[variable.name] ?? baseValues[variable.name] ?? "";
  }

  const upgradeStackTemplateMutation = useUpgradeStackTemplateMutation(tenantID, stackId);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so values typed
  // here are never wiped out by a background sign-in redirect.
  const hasUnsavedValues = Object.keys(editedValues).length > 0;
  const settingsPath = stackTemplatePath(stackId, stackTemplate.id, "settings");
  const trail = <PanelTrail name="Change revision" parent={{ label: "Settings", to: settingsPath }} current="Change revision" />;

  async function handleUpgrade() {
    if (!targetRevision || targetVariablesRefreshing) {
      return;
    }
    setErrorMessage("");
    try {
      await upgradeStackTemplateMutation.mutateAsync({
        stackTemplateID: stackTemplate.id,
        body: {
          target_template_revision_id: targetRevision.id,
          config: configFromVariableValues(targetVariables, variableValues)
        }
      });
      navigate(stackTemplatePath(stackId, stackTemplate.id, "runs"));
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  // The target's variables query is disabled, and so pending forever, when
  // there is no candidate. Wait on it only once something is being fetched.
  // Both sides matter: gating only the target would compare the real current
  // list against an empty target and call everything carried "new"; gating
  // only the current side would call everything "dropped".
  const waitingOnCurrentVariables = currentVariablesQuery.status === "pending";
  const waitingOnTargetVariables = targetRevision !== null && targetVariablesQuery.status === "pending";

  if (templateRevisionsQuery.status === "pending" || waitingOnCurrentVariables || waitingOnTargetVariables) {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="upgrade-loading">
        {trail}
        <p className="flex items-center gap-2 text-meta text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading revisions…
        </p>
      </section>
    );
  }

  if (templateRevisionsQuery.status === "error" || currentVariablesQuery.status === "error" || targetVariablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="flex min-w-0 flex-col items-start gap-5" data-testid="upgrade-load-error">
        {trail}
        <ErrorLine live={false}>Something went wrong while loading revisions.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="upgrade-retry"
          onClick={() => {
            void templateRevisionsQuery.refetch();
            void currentVariablesQuery.refetch();
            void targetVariablesQuery.refetch();
          }}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </section>
    );
  }

  // Settings disables the link while a destroy runs, but the URL still
  // reaches here. The server refuses the change regardless; this says so
  // instead of drawing a form for an action that cannot succeed.
  if (isDestroyingStackTemplate(stackTemplate)) {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="upgrade-destroying">
        {trail}
        <p className="text-meta text-muted-foreground">Destroy in progress. A template being destroyed cannot change revision.</p>
      </section>
    );
  }

  // Select shows the chosen option's label from these.
  const targetItems = candidates.map((candidate) => ({ value: candidate.id, label: templateRevisionLabel(candidate) }));

  return (
    <section className="flex min-w-0 flex-col gap-5" data-testid="upgrade-stack-template-screen" data-unsaved={hasUnsavedValues ? "true" : undefined}>
      {trail}
      {candidates.length === 0 ? (
        <p className="text-meta text-muted-foreground" data-testid="upgrade-no-alternatives">
          No other active revisions available.
        </p>
      ) : (
        <>
          <div className="flex max-w-140 flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="upgrade-target" className={fieldLabelClass}>
                Revision
              </Label>
              <Select
                items={targetItems}
                value={targetRevision?.id ?? null}
                onValueChange={(revisionID) => {
                  // Base UI types the value as nullable; a target is always chosen.
                  if (revisionID === null) return;
                  setChosenTargetID(revisionID);
                  setEditedValues({});
                }}
              >
                <SelectTrigger id="upgrade-target" data-testid="upgrade-target-select" className={selectTriggerClass}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {targetItems.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="font-mono text-meta pointer-coarse:min-h-11">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {targetVariablesRefreshing ? (
              // Inline, not the loading view: a new target is a small change to
              // a drawn view. It replaces only what would be wrong while the
              // previous target's data is still served: the notes and fields.
              <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="upgrade-target-variables-loading">
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables for the selected revision…
              </p>
            ) : (
              <>
                {/* What the change does to the config, stated outright: new
                    variables in muted text, dropped ones in warning, since
                    their values will be lost. */}
                {(partition.added.length > 0 || partition.removed.length > 0) && (
                  <ul className="flex flex-col gap-1 text-meta">
                    {partition.added.map((variable) => (
                      <li key={`added-${variable.name}`} className="text-muted-foreground wrap-anywhere" data-testid={`upgrade-added-${variable.name}`}>
                        <span className="font-mono text-code-foreground">{variable.name}</span> is new in this revision.
                      </li>
                    ))}
                    {partition.removed.map((variable) => (
                      <li key={`removed-${variable.name}`} className="text-warning wrap-anywhere" data-testid={`upgrade-removed-${variable.name}`}>
                        <span className="font-mono">{variable.name}</span> is no longer used and will be dropped.
                      </li>
                    ))}
                  </ul>
                )}
                <VariableFields
                  variables={targetVariables}
                  variableValues={variableValues}
                  onVariableValueChange={(name, value) => setEditedValues((current) => ({ ...current, [name]: value }))}
                  emptyMessage="This revision declares no variables."
                />
              </>
            )}
          </div>
          {errorMessage && <ErrorLine testId="upgrade-stack-template-error">{errorMessage}</ErrorLine>}
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              className={cn(buttonClass("primary"), "pointer-coarse:h-11")}
              disabled={targetVariablesRefreshing || upgradeStackTemplateMutation.isPending}
              onClick={() => void handleUpgrade()}
            >
              {upgradeStackTemplateMutation.isPending ? (
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
              ) : (
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
              )}
              Change revision
            </button>
            <Link to={settingsPath} className={cn(buttonClass("outline"), "pointer-coarse:h-11")}>
              Cancel
            </Link>
          </div>
        </>
      )}
    </section>
  );
}
```

- [ ] **Step 6: Run the tests and see them pass**

Run from `web/`: `npx vitest run src/features/stacks/UpgradeStackTemplateScreen.test.tsx src/features/runs/RunDetailScreen.test.tsx src/features/stacks/StackTemplatePages.test.tsx src/app/router.test.tsx`
Expected: PASS. The router test still finds `template-panel` and an `upgrade-*` test id.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/stacks web/src/features/runs/RunDetailScreen.tsx
git commit -m "feat(web): rebuild Change revision inside the panel, under Settings

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Add template

**Files:**
- Create: `web/src/features/templates/revisionIndicator.ts`
- Test: `web/src/features/templates/revisionIndicator.test.ts`
- Modify: `web/src/api/queries.ts` (`useAddTemplateToStackMutation`)
- Modify: `web/src/features/stacks/AddStackTemplateScreen.tsx`
- Modify: `web/src/features/stacks/AddStackTemplateScreen.test.tsx`
- Delete: `web/src/features/stacks/AddTemplatePanel.tsx`
- Modify: `web/src/app/router.tsx`

**Interfaces:**
- Consumes: `StatusIndicator`, `StatusLabel` (`shared/StatusLabel.tsx`); `fieldLabelClass`, `selectTriggerClass` (Task 2); `stackTemplatePath`; `templateWorkflow` helpers.
- Produces: `export function revisionIndicator(status: TemplateRevisionStatus): StatusIndicator | null`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/templates/revisionIndicator.test.ts`:

```ts
import { LoaderCircle, TriangleAlert } from "lucide-react";
import { describe, expect, it } from "vitest";
import { revisionIndicator } from "./revisionIndicator";

describe("revisionIndicator", () => {
  it("says nothing about an active revision", () => {
    expect(revisionIndicator("active")).toBeNull();
  });

  it.each([
    ["pending_validation", "waiting for validation", LoaderCircle, "settled", false],
    ["validating", "validating", LoaderCircle, "settled", false],
    ["invalid", "failed validation", TriangleAlert, "failed", true]
  ] as const)("draws %s as an icon and a word", (status, label, icon, tone, strong) => {
    expect(revisionIndicator(status)).toEqual({ label, icon, tone, strong });
  });
});
```

In `web/src/features/stacks/AddStackTemplateScreen.test.tsx`:

1. Add imports: `import type { ReactNode } from "react";`, `import TemplatePanel from "./TemplatePanel";`, and `StackTemplate` to the type import from `../../api/types`.
2. Add helpers after `LocationProbe`:

```tsx
function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

function installedTemplate(id: string): StackTemplate {
  return {
    id,
    stack_id: "stack_1",
    component_key: "vpc",
    source_template_id: "tmpl_src_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "",
    source_ref: "main",
    workspace_name: "ws-vpc",
    display_name: "",
    config: { region: "eu-west-1" },
    last_applied_run_id: "",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "never",
    created_by: "user_123",
    lifecycle: "active"
  };
}

// The header names the stack being added to, so the stack is always seeded.
function seedStack(queryClient: QueryClient) {
  queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), {
    stack: {
      id: "stack_1",
      tenant_id: "tenant_123",
      name: "Payments",
      slug: "payments",
      tags: {},
      default_credential_ids: [],
      created_by: "user_123",
      created_at: "2026-07-19T00:00:00Z",
      effectiveCapabilities: { canView: true, canOperate: true, canApprove: true, canManageAccess: true }
    },
    templates: []
  });
}
```

3. Replace `renderScreen` with:

```tsx
function renderScreen(queryClient: QueryClient, routes: ReactNode = <Route path="/stacks/:stackId/templates/:stackTemplateId/*" element={<LocationProbe />} />) {
  seedStack(queryClient);
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <MemoryRouter initialEntries={["/stacks/stack_1/templates/new"]}>
          <Routes>
            <Route path="/stacks/:stackId/templates/new" element={<AddStackTemplateScreen />} />
            <Route path="/stacks/:stackId" element={<LocationProbe />} />
            {routes}
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
```

4. Edit these existing tests:
- `"shows one row per template, named after the template"`: replace the two lines `expect(row.textContent).toContain("main");` and `expect(row.textContent).toContain("44b2e01");` with:

```tsx
    // The root path and the ref under the name; the count at the right. The
    // commit is the Revision select's to show.
    expect(row.textContent).toContain("main");
    expect(row.textContent).toContain("2 revisions");
    expect(row.textContent).not.toContain("44b2e01");
```

- `"says nothing about a template whose latest revision is active"`: change the comment above its last line to `// No state: a StatusLabel marks itself with its tone.`
- `"names the chosen template in the panel that configures it"`: delete the line `expect(screen.getByTestId("add-stack-template-unchosen")).toBeTruthy();` and its comment, replace it with `expect(screen.queryByTestId("add-stack-template-variables")).toBeNull();`, then replace the two lines after the click with:

```tsx
    expect(screen.getByRole("heading", { name: "Configure vpc" })).toBeTruthy();
    expect(screen.getByTestId("add-template-choice-tmpl_src_1").getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByTestId("add-template-choice-tmpl_src_2").getAttribute("aria-pressed")).toBe("false");
```

- `"does not allow choosing a template with no active revision, and says why"`: replace its last two lines with:

```tsx
    // A disabled row says why, as an icon and a word.
    expect(row.textContent).toContain("waiting for validation");
    expect(row.querySelector("[data-tone]")).not.toBeNull();
```

- `"installs the newest active revision when a newer one failed validation"`: replace everything from `expect((row as HTMLButtonElement).disabled).toBe(false);` to the end of the test with:

```tsx
    expect((row as HTMLButtonElement).disabled).toBe(false);
    // The newest revision's failure is not hidden behind an older success.
    expect(row.textContent).toContain("failed validation");
    expect(row.textContent).toContain("1 revision");

    fireEvent.click(row);

    // What gets installed is the active revision, not the newer broken one.
    const shown = screen.getByRole("combobox", { name: "Revision" }).querySelector('[data-slot="select-value"]')?.textContent ?? "";
    expect(shown).toContain("abcdef1");
    expect(shown).not.toContain("44b2e01");
    expect(screen.getByLabelText(/region/)).toBeTruthy();
```

- every `getByRole("button", { name: /Install/ })` → `getByRole("button", { name: "Add template" })`;
- `"installs the chosen revision and opens the installed template's page"`: rename to `"adds the chosen revision and opens the new template's Runs"` and expect `"/stacks/stack_1/templates/st_new/runs"`;
- `"offers no revision picker when the template has only one active revision"`: replace with:

```tsx
  it("shows the one active revision in the picker, so what will be installed is on screen", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [
      templateRevision(),
      // Not active, so not offered.
      templateRevision({ id: "rev_old", status: "invalid", resolved_commit_sha: "44b2e0199999" })
    ]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);

    renderScreen(queryClient);
    fireEvent.click(screen.getByTestId("add-template-choice-tmpl_src_1"));

    await userEvent.setup().click(screen.getByRole("combobox", { name: "Revision" }));
    const options = within(await screen.findByRole("listbox")).getAllByRole("option").map((option) => option.textContent ?? "");
    expect(options).toHaveLength(1);
    expect(options[0]).toContain("abcdef1");
  });
```

5. Add, at the end of the `describe`:

```tsx
  it("replaces the template header with its own: what it is, which stack, and Cancel", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);

    renderScreen(queryClient);

    expect(screen.getByRole("heading", { level: 2, name: "Add template" })).toBeTruthy();
    expect(screen.getByText("to Payments")).toBeTruthy();
    expect(screen.getByRole("heading", { level: 3, name: "Choose a template" })).toBeTruthy();
    await userEvent.setup().click(screen.getByRole("link", { name: "Cancel" }));
    expect(screen.getByTestId("location").textContent).toBe("/stacks/stack_1");
  });

  // The stack refetch after adding is still out when the new panel opens. The
  // panel must find the template anyway, not call it missing.
  it("opens the new template's panel at once, while the stack reloads", async () => {
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [templateRevision()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), [variable()]);
    queryClient.setQueryData(queryKeys.attention("tenant_123"), []);
    // The add answers; every read after it, the stack's refetch among them, hangs.
    vi.spyOn(globalThis, "fetch").mockImplementation((_input, init) =>
      init?.method === "POST" ? Promise.resolve(jsonResponse(installedTemplate("st_new"), 201)) : new Promise<Response>(() => {})
    );

    renderScreen(
      queryClient,
      <Route path="/stacks/:stackId/templates/:stackTemplateId" element={<TemplatePanel />}>
        <Route path="runs" element={<LocationProbe />} />
      </Route>
    );
    fireEvent.click(screen.getByTestId("add-template-choice-tmpl_src_1"));
    fireEvent.change(screen.getByLabelText(/region/), { target: { value: "eu-west-1" } });
    fireEvent.click(screen.getByRole("button", { name: "Add template" }));

    expect(await screen.findByTestId("template-panel")).toBeTruthy();
    expect(screen.queryByTestId("stack-template-missing")).toBeNull();
    expect(screen.getByTestId("location").textContent).toBe("/stacks/stack_1/templates/st_new/runs");
  });
```

- [ ] **Step 2: Run the tests and see them fail**

Run from `web/`: `npx vitest run src/features/templates/revisionIndicator.test.ts src/features/stacks/AddStackTemplateScreen.test.tsx`
Expected: FAIL: no `revisionIndicator`, no header, no "Add template" button, and the new panel says the template is missing.

- [ ] **Step 3: Create `web/src/features/templates/revisionIndicator.ts`**

```ts
import { LoaderCircle, TriangleAlert } from "lucide-react";
import type { TemplateRevisionStatus } from "../../api/types";
import type { StatusIndicator } from "../../shared/StatusLabel";

/**
 * A template revision's state as a StatusLabel draws it, or null for an
 * active revision, which needs saying nowhere. Validation runs on its own,
 * so a revision on its way is grey, like a run still working; only a failed
 * one is coloured.
 */
export function revisionIndicator(status: TemplateRevisionStatus): StatusIndicator | null {
  switch (status) {
    case "pending_validation":
      return { label: "waiting for validation", icon: LoaderCircle, tone: "settled", strong: false };
    case "validating":
      return { label: "validating", icon: LoaderCircle, tone: "settled", strong: false };
    case "invalid":
      return { label: "failed validation", icon: TriangleAlert, tone: "failed", strong: true };
    default:
      return null;
  }
}
```

- [ ] **Step 4: Put the new template in the cached stack, in `web/src/api/queries.ts`**

Add `StackView` to the type import from `./types` (line 5), and replace `useAddTemplateToStackMutation`'s `onSuccess` with:

```ts
    onSuccess: (installed) => {
      // The new template's panel opens as soon as this resolves, before the
      // refetch below lands. Without the template in the cached stack, the
      // panel would say it is not installed until then.
      queryClient.setQueryData<StackView>(queryKeys.stack(tenantID, stackID), (view) =>
        view && !view.templates.some((template) => template.id === installed.id) ? { ...view, templates: [...view.templates, installed] } : view
      );
      queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackID) });
      // The stacks index counts each stack's templates.
      queryClient.invalidateQueries({ queryKey: queryKeys.stacks(tenantID) });
    }
```

- [ ] **Step 5: Rewrite `web/src/features/stacks/AddStackTemplateScreen.tsx`**

```tsx
import { useId, useState } from "react";
import type { ReactNode } from "react";
import { Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  useAddTemplateToStackMutation,
  useStackQuery,
  useTemplateRevisionVariablesQuery,
  useTemplateRevisionsQuery
} from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, selectTriggerClass } from "../../shared/fieldClass";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import StatusLabel from "../../shared/StatusLabel";
import { revisionIndicator } from "../templates/revisionIndicator";
import {
  activeRevisions,
  groupTemplatesByRepository,
  latestActiveRevision,
  revisionCountLabel,
  shortCommitSHA,
  templateRootPathLabel
} from "../templates/templateWorkflow";
import type { SourceTemplateGroup } from "../templates/templateWorkflow";
import { configFromVariableValues } from "./stackWorkflow";
import { stackTemplatePath } from "./templateSelection";
import VariableFields from "./VariableFields";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/new — adding a template to the stack, in the
// stack page's panel, under a header of its own in place of a template's.
// The picker lists the registry's templates by repository, one row each; a
// pick resolves to the template's newest *active* revision, and the Revision
// select offers the rest. Installing almost always wants the newest validated
// commit; moving between commits afterwards is Change revision's job.
export default function AddStackTemplateScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const navigate = useNavigate();
  const [chosenRevisionID, setChosenRevisionID] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");
  const chooseHeadingId = useId();
  const configureHeadingId = useId();

  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? "";
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);
  const templateRevisions = templateRevisionsQuery.data ?? [];
  const chosenRevision = templateRevisions.find((revision) => revision.id === chosenRevisionID) ?? null;

  // Grouped once and shared: the picker draws these groups, and the configure
  // section finds the chosen template among them.
  const repositoryGroups = groupTemplatesByRepository(templateRevisions);
  const chosenTemplate =
    repositoryGroups
      .flatMap((group) => group.sourceTemplates)
      .find((sourceTemplate) => sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID)) ?? null;
  // Newest registered first, in the API's order (see activeRevisions), so the
  // select's first option is the one a row pick already chose.
  const chosenTemplateRevisions = chosenTemplate ? activeRevisions(chosenTemplate.revisions) : [];

  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, chosenRevision?.id ?? "");
  const variables = variablesQuery.data ?? [];
  // The variables query keeps the previous revision's variables while it
  // fetches a new one, so only isFetching covers both a first fetch and a
  // switch.
  const variablesLoading = chosenRevision !== null && variablesQuery.isFetching;
  const variablesFailed = chosenRevision !== null && variablesQuery.status === "error";

  const addTemplateToStackMutation = useAddTemplateToStackMutation(tenantID, stackId);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so values typed
  // here are never wiped out by a background sign-in redirect.
  const hasUnsavedValues = Object.keys(values).length > 0;

  function handleChoose(revisionID: string) {
    setChosenRevisionID(revisionID);
    setValues({});
    setErrorMessage("");
  }

  async function handleInstall() {
    // variablesFailed guards the request itself, not just the button: without
    // the variables, configFromVariableValues would post an empty config as
    // if the template needed nothing.
    if (!chosenRevision || variablesLoading || variablesFailed) {
      return;
    }
    setErrorMessage("");
    try {
      const installed = await addTemplateToStackMutation.mutateAsync({
        template_revision_id: chosenRevision.id,
        config: configFromVariableValues(variables, values)
      });
      navigate(stackTemplatePath(stackId, installed.id));
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  if (templateRevisionsQuery.status === "error" && boundary !== null) {
    return <>{boundary}</>;
  }

  // The select's options: the commit and when it was registered. The name and
  // ref are the template's, already on its row.
  const revisionItems = chosenTemplateRevisions.map((candidate, index) => ({
    value: candidate.id,
    label: `${shortCommitSHA(candidate.resolved_commit_sha)} · ${formatTimestamp(candidate.created_at)}${index === 0 ? " · latest" : ""}`
  }));

  let content: ReactNode;
  if (templateRevisionsQuery.status === "pending") {
    content = (
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="add-stack-template-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading templates…
      </p>
    );
  } else if (templateRevisionsQuery.status === "error") {
    content = (
      <div className="flex flex-col items-start gap-3" data-testid="add-stack-template-load-error">
        <ErrorLine live={false}>Something went wrong while loading templates.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="add-stack-template-retry"
          onClick={() => void templateRevisionsQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  } else if (templateRevisions.length === 0) {
    content = (
      <Empty className="gap-3 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="add-stack-template-none">
        <EmptyHeader className="gap-2">
          <p className="text-sm font-medium">No templates registered yet</p>
          <EmptyDescription className="text-meta">Register a template, then add it to this stack.</EmptyDescription>
        </EmptyHeader>
        <Link to="/templates/new" className={cn(buttonClass("primary"), "pointer-coarse:h-11")} data-testid="register-template-link">
          <Plus data-icon="inline-start" aria-hidden="true" />
          Register template
        </Link>
      </Empty>
    );
  } else {
    content = (
      <>
        <section aria-labelledby={chooseHeadingId} className="flex flex-col gap-3">
          <h3 id={chooseHeadingId} className="text-sm font-semibold">
            Choose a template
          </h3>
          {repositoryGroups.map((group) => (
            <div key={group.key} className="overflow-hidden rounded-lg border" data-testid={`template-group-${group.key}`}>
              <h4 className="border-b border-divider bg-canvas px-4 py-2 font-mono text-xs font-normal text-muted-foreground wrap-anywhere">
                {group.repoOwner}/{group.repoName}
              </h4>
              <ul className="divide-y divide-divider">
                {group.sourceTemplates.map((sourceTemplate) => (
                  <li key={sourceTemplate.sourceTemplateID}>
                    <TemplateChoice
                      sourceTemplate={sourceTemplate}
                      picked={sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID)}
                      onPick={handleChoose}
                    />
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
        {chosenRevision && chosenTemplate && (
          <section aria-labelledby={configureHeadingId} className="flex max-w-140 flex-col gap-4" data-testid="add-stack-template-variables">
            <h3 id={configureHeadingId} className="text-sm font-semibold wrap-anywhere">
              Configure {chosenTemplate.name}
            </h3>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="add-template-revision" className={fieldLabelClass}>
                Revision
              </Label>
              <Select
                items={revisionItems}
                value={chosenRevisionID}
                onValueChange={(revisionID) => {
                  // Base UI types the value as nullable; a revision is always chosen.
                  if (revisionID !== null) handleChoose(revisionID);
                }}
              >
                <SelectTrigger id="add-template-revision" data-testid="add-template-revision-select" className={selectTriggerClass}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {revisionItems.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="font-mono text-meta pointer-coarse:min-h-11">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {variablesLoading ? (
              <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="add-stack-template-variables-loading">
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables…
              </p>
            ) : variablesFailed ? (
              // Not the empty message: a fetch that failed must never read as
              // "this template declares no variables". Inline, so the picker
              // stays usable.
              <div className="flex flex-col items-start gap-3" data-testid="add-stack-template-variables-error">
                <ErrorLine>Could not load this template's variables.</ErrorLine>
                <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => void variablesQuery.refetch()}>
                  <RefreshCw data-icon="inline-start" aria-hidden="true" />
                  Retry
                </button>
              </div>
            ) : (
              <VariableFields
                variables={variables}
                variableValues={values}
                onVariableValueChange={(name, value) => setValues((current) => ({ ...current, [name]: value }))}
                emptyMessage="This template declares no variables."
              />
            )}
            {errorMessage && <ErrorLine testId="add-stack-template-error">{errorMessage}</ErrorLine>}
            <button
              type="button"
              className={cn(buttonClass("primary"), "self-start pointer-coarse:h-11")}
              disabled={variablesLoading || variablesFailed || addTemplateToStackMutation.isPending}
              onClick={() => void handleInstall()}
            >
              {addTemplateToStackMutation.isPending ? (
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
              ) : (
                <Plus data-icon="inline-start" aria-hidden="true" />
              )}
              Add template
            </button>
          </section>
        )}
      </>
    );
  }

  return (
    <section className="flex min-w-0 flex-col" data-testid="add-stack-template-screen" data-unsaved={hasUnsavedValues ? "true" : undefined}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b border-divider px-7 py-6">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="font-heading text-panel-title font-semibold tracking-title">Add template</h2>
          {stackName && <p className="text-meta text-muted-foreground wrap-anywhere">to {stackName}</p>}
        </div>
        <Link to={`/stacks/${stackId}`} className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")}>
          Cancel
        </Link>
      </div>
      <div className="flex min-w-0 flex-col gap-7 px-7 pt-5 pb-7">{content}</div>
    </section>
  );
}

// One template in the picker: its name, where it lives and its ref, and how
// many revisions it has that can be installed. When its newest revision is
// not active, the row says so; with no active revision at all, that is why
// the row cannot be picked.
function TemplateChoice({
  sourceTemplate,
  picked,
  onPick
}: {
  sourceTemplate: SourceTemplateGroup;
  picked: boolean;
  onPick: (revisionID: string) => void;
}) {
  const installable = latestActiveRevision(sourceTemplate.revisions);
  const installableCount = activeRevisions(sourceTemplate.revisions).length;
  const latestState = revisionIndicator(sourceTemplate.latestRevision.status);
  // The ref tells apart two templates at one path on different refs.
  const where = [templateRootPathLabel(sourceTemplate.rootPath, sourceTemplate.name), sourceTemplate.sourceRef].filter(Boolean).join(" · ");

  return (
    <button
      type="button"
      className={cn(
        "flex min-h-13 w-full items-center justify-between gap-4 px-4 py-2 text-left transition-colors focus-visible:-outline-offset-2 disabled:cursor-not-allowed pointer-coarse:min-h-14",
        picked ? "bg-primary-soft" : "enabled:hover:bg-primary-tint"
      )}
      aria-pressed={picked}
      disabled={installable === null}
      onClick={() => installable && onPick(installable.id)}
      data-testid={`add-template-choice-${sourceTemplate.sourceTemplateID}`}
    >
      <span className="flex min-w-0 flex-col gap-0.5">
        <span
          className={cn(
            "text-sm leading-label font-medium wrap-anywhere",
            picked ? "text-primary-strong" : installable ? "text-foreground" : "text-muted-foreground"
          )}
        >
          {sourceTemplate.name}
        </span>
        {where && <span className="font-mono text-xs text-muted-foreground wrap-anywhere">{where}</span>}
      </span>
      <span className="flex shrink-0 flex-wrap items-center justify-end gap-x-3 gap-y-1">
        {latestState && (
          <StatusLabel icon={latestState.icon} tone={latestState.tone} strong={latestState.strong}>
            {latestState.label}
          </StatusLabel>
        )}
        {installableCount > 0 && <span className="text-meta text-muted-foreground">{revisionCountLabel(installableCount)}</span>}
      </span>
    </button>
  );
}
```

- [ ] **Step 6: Fold `AddTemplatePanel` into the route**

In `web/src/app/router.tsx`, replace `import AddTemplatePanel from "../features/stacks/AddTemplatePanel";` with `import AddStackTemplateScreen from "../features/stacks/AddStackTemplateScreen";`, and `{ index: true, element: <AddTemplatePanel /> }` with `{ index: true, element: <AddStackTemplateScreen /> }`. Then:

```bash
git rm web/src/features/stacks/AddTemplatePanel.tsx
```

- [ ] **Step 7: Run the tests and see them pass**

Run from `web/`: `npx vitest run src/features/templates src/features/stacks src/app`
Expected: PASS. The router test still finds `add-stack-template-none`. Then `rg -n "StatusBadge|statusGlyph|toneTextClass" src/features/stacks src/features/runs`. Expected: no hits.

- [ ] **Step 8: Commit**

```bash
git add web/src/api/queries.ts web/src/app/router.tsx web/src/features/templates web/src/features/stacks
git commit -m "feat(web): rebuild Add template in the panel, with its own header

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Verify, look at it, open PR 2

**Files:** `.superpowers/handovers/stack-page-stub-api.mjs` (scratch, git-ignored), a throwaway `web/vite.verify.config.ts` (deleted at the end).

- [ ] **Step 1: Full checks**

Run from `web/`: `npm test && npm run build`
Expected: every test passes; the build succeeds. Then `rg -n "StatusBadge|statusGlyph|toneTextClass|useRunInFlight|AddTemplatePanel" src/features/stacks src/features/runs src/app`. Expected: no hits.

- [ ] **Step 2: Give the stub API credentials and revisions**

The user's docker stack holds ports 8081 and 5173 and builds its UI from `main`, so the browser check runs against the stub API. In `.superpowers/handovers/stack-page-stub-api.mjs`:

1. After `const allRuns = …`, add:

```js
const credential = (id, name, scope, at) => ({ id, name, scope, created_at: at });
const templateCredentials = {
  st_eks: [credential("cred_role", "AWS_ROLE_ARN", "stack_template", "2026-08-12T09:00:00Z")],
  st_rds: [credential("cred_long", "TF_VAR_A_VERY_LONG_CREDENTIAL_NAME_THAT_WILL_NOT_FIT_ON_A_PHONE_ROW", "stack_template", "2026-09-02T09:00:00Z")]
};
const revision = (id, src, owner, repo, root, name, ref, sha, status, at) => ({
  id, tenant_id: "tenant_123", source_template_id: src, repo_owner: owner, repo_name: repo, source_ref: ref,
  resolved_commit_sha: sha, root_path: root, name, description: "", tags: [], status, created_at: at
});
// The installed templates' desired revisions, so Settings can name their
// source, then a registry to add from: redis with 3 revisions, sqs with 1,
// a template at its repository's root, and one still validating.
const revisions = [
  ...Object.values(templates).flat().map((t) =>
    revision(t.desired_template_revision_id, t.source_template_id, "acme", "infra-modules", `aws/${t.component_key.split("-")[0]}`, t.component_key, t.source_ref, "3f9c2a1d00000000", "active", "2026-09-01T09:00:00Z")),
  revision("rev_redis_3", "src_redis", "acme", "infra-modules", "aws/redis", "redis", "main", "f17f983400000000", "active", "2026-09-19T10:00:00Z"),
  revision("rev_redis_2", "src_redis", "acme", "infra-modules", "aws/redis", "redis", "main", "a91c204500000000", "active", "2026-09-02T09:30:00Z"),
  revision("rev_redis_1", "src_redis", "acme", "infra-modules", "aws/redis", "redis", "main", "3c0e112600000000", "active", "2026-08-28T14:15:00Z"),
  revision("rev_sqs_1", "src_sqs", "acme", "infra-modules", "aws/sqs", "sqs-queues", "v0.4.0", "77b2c10000000000", "active", "2026-08-12T09:00:00Z"),
  revision("rev_root_1", "src_root", "acme", "bootstrap", ".", "bootstrap", "main", "8d41f0c000000000", "active", "2026-08-01T09:00:00Z"),
  revision("rev_lambda_1", "src_lambda", "acme", "platform-modules", "aws/lambda", "lambda-edge", "main", "9b3e7d4000000000", "pending_validation", "2026-10-03T08:00:00Z")
];
```

2. In `routes`, replace `[/\/stack-templates\/([^/]+)\/credentials$/, () => []],` with:

```js
  [/\/stack-templates\/([^/]+)\/credentials$/, (m) => templateCredentials[m[1]] ?? []],
  [/\/stacks\/([^/]+)\/credentials$/, () => [credential("cred_env", "AWS_ACCESS_KEY_ID", "stack", "2026-07-19T09:00:00Z")]],
```

and put the new `/stacks/…/credentials` route **before** `[/\/stacks\/([^/]+)$/, …]` (the patterns are tried in order).

3. Replace `[/\/template-revisions$/, () => []],` with `[/\/template-revisions$/, () => revisions],`, and give the variables route four variables with descriptions:

```js
  [/\/template-revisions\/([^/]+)\/variables$/, () => [
    ["cluster_name", "Name of the EKS cluster.", true],
    ["cluster_version", "Kubernetes minor version.", false],
    ["node_instance_type", "Instance type of the general node group.", false],
    ["node_desired_size", "Nodes the general group scales to.", false]
  ].map(([name, description, required]) => ({
    template_revision_id: "r", name, type_expression: "string", description, required, has_default: false, sensitive: false, has_validation: false
  }))]
```

- [ ] **Step 3: Serve the UI against the stub**

```bash
node .superpowers/handovers/stack-page-stub-api.mjs &
cat > web/vite.verify.config.ts <<'EOF'
import base from "./vite.config";
export default { ...base, server: { port: 5174, strictPort: true, proxy: { "/v1": "http://127.0.0.1:8091" } } };
EOF
(cd web && npx vite --config vite.verify.config.ts) &
```

Use `http://localhost:5174` (not 127.0.0.1).

- [ ] **Step 4: Desktop screenshots**

For each path, run headless Chrome in the background and kill it after about 14 seconds (it writes the PNG but never exits, because the app polls):

```bash
CHROME="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
# shot FILE URL [WIDTH,HEIGHT]
shot() { "$CHROME" --headless=new --disable-gpu --hide-scrollbars --virtual-time-budget=8000 --window-size="${3:-1440,1000}" --user-data-dir="$(mktemp -d)" --screenshot="$1" "$2" & sleep 14; kill $! 2>/dev/null; }
S=$(mktemp -d) && echo "screenshots in $S"
APP=http://localhost:5174
shot $S/variables.png $APP/stacks/stack_prod/templates/st_network/variables
shot $S/variables-locked.png $APP/stacks/stack_prod/templates/st_eks/variables
shot $S/credentials.png $APP/stacks/stack_prod/templates/st_eks/credentials
shot $S/credentials-empty.png $APP/stacks/stack_prod/templates/st_network/credentials
shot $S/settings.png $APP/stacks/stack_prod/templates/st_network/settings
shot $S/settings-locked.png $APP/stacks/stack_prod/templates/st_eks/settings
shot $S/upgrade.png $APP/stacks/stack_prod/templates/st_network/upgrade
shot $S/add.png $APP/stacks/stack_prod/templates/new
shot $S/environment.png $APP/stacks/stack_prod/environment
shot $S/empty-stack.png $APP/stacks/stack_sandbox
```

Compare with the boards (`StackPage.dc.html` with `opening` variables, credentials, settings, add; `stack` sandbox). Check: mono labels with descriptions and the lock note above them on eks-cluster; the credentials list with "Added 12 Aug 2026" and the dashed empty state; two Settings sections with "of aws/network" and, on eks-cluster, both reasons; the crumb row and Cancel on Change revision; the Add header, the repository groups with "3 revisions", the disabled lambda-edge row saying "waiting for validation"; Environment's list under its breadcrumb; the sandbox empty state. Picking a row in Add needs a click, so for the picked state open `/stacks/stack_prod/templates/new` in a desktop browser and look by hand.

- [ ] **Step 5: Phone screenshots (375px)**

Headless Chrome's smallest window is 500px, so load the app in a 375px iframe. Write `$S/phone.html`:

```html
<!doctype html><meta charset="utf-8"><body style="margin:0">
<iframe id="f" style="width:375px;height:812px;border:0"></iframe>
<script>document.getElementById("f").src = "http://localhost:5174" + location.hash.slice(1);</script>
```

Then:

```bash
shot $S/phone-credentials.png "file://$S/phone.html#/stacks/stack_prod/templates/st_rds/credentials" 500,900
shot $S/phone-settings.png "file://$S/phone.html#/stacks/stack_prod/templates/st_eks/settings" 500,900
shot $S/phone-add.png "file://$S/phone.html#/stacks/stack_prod/templates/new" 500,900
```

Expected: the long credential name on rds-postgres truncates with the delete button in reach; the Settings buttons wrap under their text; nothing runs past the iframe's 375px right edge.

- [ ] **Step 6: Clean up**

Kill the stub API and Vite, then `rm web/vite.verify.config.ts`. `git status` must show no untracked files under `web/`.

- [ ] **Step 7: Push and open PR 2 (ask the user first)**

Ask before pushing. Then:

```bash
gh auth status
gh pr view 293 --json state --jq .state   # OPEN: base is feat/stack-page; MERGED: rebase onto main and use base main
git push -u origin feat/stack-page-panels
gh pr create --base feat/stack-page --title "feat(web): rebuild the stack page's panel views on openplan UI" --body-file - <<'EOF'
## What changes

The views inside the stack page's template panel are rebuilt on openplan UI. PR 1 (#293) built the page frame; this finishes it.

- **Variables:** mono labels with each variable's description, 36px fields at most 560px wide, and **Save variables** (was "Save config"). While a run is in flight, a destroy runs, or the viewer cannot operate, a note above the fields says why they are locked.
- **Credentials:** the note on what they override, a bordered list (name, when it was added, delete), a dashed empty state, and Name / Value / **Add credential**. The Environment page shares the panel, so it takes the new look too.
- **Settings:** two bordered sections. Revision names the revision and where it comes from, with Change revision. Destroy still only plans the destroy, then opens Runs. The red "Danger zone" card is gone.
- **Behaviour change:** Change revision and Destroy show to every viewer. When one can't be used, it's disabled and its section says why. Change revision used to be hidden from viewers without operator access.
- **Change revision:** a "Settings / Change revision" crumb row, the Revision select, notes on added and dropped variables, then Change revision and Cancel. On success it opens Runs.
- **Add template:** its own header ("Add template", "to <stack>", Cancel), one bordered group per repository, rows with the template's path, ref and revision count, and a "Configure <name>" section with **Add template** (was "Install"). A template with no active revision is disabled and says why, as an icon and a word. The status pills are gone from the stack page.
- Every lock reason now comes from one module and ends with a full stop. Plan and Apply now say why they are disabled while runs load and while a template is destroying or orphaned.
- Adding a template no longer flashes "That template is not installed on this stack." while the stack reloads.

Spec: `docs/superpowers/specs/2026-10-03-stack-page-design.md`
Plan: `docs/superpowers/plans/2026-10-03-stack-page-panels.md`

No configuration or migration impact. Stacked on #293. Retarget to `main` once that merges.

## Validation

- `npm test`
- `npm run build`
- Browser check against a stub API at desktop width and at 375px. I'll attach the screenshots in a comment.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
```

`gh` cannot attach images: tell the user, and offer the screenshot files (SendUserFile) to attach by hand.

- [ ] **Step 8: After #293 and this PR merge: record the new states in openplan UI**

Ask the user before publishing. Then update the "openplan UI" artifact (https://claude.ai/artifact/4hCc6LddTF31cNeaqHwZn8), a Design System type: read its instructions, list its files, read the ones you change, and publish only those. Add the run states from the spec's "Run status" table and the revision states (waiting for validation, validating, failed validation) to the status tables in `project/README.md` and `project/components/StatusLabel/README.md`. In `project/code.md`, list `/stacks/:id` under Pages, and map TemplatePanel, RunStatusLabel, TemplateTabs, SettingsSection and `shared/fieldClass.ts`.

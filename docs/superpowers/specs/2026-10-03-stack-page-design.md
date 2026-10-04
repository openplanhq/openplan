# Stack page — design

Status: approved for planning, 2026-10-03.

## Problem

A stack's templates are spread across pages. `/stacks/:id` is a placeholder
("Stack overview"), the templates are a list one tab away, and each template is
its own page with its own tab row, which replaces the stack's tabs. To approve a
plan on one template and check another, a person goes up and down that tree.
None of these pages are built to openplan UI: they sit on the white page, draw
status with `StatusBadge`'s dots, and use the vendored shadcn look.

## Design source

- Canvas: https://claude.ai/artifact/74qBEVqi1VAWDrEtFcnbhh. The boards are
  `StackPage.dc.html` (prod, clickable) and its wrappers: Run, Variables,
  Settings and Add for prod; staging, dev (a template never applied),
  Analytics platform, Edge CDN (failed destroy) and sandbox (empty).
- Design system: "openplan UI", https://claude.ai/artifact/4hCc6LddTF31cNeaqHwZn8.
  Its `code.md` maps every token to `web/src/styles/theme.css` and every
  component to the file that implements it. Nothing on this page uses a colour,
  size or radius outside those tokens, and no state is shown as a coloured dot.

## The page

`/stacks/:id` is the stack's one page, on the canvas:

- **Header.** `PageHeader` with the trail Stacks / the stack's name, the name as
  the h1 (no count), and under it the slug (`code-small`) and the tags (Tag).
  At the right, Environment (`key-round`) and Access (`users`) as outline
  buttons at `control-lg`, shown only with `canManageAccess`.
- **Split view.** One `card` panel with a `border` and `rounded-panel` that fills
  the page below the header. The template list on the left, `w-90` wide, and the
  panel for the selected template on the right.

### The template list

- A SearchField, "Filter templates", matching the template's label. The
  SearchField classes move out of `StacksListScreen` into `web/src/shared` so
  both pages share them.
- One ListItem per template, in the API's order: the label in `code` at weight
  500 (`text-primary-strong` when selected); under it, in `caption`
  `muted-foreground`, the ref (mono) · what last happened
  (`stackTemplateActivity`: "Applied 22 Sept 2026", "Planned 3 Oct, 15:00",
  "Failed 29 Sept, 08:00", "Never applied").
- Chips at the right, only when something needs a person: "plan to approve"
  (`hourglass`, warning) when `pending_plan_run_id` is set, and "destroy
  failed" (`triangle-alert`, destructive) when `lifecycle` is `failed`. A
  template has at most one pending plan, so the waiting chip has no count.
- The failed time comes from the attention query's item for that template,
  as on `/stacks`. Everything else comes from the stack query.
- After the templates, an "Add template" row (`plus`, `primary` text), a link
  to `templates/new`, shown only with `canOperate`. While adding, it is the
  selected row and reads "New template".
- "No templates in this stack yet." with no templates; "No templates match
  this filter." when the filter hides them all.
- Rows are links with aria-current="true" on the selected one. They push a
  history entry, unlike `/stacks`, which replaces: a selection here is a
  working page, and on a phone it is its own screen, so Back returns to the
  list.

### Selection

- **Default.** With no template in the URL, the panel shows the first template
  with a pending plan, else the first whose destroy failed, else the first in
  the list. The URL does not change.
- **Switching keeps the tab.** A row links to the same tab of its template:
  from eks-cluster's Variables, network opens on Variables. From a run it
  opens Runs; from Change revision, Settings; from Add template, Runs.
- **Filtered away.** The panel keeps showing the selected template even when
  the filter hides its row; the filter narrows the list, not the selection.

### Phone

Below `md` the page shows the list or the panel, never both. The index shows
the list and hides the panel; a template route or `templates/new` shows the
panel and hides the list, and the panel starts with a "Templates" back link
(`arrow-left`) to `/stacks/:id`. The header shows on both. This is CSS only:
each side carries `hidden md:flex` on the routes where it gives way.

## Routes

```
stacks/:stackId                        RequireCapability canView
├─ StackPage (handle: canvas)          header, list, and the panel as its <Outlet/>
│  ├─ index                            the default template's panel, on Runs
│  ├─ templates                        <Navigate to=".." replace />
│  ├─ templates/new         canOperate AddTemplatePanel
│  └─ templates/:stackTemplateId       TemplatePanel
│     ├─ index                         <Navigate to="runs" replace />
│     ├─ runs                          RunsTab
│     ├─ runs/:runNumber               RunView
│     ├─ variables                     VariablesTab
│     ├─ credentials   canManageAccess CredentialsTab
│     ├─ settings                      SettingsTab
│     └─ upgrade            canOperate ChangeRevision
└─ StackSectionLayout                  environment, access
   ├─ environment  canManageAccess     EnvironmentScreen, unchanged
   └─ access       canManageAccess     StackAccessScreen, unchanged
```

- Every URL in use today resolves to the same template, tab and run, so the
  links from `/stacks`, the attention page, `attentionRunPath` and run rows
  keep working unchanged.
- The index renders `TemplatePanel` for the default template with the Runs tab
  as its content, rather than redirecting, so a phone can show the list there.
- `StackSectionLayout` gives Environment and Access the shared Breadcrumb,
  Stacks / name / Environment (or Access), on the white page, and no tab row.
  Restyling them is a later change.
- Deleted: `StackDetailShell` (its tab row and the Overview placeholder),
  `StackTemplateDetailShell`, `StackTemplateListScreen`, `TemplateRunsTab`'s
  outlet context, and `RouteTabs` once nothing imports it.

## The template panel

**Header** (`px-7 pt-6`, a `border-divider` line under the tabs): the label as
an h2 in `panel-title`, then a row of the template's StatusLabel · the ref
(`code-small`) · what last happened. The status is `stackTemplateIndicator`,
with "waiting for approval" now read from `pending_plan_run_id` instead of the
attention item, so it moves with the stack query.

**Tabs:** Runs, Variables, Credentials (only with `canManageAccess`), Settings.
Links 40px tall (44px on a coarse pointer), `gap-6` apart, `label` style in
`muted-foreground`, the current one in `foreground` with a 2px `primary` line
under it and aria-current="page", inside a `nav` named "Template sections".
Runs stays current on a run; Settings stays current on Change revision.

**Content:** `px-7 pt-5 pb-7`, `gap-5` between parts. The panel is keyed on the
template, so unsaved edits never carry over to another template.

**Refreshing the stack.** The effect that invalidates the stack query when the
latest run settles or starts waiting moves from `TemplateRunActions` to
`TemplatePanel`, so it runs on every tab and the list's chips stay current.

**Missing template.** An id the stack does not have shows "That template is
not installed on this stack." in the panel, with the list still beside it.

### Runs

- **Toolbar.** At the left a `meta` note that explains the buttons:
  - nothing in flight: "Plan shows what would change. Apply saves a plan that
    waits for approval."
  - a run in flight: `runInFlightReason(run, "starting another run")`, so
    "Apply or discard run #14 before starting another run." or "Wait for run
    #14 before starting another run."
  - a failed destroy: "A template whose destroy failed cannot start runs."
  - without `canOperate`: "Starting a run requires operator access".

  At the right: Auto apply (only with `canApprove`), Plan (outline,
  `file-search`) and Apply (primary, `play`), at `control-md`. Disabled while a
  run is in flight, while the template is not active, or without `canOperate`.
- **Waiting card**, while a run waits for approval: a bordered row with the
  `hourglass` and "Plan waiting for approval" ("Destroy plan waiting for
  approval" for a destroy), under it "Run #14 · Planned by Priya Shah · 3 Oct,
  15:00", then its PlanDiff, then **Review plan**, a link styled as the primary
  button, to the run.
- **Failed card**, when the destroy failed: `triangle-alert` and "Destroy
  failed", "The destroy run stopped before it finished. Some resources may
  still exist.", and **View run**, an outline link to the latest failed
  destroy run. Without one on record, the card has no link.
- **Runs table.** Columns Run (64px), Status (190px), Changes (110px), Started
  by (the rest), Time (110px), with a header row. The run number is a mono link
  to the run; Status is the run's StatusLabel; Changes is a PlanDiff, empty
  without a plan; Time is `formatDateTime(created_at)`. A shadcn Table with a
  `<colgroup>`, inside a bordered `rounded-lg` frame that scrolls sideways on a
  phone.
- **Approve and Discard leave the table.** They live on the run, behind Review
  plan. This is a behaviour change: approving takes one more click, and nobody
  approves a plan they have not opened.
- **No runs:** a dashed EmptyState, "No runs yet" / "Plan to see what this
  template would create."

### A run (`runs/:runNumber`)

- A crumb row, "Runs / Run #14": Runs links to the Runs tab, the last crumb is
  plain text with aria-current="page".
- "Run #14" as an h3 (18px, 600, `tracking-title`) with the run's StatusLabel.
  At the right, while it waits, Discard (outline) and Approve (primary), or the
  destroy confirmation for a destroy, with today's capability checks and
  dialog (`WaitingRunActions`).
- A bordered `dl` in three columns (one on a phone): Started, Finished ("Not
  finished" until it ends), Changes (PlanDiff, left out without a plan),
  Started by, Source (`v1.4.0 @ 3f9c2a1`).
- An error is one `meta` line in `destructive` with a `triangle-alert`, not an
  Alert.
- **Logs:** a bordered section headed "Logs" (`square-terminal`), one row per
  phase that opens onto its log, a chevron at the left. The log body is mono
  12px on `canvas` with a `border-divider` line above, in place of today's
  inverted block. Which rows open, and what they fetch, does not change.
- An unknown number shows "This template has no run #N.", as today.

### Variables

- While a run is in flight, a `meta` note above the fields:
  `runInFlightReason(run, "changing the config")`. While destroying: "Destroy
  in progress".
- The fields, at most `max-w-140` (560px), `gap-4`: the variable's name as a
  mono label (13px, 500, with " *" when required), a 36px mono input, and its
  description in `caption` `muted-foreground`.
- **Save variables** (primary, `control-md`; renamed from "Save config"),
  disabled while locked or unchanged.
- Without `canOperate`: the inputs are disabled and the note reads "Editing
  requires operator access".

### Credentials

- The note "Overrides the stack environment for this template only."
- A bordered list: the name (mono), "Added 12 Aug 2026" in `meta`, and a trash
  icon button with aria-label "Delete NAME".
- With none, a dashed EmptyState: "No credentials for this template" / "Its
  runs use the stack environment."
- Name and Value (password) fields and **Add credential** (outline,
  `control-lg`), wrapping on a phone.
- `CredentialsPanel` is shared with the Environment page, which takes the new
  look too. Its copy comes from props.

### Settings

Two bordered sections (`rounded-lg`, `px-5 py-4`), the text at the left and the
button at the right, wrapping on a phone. The red "Danger zone" card goes.

- **Revision.** "This template runs revision `v1.4.0` of `aws/eks`." with
  Change revision (outline), a link to `upgrade`. The source is the desired
  revision's root path, from the template revisions query.
- **Destroy.** "Plans the removal of everything this template manages. Nothing
  is removed until that plan is approved." with Destroy as an outline button in
  `destructive` text. It still only plans the destroy, then opens Runs.
- A locked button's section adds why: `runInFlightReason(run, "changing the
  revision")` or `runInFlightReason(run, "destroying")`, "Destroy in
  progress", or, without `canOperate`, "Changing the revision requires
  operator access" and "Destroying requires operator access". Both buttons
  show to every viewer, disabled when they cannot be used; a disabled button
  never stands without a reason.

### Change revision (`upgrade`)

There is no board; it is built from the same parts. A crumb row "Settings /
Change revision". A Revision select (36px), the notes on added and removed
variables, the fields at most `max-w-140`, then **Change revision** (primary)
and Cancel (outline, back to Settings). The behaviour and its guards stay as in
`UpgradeStackTemplateScreen`; on success it opens the template's Runs tab.

### Add template (`templates/new`)

It replaces the template header:

- "Add template" in `panel-title`, "to prod" under it in `meta`, and Cancel
  (outline, `control-lg`) at the right, which goes back to `/stacks/:id`.
- **Choose a template.** One bordered group per repository: a mono `caption`
  header on `canvas` (`acme/infra-modules`), then a row per template, at least
  52px tall: the name in `label`, the root path in mono `caption`, and "3
  revisions" in `meta` at the right. The picked row takes the selected style
  and aria-pressed="true". A template with no active revision is disabled and
  its row says why.
- **Configure redis**, at most `max-w-140`: the Revision select (newest active
  first), the variable fields, and **Add template** (primary, `plus`). Adding
  opens the new template's panel.
- With no template registered: an EmptyState with a link to `/templates/new`.
- The behaviour, guards and `data-unsaved` marker stay as in
  `AddStackTemplateScreen`.

### Empty stack

The list says "No templates in this stack yet." and the panel shows a dashed
EmptyState: "No templates in this stack yet" / "Add a template to plan and
apply its infrastructure here." with **Add template** (primary, `plus`), only
with `canOperate`.

## Run status

`runStatusLabel` becomes `runIndicator(run)`, returning `{ label, icon, tone,
strong }` for StatusLabel. The headline logic stays; the words are lowercase,
and a waiting plan reads "waiting for approval", not "Planned".

| Run | Words | Icon | Tone |
| --- | --- | --- | --- |
| waiting | waiting for approval, destroy waiting for approval | `hourglass` | attention, 500 |
| finished | applied, destroyed, plan finished | `check`, `check`, `file-text` | settled |
| no changes | no changes, nothing to destroy | `equal` | settled |
| failed | plan failed, apply failed, destroy plan failed, destroy failed | `triangle-alert` | failed, 500 |
| discarded | discarded, destroy discarded, canceled | `ban` | idle |
| working | planning, planning destroy, applying, destroying, approved, destroy approved | `loader-circle` (still) | settled |

- A running run adds its step ("applying · fetching source"); a failed one the
  step it failed on ("apply failed while initializing"), as today. A queued
  run reads as the headline it will have ("planning · waiting for an
  executor"), as today.
- An unknown step from a newer backend reads as no step, as today.
- `StatusBadge` and `statusGlyph` stay for the registry screens, which are not
  part of this change.

## Delivery

One branch, two PRs.

1. **Stack page frame.** Routes, StackPage (header, list, selection, phone),
   TemplatePanel (header, tabs, refresh effect), the Runs tab, the run, and
   `runIndicator`; `StackSectionLayout`; the deleted shells. Variables,
   Credentials and Settings render their current content inside the new panel
   until PR 2.
2. **Panel views.** Variables, Credentials (and with it Environment), Settings,
   Change revision and Add template.

After PR 1 lands, the "openplan UI" artifact gains the run rows in its README
and StatusLabel status tables, and `code.md` lists `/stacks/:id` under Pages
and maps TemplatePanel, the run StatusLabel and the underline tabs.

## Testing

- **Units.** `runIndicator` for every status, operation and step; the default
  selection (waiting, then failed, then first, and none); the link target that
  keeps the tab when switching templates.
- **StackPage.** Header actions gated on `canManageAccess`; chips; the filter
  and its empty line; aria-current on the default and on a picked row; the Add
  template row gated on `canOperate`; the empty stack.
- **TemplatePanel.** Tab aria-current; the run and Change revision crumbs; deep
  links to a tab and to `runs/:n`; an unknown template and an unknown run.
- **Moved tests.** `StackTemplatePages.test`, `StackDetailShell.test`,
  `TemplateRunHistory`, `RunDetailScreen`, `TemplateDestroyPanel`, Add and
  Upgrade follow the new markup and keep their behaviour assertions, except
  where this spec changes the behaviour: Approve and Discard leave the runs
  table, and "Save config" is "Save variables".
- **Router.** The new tree, and `templates` redirecting to the stack.
- **Browser.** `scripts/drive-web.mjs` at desktop width and at 375px against
  the prod, run, Edge CDN and sandbox boards.
- `npm test` and `npm run build` pass.

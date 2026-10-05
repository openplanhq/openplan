# Templates page — design

Status: approved for planning, 2026-10-05.

## Problem

Templates is one of the two sections in the top bar, and the only one still on
the old look. `/templates` lists templates in shadcn Cards under a breadcrumb
used as a title bar; `/templates/:id` is only a table of commits; `/templates/new`
is a four-field Card form whose progress is a "Registration: pending" row.
Status is drawn with `StatusBadge`'s glyphs (● ◐ ○ ✕ ⊘), the last place in the
app that does, and failures are red Alert boxes.

What a template is for is missing too. Its description, tags and variables are
on every revision, but these pages show none of them: the only place a person
sees a template's variables is the Add template picker inside a stack, after
they have decided to add it.

The section serves two people. Most people browse: what is available, what it
does, what it needs. Publishers (`canPublishTemplate`) also register templates
and sync them, and need to see a template's commits.

## Design source

- Canvas: https://claude.ai/artifact/74qBEVqi1VAWDrEtFcnbhh, direction K (the
  two bottom rows): `TemplatesPage.dc.html` (clickable) and its state boards —
  Revisions, Sync found a new commit, a template without `template.yaml`, the
  viewer, Register template, registering, a registration that failed, and an
  empty registry. Directions A, B and C, above K, are the explorations it was
  picked from: K is A's page with B's details.
- Design system: "openplan UI", https://claude.ai/artifact/4hCc6LddTF31cNeaqHwZn8.
  Its `code.md` maps every token and component to `web/`. Nothing here uses a
  value outside its tokens, and no state is a coloured dot.

## What a template is, as the page uses it

A template is the set of revisions sharing one identity: repository
(`repo_owner/repo_name`), root path and ref. `groupTemplatesByRepository` and
`revisionsForSourceTemplate` already build that from the tenant's revisions
list, newest first, and `sourceTemplateKey` gives its id, which is the
`:sourceTemplateId` in the URL. Everything shown is read from the **latest
revision**: its name (`templateDisplayName`), description, tags and variables.

Facts that shape the design:

- Every repository is cloned from github.com (`gitHubRepoURL`), so links to the
  repository, the module's folder, the ref and a commit are built in the app.
- A revision is created `active`. A module that cannot be registered never
  becomes a revision: its registration ends `invalid` or `failed` with an
  `error_summary`. So the registry only ever lists working templates, and the
  failures to design for are a registration and a Sync.
- A variable carries `type_expression`, `required` and `has_default`, but not
  its default value.
- There is no list of registrations and no record of which stacks use a
  template. Neither is shown; adding them is out of scope.

## The page

`/templates` is one page on the canvas ground:

- **Header.** `PageHeader` with "Templates" and a CountPill of templates. At the
  right, **Register template** (primary, `plus`, `control-lg`), only with
  `canPublishTemplate`.
- **Split view.** One `card` panel with a `border` and `rounded-panel` below the
  header: the template list `w-90` wide on the left, the selected template's
  panel on the right. The same frame as `StackPage`.

### The template list

- A SearchField, "Filter templates", matching the name, repository, root path,
  description and tags.
- The templates under their repository: a `code-small` `muted-foreground` label
  per repository (`acme/infra-modules`), then a ListItem per template: the name
  in `label`, and under it in mono `caption` `muted-foreground` the root path
  and ref (`aws/eks · main`; the ref alone for a module at the root). No chips:
  nothing in a template waits on a person.
- "No templates yet." with none; "No templates match this filter." when the
  filter hides them all.
- Rows are links to `/templates/:sourceTemplateId/<tab>` with
  aria-current="true" on the selected one, keeping the current tab (Variables
  from the index or from Register template). They push a history entry, as on
  the stack page.

### Selection

- With no template in the URL, the panel shows the first template in the list;
  the URL does not change.
- The filter narrows the list, never the selection.

### Phone

As on the stack page: below `md` the page shows the list or the panel, never
both. `/templates` shows the list; a template's address or `/templates/new`
shows the panel, opening with a "Templates" back link (`arrow-left`). The
header shows on both. CSS only.

## Routes

```
templates                                   TemplatesPage (handle: canvas): header, list, <Outlet/>
├─ index                                    the default template's panel, on Variables
├─ new                  canPublishTemplate  RegisterTemplatePanel
└─ :sourceTemplateId                        TemplatePanel
   ├─ index                                 <Navigate to="variables" replace />
   ├─ variables                             VariablesTab
   └─ revisions                             RevisionsTab
```

- `/templates/:id` keeps working: it opens the template on Variables.
- `/templates/new` gains the `canPublishTemplate` route guard it lacks today.
- `?selected=<revisionID>` goes: registering now opens the new template's own
  address.

## The template panel

**Header** (`px-7 pt-6`, a `border-divider` line under the tabs, `gap-5`):

- The name as an h2 in `panel-title`, breaking anywhere. Under it, `gap-3`: the
  description in `body` `foreground`, at most 680px wide, and the tags as Tags.
- No description: with `canPublishTemplate`, a `meta` note, "No description.
  Add a `template.yaml` beside the module with a name, a description and tags."
  Without it, nothing.
- At the right, with `canPublishTemplate`: **Sync** (outline, `refresh-cw`,
  `control-lg`) and its result line under it (see Sync).
- **Details**: a DetailList in four columns (two below `lg`), each value a link
  to GitHub opening in a new tab:

  | Term | Value | Link |
  | --- | --- | --- |
  | Repository | `acme/infra-modules` | `https://github.com/{owner}/{repo}` |
  | Root path | `aws/eks`, or "the repository root" | `…/tree/{ref}/{root_path}` |
  | Ref | `main` | `…/tree/{ref}` |
  | Latest commit | `3f9c2a1` · 22 Sept 2026 | `…/commit/{resolved_commit_sha}` |

  The date is when that commit was registered. A link is the value in `code`
  and `foreground` with an `arrow-up-right` at `icon-xs` in
  `subtle-foreground`; it turns `primary` and underlines on hover, and carries
  a visually hidden ", on GitHub (opens in a new tab)". Each path segment is
  URI-encoded; the slashes between them are kept.
- **Tabs**: Variables and Revisions, each with its count after it in
  `caption-strong` `subtle-foreground` ("Variables 5"), in a `nav` named
  "Template sections". The TemplateTabs look: 40px, `gap-6`, the current one in
  `foreground` with a 2px `primary` line. Both tabs show to everyone.

**Content**: `px-7 pt-5 pb-7`, `gap-3`. Keyed on the template.

**Missing template.** An id that matches no template shows "That template is
not registered." in the panel, with the list beside it.

### Variables

- A `meta` note: "From the latest revision, `3f9c2a1`. A required variable
  needs a value when the template is added to a stack."
- A table in a bordered `rounded-lg` frame that scrolls sideways on a phone:
  Name (200px, `code` `foreground`), Type (120px, mono `caption`
  `muted-foreground`, the `type_expression`), Value (90px: "required" in `meta`
  at 500 `foreground`, else "optional" in `meta` `muted-foreground`),
  Description (the rest, `meta` `muted-foreground`). A header row in
  `caption-strong`. Rows at least 48px.
- None: "This template takes no variables." in `meta`, no table.
- Loading and failure as on the stack page's Variables tab: a loading line, or
  an ErrorLine with Retry.

### Revisions

- A `meta` note: "Every commit registered from `main`, newest first. A stack
  keeps the revision it was added with until someone changes it."
- A table: Commit (200px: the short SHA as a GitHub link, and a `latest` Tag on
  the first row) and Registered (`formatTimestamp(created_at)`). A revision that
  is not active adds its `revisionIndicator` StatusLabel after the commit; none
  is today, but the API allows it.

## Sync

Sync keeps today's flow from `TemplateDetailScreen`: it re-registers the
template's identity, polls the registration, and refreshes the revisions list
when it completes. It moves into the panel header, and its result is said in
words under the button, in a `role="status"` line that is mounted empty:

- While it runs: the button is disabled and its icon is a turning loader.
- A new commit: "Registered commit `9e7d3b2`." The commit becomes the latest,
  in Details and at the top of Revisions. Today a new commit arrives silently,
  which on the Variables tab looks like nothing happened.
- Nothing new: "Already up to date."
- A failure: an ErrorLine with the registration's `error_summary`, or "Sync
  failed" when it has none.

## Register template (`/templates/new`)

It replaces the panel's header, as Add template does on the stack page:

- "Register template" in `panel-title`, "from a Git repository" under it in
  `meta`, and Cancel (outline, `control-lg`) at the right, back to `/templates`.
- A `meta` note: "openplan clones the repository, reads the module's variables
  and its `template.yaml`, and registers the commit the ref points at."
- Fields, at most `max-w-140`, as Field: Owner and Repository side by side
  (wrapping on a phone), then Ref and Root path. Hints: "The GitHub organization
  or user.", "The repository name.", "A branch or a tag. On a branch, Sync picks
  up new commits.", "The module's directory in the repository, or . when the
  module is at its root." Defaults as today: `hashicorp`, empty, `main`, `.`.
- **Register template** (primary, `plus`, `control-md`).
- **Registering**: the fields and the button are disabled, and under them a
  bordered status row: a turning loader, "Registering `acme/infra-modules ·
  aws/sqs at main`", and "Cloning the repository and reading the module. The
  template opens here when it is registered."
- **Registered**: once the refreshed revisions list holds the new revision, go
  to `/templates/<its sourceTemplateKey>` (replace). An identity that was
  already registered opens that template.
- **Failed** (`invalid` or `failed`): an ErrorLine, "This template could not be
  registered. Check the fields and register it again.", then the
  `error_summary` as written, in a mono block on `canvas` (`root path
  "aws/sqs-queues": directory does not exist`). The fields keep what was typed.
  A refused request shows its message the same way.
- The `data-unsaved` marker stays as in `TemplateRegistrationScreen`.

## Empty registry

The list says "No templates yet." and the panel shows a dashed EmptyState, "No
templates yet", with "Register a Terraform module from a Git repository to make
it available to your stacks." and **Register template**, with
`canPublishTemplate`. Without it: "Templates appear here once someone who can
publish registers one." and no button.

## Loading and errors

- Loading the revisions list: "Loading templates…" in the page, as now.
- A 401, 403, 404 or 503 goes to `useQueryErrorBoundary`, as now.
- Any other failure: the page header, then an ErrorLine "Something went wrong
  while loading templates." and Retry (outline, `refresh-cw`), in place of the
  red Alert.

## What goes

- `TemplateRegistryScreen`, `TemplateDetailScreen` and
  `TemplateRegistrationScreen`, replaced by `TemplatesPage`, `TemplatePanel`,
  `VariablesTab`, `RevisionsTab` and `RegisterTemplatePanel` under
  `web/src/features/templates/`. Their behaviour tests move with them.
- `unsettledStatusTone` in `templateWorkflow.ts`, once nothing calls it.
- `StatusBadge`, `StatusRow` and `statusGlyph` lose their last screens; the
  style guide still shows them. Deleting them, and the style guide's
  specimens, is a follow-up, as are `shared/Breadcrumb` (still used by the
  stacks error state, Create stack and the stack section layout) and the `Alert`
  component.

## Shared pieces

- The list and panel frame repeat `StackPage`'s. Lift the frame, the phone
  rules and the back link into one shared component if it stays small;
  otherwise mirror the classes and say so in both files.
- The tabs reuse `TemplateTabs`' look; generalise it to take its tabs as data.
- External links get one helper, `githubLinks(revision)`, returning the four
  URLs, and one `ExternalLink` component for the look and the hidden text.

## After this lands

The "openplan UI" artifact gains: `arrow-up-right` in Icons; an external link in
Button (or its own page): `code`, `arrow-up-right`, new tab, the hidden text; a
Templates page pattern in the README; `/templates` under Pages in `code.md` and
the new files in its component table; and "Not yet" loses the registry.

## Testing

- **Units.** `githubLinks` for a nested path, a root module (`.` and empty), and
  a ref with a slash; the filter's fields; the default selection.
- **TemplatesPage.** Register template gated on `canPublishTemplate`; grouping
  by repository; aria-current on the default and on a picked row; switching
  keeps the tab; the filter and its empty line; the empty registry with and
  without the capability.
- **TemplatePanel.** Name, description, tags; the no-description note only for
  publishers; the four links and their hrefs; tab aria-current and counts;
  Variables' required and optional cells and the no-variables line; Revisions'
  order, `latest` and commit links; an unknown id.
- **Sync.** Hidden without the capability; disabled while running; "Already up
  to date."; "Registered commit …" with the new commit on top; the error line.
- **Register.** The route guard; the defaults; disabled while registering, with
  the status row; opening the new template on completion; an already
  registered identity; `invalid` and `failed` with their summary and the fields
  kept; `data-unsaved`.
- **Router.** The new tree, and `/templates/:id` opening Variables.
- **Browser.** `scripts/drive-web.mjs` at desktop width and at 375px against
  the K boards.
- `npm test` and `npm run build` pass.

## Delivery

One branch, `feat/templates-page`, one PR: the page, the panel and its tabs,
Sync, Register template, and the deletions above. Then the design system sync.

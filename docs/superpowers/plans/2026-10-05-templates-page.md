# Templates Page Implementation Plan

**Goal:** Rebuild `/templates`, `/templates/new` and `/templates/:id` as one openplan UI split-view page: templates grouped by repository on the left, the selected template's panel (description, tags, Sync, GitHub details, Variables and Revisions tabs) on the right, and Register template inside the panel.

**Architecture:** `TemplatesPage` is a route layout like `StackPage`: it loads the tenant's revisions once, draws the header and the list, and renders the selected template's route into its `<Outlet/>`. `TemplatePanel` resolves the template from the URL and hands it to its tabs through a React context, as the stack page's `TemplatePanel` does. Everything is derived from the existing revisions, variables and registration endpoints; nothing on the server changes.

**Tech Stack:** React 19, react-router-dom 6 (data router), TanStack Query 5, Tailwind v4 on `web/src/styles/theme.css`, shadcn/ui (base-nova on Base UI), lucide-react 0.468, Vitest 4 + Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-05-templates-page-design.md`, which is where behaviour, copy and layout are specified. Design boards: direction K on https://claude.ai/artifact/74qBEVqi1VAWDrEtFcnbhh (`TemplatesPage.dc.html` and its state boards).

> **This plan is condensed.** It records what each task built: the files it touched, the interfaces it produced, the tests it asserted and the commit it made. It does not carry the verbatim file bodies, so it is not executable on its own; work from the spec and the repository. The full step-by-step version, with every listing, is in this file's history.

## Global Constraints

- All commands run from `web/`: `npx vitest run <file>` for one file, `npm test` for the suite, `npm run build` for the type-check and build.
- Classes use theme tokens only. `src/styles/tailwind.guard.test.ts` fails on arbitrary values (`w-[37px]`, `max-w-[680px]`) and Tailwind palette colours; use spacing-scale steps (`max-w-170` is 680px, `max-w-140` is 560px, `w-90` is 360px).
- `src/styles/tables.guard.test.ts`: every table is shadcn's `Table` and opens with a `<colgroup>`.
- No colour literals in `.tsx` files (the raw-colours guard).
- No status is drawn as a coloured dot or glyph; states use `StatusLabel`.
- Copy is sentence case, no exclamation marks, no relative times. Dates come from `formatTimestamp` ("22 Sept 2026").
- Every link that leaves openplan opens in a new tab with `rel="noreferrer"` and says so to a screen reader.
- Register template and Sync appear only with the tenant capability `canPublishTemplate`; `/templates/new` is guarded by it.
- Touch targets grow to 44px on a coarse pointer (`pointer-coarse:h-11`), as everywhere else.
- Commits: short imperative subjects with a `feat(web):`, `test(web):`, `refactor(web):` or `fix(web):` prefix, ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **A template whose `source_template_id` is empty** has a JSON-tuple id (`sourceTemplateKey`). Its row link must be URI-encoded and must select the same template when followed. Pinned in Task 4.
2. **Switching templates while a Sync runs** must not carry the spinner, result or error to the next template. Pinned in Task 8.
3. **A registration poll that keeps failing** (403, network) must stop the spinner, re-enable the form and say why, not leave it busy forever. Pinned in Task 9.
4. **Registering an identity that is already registered** completes with an existing revision and must open that template, not wait for a new one. Pinned in Task 9.
5. **A filter typed with capitals or surrounding spaces** must match the same templates as its trimmed lower-case form. Pinned in Task 2.

## File Structure

Create, under `web/src/features/templates/`:

| File | Responsibility |
| --- | --- |
| `testSupport.ts` | Fixtures shared by the templates tests: `revision`, `variable`, `registration`, `authValue`, `testQueryClient`, `jsonResponse`, `TENANT`. |
| `templateLinks.ts` | `githubLinks(revision)` and `rootPathOf(rootPath)`. |
| `templateRoutes.ts` | `TemplateTab`, `templateTabOf(pathname)`, `templatePath(id, tab)`. |
| `templatesPageOutlet.ts` | The outlet context `TemplatesPage` hands its routes (`indexTemplateId`). |
| `templateContext.ts` | `TemplateContext` and `useTemplate()`: the template a panel is about. |
| `TemplatesPage.tsx` | Route layout: loading and errors, header, list column, panel column, phone rules. |
| `TemplateList.tsx` | The list column: filter, repository groups, rows. |
| `TemplatePanel.tsx` | The panel header (name, description, tags, Sync, details, tabs) and its content outlet. |
| `TemplatesIndexPanel.tsx` | `/templates` itself: the default template on Variables, or the empty registry. |
| `VariablesTab.tsx` | The Variables table. |
| `RevisionsTab.tsx` | The Revisions table. |
| `TemplateSync.tsx` | Sync and its result. |
| `RegisterTemplatePanel.tsx` | Register template inside the panel. |

Create, under `web/src/shared/`: `ExternalLink.tsx` (a link out of openplan) and `UnderlineTabs.tsx` (openplan UI's Tabs, from `TemplateTabs`).

Modify: `web/src/features/templates/templateWorkflow.ts` (export `sourceTemplateKey`, add `matchesTemplateFilter`, later drop `unsettledStatusTone`), `web/src/features/stacks/TemplateTabs.tsx` (draw with `UnderlineTabs`), `web/src/app/router.tsx`, `web/src/app/router.test.tsx`.

Delete: `TemplateRegistryScreen.tsx`, `TemplateDetailScreen.tsx`, `TemplateRegistrationScreen.tsx` and their `.test.tsx` files.

The list-and-panel frame mirrors `StackPage`'s classes instead of sharing a component: the two pages' guards, ids and outlet contexts differ, and the frame is a dozen lines. Both files say so in a comment (Task 4).

---

### Task 1: GitHub links and ExternalLink

**Files:**
- Create: `web/src/features/templates/testSupport.ts`
- Create: `web/src/features/templates/templateLinks.ts`
- Create: `web/src/features/templates/templateLinks.test.ts`
- Create: `web/src/shared/ExternalLink.tsx`
- Create: `web/src/shared/ExternalLink.test.tsx`

**Interfaces:**
- Produces: `githubLinks(revision: Pick<TemplateRevision, "repo_owner" | "repo_name" | "source_ref" | "root_path" | "resolved_commit_sha">): GitHubLinks` with `{ repository: string; tree: string; ref: string; commit: string }`; `rootPathOf(rootPath: string): string | null`; `ExternalLink({ href, site, mono?, testId?, children })` (default export); and the fixtures in `testSupport.ts` used by every later test.

- [x] Step 1: the shared test fixtures (`TENANT`, `revision`, `variable`, `registration`, `testQueryClient`, `authValue`, `jsonResponse`).
- [x] Step 2: the failing tests for the links, 8 of them.
- [x] Step 3: run them to see them fail. `npx vitest run src/features/templates/templateLinks.test.ts`, expected FAIL, "Failed to resolve import ./templateLinks".
- [x] Step 4: implement the links.
- [x] Step 5: run the link tests to see them pass. Same command, expected PASS, 8 tests.
- [x] Step 6: the failing `ExternalLink` test.
- [x] Step 7: run it to see it fail. `npx vitest run src/shared/ExternalLink.test.tsx`, expected FAIL, "Failed to resolve import ./ExternalLink".
- [x] Step 8: implement `ExternalLink`.
- [x] Step 9: run both files. `npx vitest run src/shared/ExternalLink.test.tsx src/features/templates/templateLinks.test.ts`, expected PASS, 10 tests.
- [x] Step 10: commit.

```bash
git add src/features/templates/testSupport.ts src/features/templates/templateLinks.ts src/features/templates/templateLinks.test.ts src/shared/ExternalLink.tsx src/shared/ExternalLink.test.tsx
git commit -m "feat(web): link a template's repository, folder, ref and commit on GitHub

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Template routes, the filter, and the template key

**Files:**
- Create: `web/src/features/templates/templateRoutes.ts`
- Create: `web/src/features/templates/templateRoutes.test.ts`
- Modify: `web/src/features/templates/templateWorkflow.ts` (export `sourceTemplateKey`, add `matchesTemplateFilter`)
- Modify: `web/src/features/templates/templateWorkflow.test.ts`

**Interfaces:**
- Produces: `type TemplateTab = "variables" | "revisions"`; `templateTabOf(pathname: string): TemplateTab`; `templatePath(sourceTemplateId: string, tab?: TemplateTab): string`; `sourceTemplateKey(revision: TemplateRevision): string` (now exported); `matchesTemplateFilter(sourceTemplate: SourceTemplateGroup, query: string): boolean`.

- [x] Step 1: the failing route tests.
- [x] Step 2: the failing filter tests, appended to `templateWorkflow.test.ts` (importing `revision` from `./testSupport`; the file's own helper is `templateRevision`, so the names do not clash).
- [x] Step 3: run them to see them fail. `npx vitest run src/features/templates/templateRoutes.test.ts src/features/templates/templateWorkflow.test.ts`, expected FAIL: `./templateRoutes` does not resolve, and `matchesTemplateFilter` / `sourceTemplateKey` are not exported.
- [x] Step 4: implement the routes.
- [x] Step 5: implement the filter and export the key: make `sourceTemplateKey` exported (its comment stays), and add `matchesTemplateFilter` after `revisionsForSourceTemplate`.
- [x] Step 6: run the tests to see them pass. Same command, expected PASS.
- [x] Step 7: commit.

```bash
git add src/features/templates/templateRoutes.ts src/features/templates/templateRoutes.test.ts src/features/templates/templateWorkflow.ts src/features/templates/templateWorkflow.test.ts
git commit -m "feat(web): address a template's tabs, and filter templates by what they are

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: UnderlineTabs, and the stack page's tabs on it

**Files:**
- Create: `web/src/shared/UnderlineTabs.tsx`
- Create: `web/src/shared/UnderlineTabs.test.tsx`
- Modify: `web/src/features/stacks/TemplateTabs.tsx` (whole file)

**Interfaces:**
- Produces: `UnderlineTabs({ label: string; tabs: UnderlineTab[] })` (default export) with `interface UnderlineTab { to: string; label: string; current: boolean; count?: number; testId?: string }`.
- Consumes: `useStackCapabilities(stackId)` from `web/src/auth/useStackCapabilities.ts`.

- [x] Step 1: the failing test.
- [x] Step 2: run it to see it fail. `npx vitest run src/shared/UnderlineTabs.test.tsx`, expected FAIL, "Failed to resolve import ./UnderlineTabs".
- [x] Step 3: implement `UnderlineTabs`.
- [x] Step 4: run it to see it pass. Same command, expected PASS.
- [x] Step 5: draw the stack page's tabs with it, replacing `web/src/features/stacks/TemplateTabs.tsx` whole.
- [x] Step 6: run the stack page's tests to see nothing moved. `npx vitest run src/features/stacks src/shared/UnderlineTabs.test.tsx`, expected PASS, every existing stack test unchanged.
- [x] Step 7: commit.

```bash
git add src/shared/UnderlineTabs.tsx src/shared/UnderlineTabs.test.tsx src/features/stacks/TemplateTabs.tsx
git commit -m "refactor(web): draw a template's tabs from data, so the registry can share them

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: TemplatesPage: header, list and frame

**Files:**
- Create: `web/src/features/templates/templatesPageOutlet.ts`
- Create: `web/src/features/templates/TemplateList.tsx`
- Create: `web/src/features/templates/TemplatesPage.tsx`
- Create: `web/src/features/templates/TemplatesPage.test.tsx`
- Modify: `web/src/features/stacks/StackPage.tsx` (one comment line)

**Interfaces:**
- Consumes: `templatePath`, `templateTabOf` (Task 2); `matchesTemplateFilter`, `groupTemplatesByRepository`, `templateRootPathLabel`, `TemplateRepositoryGroup` from `templateWorkflow.ts`; `SearchField`, `PageHeader`, `listItemClass`, `buttonClass`, `ErrorLine`, `RequireCapability`.
- Produces: `TemplatesPage` (default export, route layout); `TemplateList({ groups, selectedId })`; `interface TemplatesPageOutletContext { indexTemplateId: string | null }` and `useIndexTemplateId(): string | null` from `templatesPageOutlet.ts`. Test ids: `templates-page`, `templates-loading`, `templates-error`, `templates-retry`, `register-template-link`, `templates-filter`, `templates-none`, `templates-filter-empty`, `template-group-<repo key>`, `template-link-<source template id>`, `templates-list-column`, `templates-panel-column`.

- [x] Step 1: the failing tests, 17 of them, including Review focus 1 and 5.
- [x] Step 2: run them to see them fail. `npx vitest run src/features/templates/TemplatesPage.test.tsx`, expected FAIL, "Failed to resolve import ./TemplatesPage".
- [x] Step 3: the outlet context.
- [x] Step 4: the list.
- [x] Step 5: the page.
- [x] Step 6: note the mirror in `StackPage`, one line at the end of the comment above `export default function StackPage()`.
- [x] Step 7: run the tests to see them pass. Same command, expected PASS, 17 tests.
- [x] Step 8: commit.

```bash
git add src/features/templates/templatesPageOutlet.ts src/features/templates/TemplateList.tsx src/features/templates/TemplatesPage.tsx src/features/templates/TemplatesPage.test.tsx src/features/stacks/StackPage.tsx
git commit -m "feat(web): list templates by repository beside the selected one

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: TemplatePanel: header, details and tabs

**Files:**
- Create: `web/src/features/templates/templateContext.ts`
- Create: `web/src/features/templates/TemplatePanel.tsx`
- Create: `web/src/features/templates/TemplatePanel.test.tsx`

**Interfaces:**
- Consumes: `githubLinks`, `rootPathOf` (Task 1); `ExternalLink` (Task 1); `templatePath`, `templateTabOf` (Task 2); `UnderlineTabs` (Task 3); `revisionsForSourceTemplate`, `templateDisplayName`, `shortCommitSHA` from `templateWorkflow.ts`; `useTemplateRevisionsQuery`, `useTemplateRevisionVariablesQuery`.
- Produces: `TemplatePanel({ sourceTemplateId?: string; children?: ReactNode })` (default export); `TemplateContext`, `useTemplate(): { sourceTemplateId: string; revisions: TemplateRevision[]; latest: TemplateRevision }` from `templateContext.ts`. Test ids: `template-panel`, `template-missing`, `template-description`, `template-no-description`, `template-details`, `template-sync-slot` (where Task 8 mounts Sync).

- [x] Step 1: the failing tests.
- [x] Step 2: run them to see them fail. `npx vitest run src/features/templates/TemplatePanel.test.tsx`, expected FAIL, "Failed to resolve import ./TemplatePanel".
- [x] Step 3: the context.
- [x] Step 4: the panel.
- [x] Step 5: run the tests to see them pass. Same command, expected PASS, 6 tests.
- [x] Step 6: commit.

```bash
git add src/features/templates/templateContext.ts src/features/templates/TemplatePanel.tsx src/features/templates/TemplatePanel.test.tsx
git commit -m "feat(web): show a template's description, tags and GitHub details in its panel

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: The Variables tab and /templates itself

**Files:**
- Create: `web/src/features/templates/VariablesTab.tsx`
- Create: `web/src/features/templates/VariablesTab.test.tsx`
- Create: `web/src/features/templates/TemplatesIndexPanel.tsx`
- Create: `web/src/features/templates/TemplatesIndexPanel.test.tsx`

**Interfaces:**
- Consumes: `useTemplate()` (Task 5); `useIndexTemplateId()` (Task 4); `TemplatePanel` (Task 5); `TemplatesPage` (Task 4).
- Produces: `VariablesTab` and `TemplatesIndexPanel` (default exports). Test ids: `template-variables`, `template-variables-none`, `template-variables-loading`, `template-variables-error`, `template-variables-retry`, `template-variable-<name>`, `templates-empty`.

- [x] Step 1: the failing Variables tests.
- [x] Step 2: run them to see them fail. `npx vitest run src/features/templates/VariablesTab.test.tsx`, expected FAIL, "Failed to resolve import ./VariablesTab".
- [x] Step 3: the Variables tab.
- [x] Step 4: run the Variables tests to see them pass. Same command, expected PASS, 5 tests.
- [x] Step 5: the failing index tests.
- [x] Step 6: run them to see them fail. `npx vitest run src/features/templates/TemplatesIndexPanel.test.tsx`, expected FAIL, "Failed to resolve import ./TemplatesIndexPanel".
- [x] Step 7: the index panel.
- [x] Step 8: run both files to see them pass. `npx vitest run src/features/templates/VariablesTab.test.tsx src/features/templates/TemplatesIndexPanel.test.tsx`, expected PASS, 8 tests.
- [x] Step 9: commit.

```bash
git add src/features/templates/VariablesTab.tsx src/features/templates/VariablesTab.test.tsx src/features/templates/TemplatesIndexPanel.tsx src/features/templates/TemplatesIndexPanel.test.tsx
git commit -m "feat(web): show what a template asks for, and open /templates on the first one

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: The Revisions tab

**Files:**
- Create: `web/src/features/templates/RevisionsTab.tsx`
- Create: `web/src/features/templates/RevisionsTab.test.tsx`

**Interfaces:**
- Consumes: `useTemplate()` (Task 5); `githubLinks` (Task 1); `ExternalLink` (Task 1); `revisionIndicator` from `./revisionIndicator`; `StatusLabel`.
- Produces: `RevisionsTab` (default export). Test ids: `template-revisions`, `revision-row-<id>`, `revision-latest`.

- [x] Step 1: the failing tests.
- [x] Step 2: run them to see them fail. `npx vitest run src/features/templates/RevisionsTab.test.tsx`, expected FAIL, "Failed to resolve import ./RevisionsTab".
- [x] Step 3: the Revisions tab.
- [x] Step 4: run the tests to see them pass. Same command, expected PASS, 3 tests.
- [x] Step 5: commit.

```bash
git add src/features/templates/RevisionsTab.tsx src/features/templates/RevisionsTab.test.tsx
git commit -m "feat(web): list a template's revisions with their commits on GitHub

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Sync, said in words

**Files:**
- Create: `web/src/features/templates/TemplateSync.tsx`
- Create: `web/src/features/templates/TemplateSync.test.tsx`
- Modify: `web/src/features/templates/TemplatePanel.tsx` (replace the `template-sync-slot` div)

**Interfaces:**
- Consumes: `useTemplate()` (Task 5); `useRegisterTemplateMutation`, `useTemplateRegistrationQuery`; `isTerminalRegistrationStatus` from `../../api/polling`; `queryKeys`.
- Produces: `TemplateSync` (default export). Test ids: `template-sync`, `template-sync-result`, `template-sync-error`.

- [x] Step 1: the failing tests, the behaviour tests of `TemplateDetailScreen.test.tsx` moved, plus the new result line and Review focus 2.
- [x] Step 2: run them to see them fail. `npx vitest run src/features/templates/TemplateSync.test.tsx`, expected FAIL: `template-sync` is not in the document (and `./TemplateSync` is not imported anywhere yet).
- [x] Step 3: Sync itself.
- [x] Step 4: mount it in the panel: add the import, and replace the `template-sync-slot` div with `<TemplateSync />`.
- [x] Step 5: run Sync's and the panel's tests to see them pass. `npx vitest run src/features/templates/TemplateSync.test.tsx src/features/templates/TemplatePanel.test.tsx`, expected PASS (the panel tests seed no registration and never press Sync).
- [x] Step 6: commit.

```bash
git add src/features/templates/TemplateSync.tsx src/features/templates/TemplateSync.test.tsx src/features/templates/TemplatePanel.tsx
git commit -m "feat(web): sync a template from its panel and say what the sync found

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Register template inside the panel

**Files:**
- Create: `web/src/features/templates/RegisterTemplatePanel.tsx`
- Create: `web/src/features/templates/RegisterTemplatePanel.test.tsx`

**Interfaces:**
- Consumes: `sourceTemplateKey` (Task 2); `templatePath` (Task 2); `useRegisterTemplateMutation`, `useTemplateRegistrationQuery`, `useTemplateRevisionsQuery`; `inputClass`, `fieldLabelClass` from `../../shared/fieldClass`; shadcn `Input` and `Label`.
- Produces: `RegisterTemplatePanel` (default export). Test ids: `register-template-panel`, `register-template-cancel`, `register-template-submit`, `register-template-progress`, `register-template-error`.

- [x] Step 1: the failing tests, the behaviour tests of `TemplateRegistrationScreen.test.tsx` moved and updated to the new outcome, plus Review focus 3 and 4.
- [x] Step 2: run them to see them fail. `npx vitest run src/features/templates/RegisterTemplatePanel.test.tsx`, expected FAIL, "Failed to resolve import ./RegisterTemplatePanel".
- [x] Step 3: the panel.
- [x] Step 4: run the tests to see them pass. Same command, expected PASS, 8 tests.
- [x] Step 5: commit.

```bash
git add src/features/templates/RegisterTemplatePanel.tsx src/features/templates/RegisterTemplatePanel.test.tsx
git commit -m "feat(web): register a template inside the templates page, and open it when done

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Wire the routes and remove the old screens

**Files:**
- Modify: `web/src/app/router.tsx` (imports, and the three `templates` routes)
- Modify: `web/src/app/router.test.tsx` (the two template route tests, plus three new ones)
- Modify: `web/src/features/templates/templateWorkflow.ts` (drop `unsettledStatusTone` and its `statusTone` imports)
- Modify: `web/src/features/templates/templateWorkflow.test.ts` (drop `unsettledStatusTone`'s tests)
- Delete: `web/src/features/templates/TemplateRegistryScreen.tsx`, `TemplateRegistryScreen.test.tsx`, `TemplateDetailScreen.tsx`, `TemplateDetailScreen.test.tsx`, `TemplateRegistrationScreen.tsx`, `TemplateRegistrationScreen.test.tsx`

**Interfaces:**
- Consumes: `TemplatesPage`, `TemplatesIndexPanel`, `RegisterTemplatePanel`, `TemplatePanel`, `VariablesTab`, `RevisionsTab` (Tasks 4–9).

- [x] Step 1: update the router tests first: replace `"renders the template registry screen at /templates"` and `"renders the template registration screen at /templates/new"` with the five new route tests. (`renderToStaticMarkup` runs no effects, so the template index is a loader redirect, as `/stacks/:stackId/templates` is, and the tests poll the router's state with `vi.waitFor`.)
- [x] Step 2: run the router tests to see the new ones fail. `npx vitest run src/app/router.test.tsx`, expected FAIL on the five new tests (the old screens are still routed).
- [x] Step 3: route the new page: swap the three old screen imports for the new ones (`RegistryTemplatePanel` and `RegistryVariablesTab` keep them apart from the stack page's `TemplatePanel` and `TemplateVariablesTab`, which this file also imports; `redirect` is already imported from `react-router-dom`), then replace the three routes with the `templates` layout and its children.
- [x] Step 4: delete the old screens and their tests.

```bash
git rm src/features/templates/TemplateRegistryScreen.tsx src/features/templates/TemplateRegistryScreen.test.tsx \
  src/features/templates/TemplateDetailScreen.tsx src/features/templates/TemplateDetailScreen.test.tsx \
  src/features/templates/TemplateRegistrationScreen.tsx src/features/templates/TemplateRegistrationScreen.test.tsx
```

- [x] Step 5: drop `unsettledStatusTone`. Confirm nothing else calls it: `grep -rn "unsettledStatusTone" src --include='*.ts' --include='*.tsx'`, expected only `templateWorkflow.ts` and `templateWorkflow.test.ts`. Then delete the two `statusTone` imports at the top of `templateWorkflow.ts`, the `unsettledStatusTone` function with its comment, and its `describe` block and import in the test.
- [x] Step 6: run the whole suite and the build. `npm test`, expected PASS, including `src/styles/*.guard.test.ts` (no arbitrary values, palette colours or colour literals; every table on shadcn's Table with a colgroup). Then `npm run build`, expected exit 0, no type errors.
- [x] Step 7: commit.

```bash
git add -A src/app/router.tsx src/app/router.test.tsx src/features/templates
git commit -m "feat(web): route /templates to the new page and remove the old template screens

/templates/new now requires canPublishTemplate, which it never checked.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Check it in a browser

**Files:** none changed unless a check fails.

- [x] Step 1: start the stack and the driver, following the header of `scripts/drive-web.mjs`: the local stack running (`README.md`), `npm run dev` in `web/`, headless Chrome on port 9222, `OPENPLAN_USER` and `OPENPLAN_PASS` exported. Register at least one template first if the tenant has none (through the page itself, which is part of the check).
- [x] Step 2: shoot each state at desktop width, from the repository root:

```bash
node scripts/drive-web.mjs --click Templates --shot /tmp/templates-index.png
node scripts/drive-web.mjs --click Templates --click Revisions --shot /tmp/templates-revisions.png
node scripts/drive-web.mjs --click Templates --click "Register template" --shot /tmp/templates-register.png
node scripts/drive-web.mjs --respond '*v1/tenants/*template-revisions=[]' --click Templates --shot /tmp/templates-empty.png
```

  Compare each with its K board: `TemplatesPage`, `TemplatesPageRevisions`, `TemplatesPageRegister`, `TemplatesPageEmpty`. Check that the details block's four links open GitHub in a new tab (`--probe 'Array.from(document.querySelectorAll("[data-testid=template-details] a")).map(a => a.target + " " + a.href)'`).
- [x] Step 3: shoot the phone layout. Restart Chrome with `--window-size=375,812` and repeat Step 2's first two commands. Expected: `/templates` shows only the list; a template shows only its panel with the "Templates" back link, and the variables table scrolls sideways inside its frame, not the page.
- [x] Step 4: fix anything that differs, then commit as `fix(web): …`. Nothing needed fixing, so there was nothing to commit.

---

## Fixes after the tasks

Recorded here because each is part of what this branch landed, and each has a test.

| Commit | Fix |
| --- | --- |
| `a5a9728` | Keep Register template busy until the new template can open. |
| `1d58140` | Say a registration may still finish when it cannot be checked on. |
| `0698c0b` | Refetch the templates when the panel moves to another one. |
| `278224e` | Say the variables are loading, not the previous revision's, after a sync. |
| `bcfa76f` | Keep Register template's status region mounted so its progress is announced. |
| `cda5f11` | Read null template tags and variables as empty lists in the web layer. |
| `c437283` | Store and read no template or stack tags as empty, not null. |
| `2412128` | Store and read no default credentials as empty, not null. |
| `5e1f231` | Pin that Register template gives way when its list cannot load. |

### The gap that was not one

`RegisterTemplatePanel`'s `opening` was once recorded here as a known gap: a refetch that failed, or ran before the revision was in the list, would leave the panel busy for good. Neither can. The sync workflow records the registration completed only with the id of the revision it already wrote, so a list fetched after the poll reads completed holds it; and a failed refetch puts the list query in error, which `TemplatesPage` draws in place of the panel. `5e1f231` corrects the comment and pins the second through the page.

The dead `if credentialIDsJSON == nil` guard that predated this work (`8247ec7`) hid a real write: the service hands a stack without default credentials a nil slice, so every such stack was stored as `null`. `2412128` fixes it the way `c437283` fixed tags.

## After merge

Sync the "openplan UI" design system artifact (https://claude.ai/artifact/4hCc6LddTF31cNeaqHwZn8), as the spec's "After this lands" lists: `arrow-up-right` in Icons, the external link pattern, a Templates page pattern in the README, `/templates` under Pages in `code.md` with the new files in its component table, and the registry removed from "Not yet".
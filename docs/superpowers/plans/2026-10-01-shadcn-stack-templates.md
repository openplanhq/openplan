# Stack templates on shadcn (PR 6) Implementation Plan

> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move a stack's template screens onto shadcn components and Tailwind utilities, and delete the legacy CSS they stop using. The screens are the template list, the template detail shell and its four tabs (Runs, Variables, Credentials, Settings), the config panel, the variable fields, Add template and Change revision.

**Architecture:**
- **Template tab row.** `StackTemplateDetailShell` draws `RouteTabs` (PR 4) with the template's state, a `StatusBadge`, at the far end of the same row. The active tab comes from the pathname, as in `StackDetailShell`: everything under `runs/` keeps Runs selected.
- **Template list.** A bordered list of links, like the grants list in PR 5. Each row has its state glyph, the name, and the state label. The header has the `Add template` link styled as a Button. With nothing installed, the screen shows an `Empty` with a border.
- **Variables.** `VariableFields` becomes a two-column grid of `Label` and `Input`. `StackTemplateConfigPanel` becomes a Card.
- **Settings.** The revision action becomes a muted, bordered band holding an outline Button-styled link, or a disabled Button with its reason.
- **Add template.** The picker rows become bordered buttons, grouped under each repository's heading, and show a `StatusBadge` when the template's latest revision is not active. The configure pane becomes a Card. Its revision picker becomes a Select.
- **Change revision.** The target picker becomes a Select inside a Card. The notes about added and removed variables become plain lists.
- **Out of scope for PR 6:** the run components the Runs and Settings tabs embed (`TemplateRunActions`, `TemplateRunHistory`, `TemplateDestroyPanel`, run detail) stay legacy; the combined PR 7 migration moves them with the template registry screens. Only their wrappers move in PR 6.

**Spec:** `docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md`. Read "What PR 2 settled" through "What PR 5 settled", "What every screen PR must do", and "Guards".

## Decisions the spec left open

1. **Status colours outside a pill.** The list's state glyph and the Change revision notes need a tone's text colour without the Badge. `src/shared/statusTone.ts` gains `toneTextClass(tone)`, mapping each tone to the text class of its Badge variant (`text-success`, `text-primary`, `text-warning`, `text-destructive`, `text-muted-foreground`). The class names are literals, so Tailwind finds them.
2. **The install picker keeps its own row style.** The registry list (PR 7) still uses `.templates-list`. The picker rows become Tailwind-styled buttons now; PR 7 can share a component with them once the registry moves.
3. **Two columns on a wide screen.** The install screen splits 3:4 from `lg`, as the legacy `.workflow-grid` (0.85fr and 1.15fr) did and as PR 5's access screen does. The configure pane stays sticky beside the list from `lg` up.
4. **Variables keep two columns from `md`,** where the legacy grid collapsed at 920px. Each input's id comes from `useId`, so a label always names its own input.

## Global Constraints

- **Branch.** `feat/shadcn-stack-templates`, from `main`, with PRs 0 to 5 merged. The PR base is `main`.
- **Copy, test IDs and behaviour stay word for word.** Every `data-testid` in these screens stays, because other tests depend on them. The `data-unsaved` markers stay too, because `SessionProvider` reads them.
- **Accessible names don't change.** Tab links stay `Runs`, `Variables`, `Credentials` and `Settings`, inside a list named `Template sections`. Variable inputs keep `<name>` or `<name> *` as their label. The selects take the labels `Revision` and `Revision to apply`.
- **Classes.** App code uses theme colours and scale steps only, so `tailwind.guard.test.ts` passes. Every migrated `h2` sets `font-heading text-base leading-snug font-medium tracking-normal`, because `base.css` still styles bare `h2`. The outermost element of each screen sets `text-foreground`.
- **44px targets on coarse pointers:** `pointer-coarse:h-11` on Buttons and Inputs, `pointer-coarse:data-[size=default]:h-11` on Select triggers, `pointer-coarse:min-h-11` on SelectItems, picker rows and list rows.
- **Links styled as buttons** use `buttonVariants()` with `no-underline`, as in `StacksListScreen`.
- **Dead CSS.** Delete every rule these screens stop using. Rules still used by the registry, the runs screens or the legacy specimen stay. Check with `rg` after deleting.
- **Tests** find elements by role, label, text or test ID, never by legacy class. Selects are driven with `@testing-library/user-event`.

## Review Focus

1. **Switching the install or upgrade revision while the variables load.** The old fields and diff notes must not show, and Install or Change revision must stay disabled. Existing tests cover this; they move to the Select.
2. **A template with no active revision in the install picker.** The row is disabled and its `StatusBadge` says why.
3. **The tab row on a run's page.** Runs stays selected (`aria-current="page"`), and the template's state is left off.
4. **Long template names on a 375px phone.** List rows truncate the name, the state label keeps its place, and the page doesn't scroll sideways.

---

### Task 1: Template list, detail shell and tab row

- [ ] Add `toneTextClass` to `src/shared/statusTone.ts`, with a test.
- [ ] `StackTemplateDetailShell`: loading, error, missing and loaded states on Tailwind. Use `RouteTabs` (`className="mb-0"`) in a flex row with a bottom border, and a `StatusBadge` (title = description, test ID kept) at its end, left off a run's page. Wrap the outlet in `grid min-w-0 content-start gap-6`.
- [ ] `StackTemplateListScreen`: header (h2 and link), bordered link list, `Empty` for no templates, loading and error states.
- [ ] Tests: replace the `className` "active" checks with `aria-current`, and keep every other assertion.
- [ ] Delete the legacy rules: `.stack-detail-tabs`, `.stack-template-tabbar`, `.stack-template-items`, `.stack-template-list-content`, `.stack-template-detail`, and `.panel-header` if nothing else uses it.
- [ ] Commit: `feat(web): stack template list and tab row on shadcn`.

### Task 2: Variables, config panel and the tab wrappers

- [ ] `VariableFields`: `md:grid-cols-2` of Label and Input, with ids from `useId`.
- [ ] `StackTemplateConfigPanel`: a Card, the `Save config` Button, and the reason as muted text.
- [ ] `TemplateVariablesTab`: loading and error states, and the error message as an Alert.
- [ ] `TemplateSettingsTab`: the revision band. `TemplateRunsTab` and `TemplateCredentialsTab`: plain grid wrappers.
- [ ] Delete `.variable-grid`, `.stack-template-tab`, `.stack-template-revision-action`, and `.form-actions` / `.button-row` uses that are now dead.
- [ ] Commit: `feat(web): template variables and settings on shadcn`.

### Task 3: Add template

- [ ] Picker groups and rows, the configure Card, a Select for the revision, Alerts for errors, and an `Empty` when no template is registered.
- [ ] Tests: replace the class queries (`.templates-list__name`, `.template-choices li`, `.status-tone`) with role and text queries, and drive the revision Select with user-event.
- [ ] Delete `.template-choices`, `.template-choice`, `.template-configure`, `.workflow-grid` and `.selector-label` if nothing else uses them.
- [ ] Commit: `feat(web): add template on shadcn`.

### Task 4: Change revision

- [ ] The Card, the Select for the target, the notes, and an Alert for errors.
- [ ] Tests: drive the Select with user-event, and read its options from the listbox.
- [ ] Delete `.upgrade-variable-notes`.
- [ ] Commit: `feat(web): change revision on shadcn`.

### Task 5: Audit, spec and PR

- [ ] Run `npm test`, `npm run build` and `python3 scripts/audit-css.py`.
- [ ] Browser check with `scripts/drive-web.mjs` (see the `openplan-drive-web` skill): the list, a template's tabs, Add and Change revision, at desktop width and 375px, then stop Chrome.
- [ ] Add "What PR 6 settled" to the spec, and fix checklist item 5's stale sign-in note (Dex's `admin@openplan.local`, not root or Keycloak).
- [ ] Open the PR.

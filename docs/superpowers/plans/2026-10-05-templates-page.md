# Templates Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild `/templates`, `/templates/new` and `/templates/:id` as one openplan UI split-view page: templates grouped by repository on the left, the selected template's panel (description, tags, Sync, GitHub details, Variables and Revisions tabs) on the right, and Register template inside the panel.

**Architecture:** `TemplatesPage` is a route layout like `StackPage`: it loads the tenant's revisions once, draws the header and the list, and renders the selected template's route into its `<Outlet/>`. `TemplatePanel` resolves the template from the URL and hands it to its tabs through a React context, as the stack page's `TemplatePanel` does. Everything is derived from the existing revisions, variables and registration endpoints; nothing on the server changes.

**Tech Stack:** React 19, react-router-dom 6 (data router), TanStack Query 5, Tailwind v4 on `web/src/styles/theme.css`, shadcn/ui (base-nova on Base UI), lucide-react 0.468, Vitest 4 + Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-05-templates-page-design.md`. Design boards: direction K on https://claude.ai/artifact/74qBEVqi1VAWDrEtFcnbhh (`TemplatesPage.dc.html` and its state boards).

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

---

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

- [ ] **Step 1: Write the shared test fixtures**

`web/src/features/templates/testSupport.ts`:

```ts
import { QueryClient } from "@tanstack/react-query";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { TemplateRegistration, TemplateRevision, TemplateVariable } from "../../api/types";

// Fixtures for the templates tests. Not a test file itself, so it carries no
// assertions and imports nothing from Vitest.

export const TENANT = "tenant_123";

export function revision(overrides: Partial<TemplateRevision> = {}): TemplateRevision {
  return {
    id: "rev_1",
    tenant_id: TENANT,
    source_template_id: "tpl_1",
    repo_owner: "acme",
    repo_name: "infra-modules",
    source_ref: "main",
    resolved_commit_sha: "3f9c2a1d8e7b6a5c4d3e2f1a0b9c8d7e6f5a4b3c",
    root_path: "aws/eks",
    name: "eks",
    description: "An EKS cluster with one managed node group.",
    tags: ["aws", "kubernetes"],
    status: "active",
    created_at: "2026-09-22T10:00:00Z",
    ...overrides
  };
}

export function variable(overrides: Partial<TemplateVariable> = {}): TemplateVariable {
  return {
    template_revision_id: "rev_1",
    name: "cluster_name",
    type_expression: "string",
    description: "Name of the EKS cluster.",
    required: true,
    has_default: false,
    sensitive: false,
    has_validation: false,
    ...overrides
  };
}

export function registration(overrides: Partial<TemplateRegistration> = {}): TemplateRegistration {
  return {
    id: "reg_1",
    tenant_id: TENANT,
    repo_owner: "acme",
    repo_name: "infra-modules",
    source_ref: "main",
    root_path: "aws/eks",
    status: "completed",
    step: "syncing",
    template_revision_id: "rev_1",
    resolved_commit_sha: "3f9c2a1d8e7b6a5c4d3e2f1a0b9c8d7e6f5a4b3c",
    requested_by: "user_1",
    requested_at: "2026-10-05T09:00:00Z",
    error_summary: "",
    ...overrides
  };
}

// retry: false and staleTime: Infinity: seeded data never triggers a refetch,
// so a test only reaches fetch() when it means to.
export function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

export function authValue(canPublishTemplate = true): AuthContextValue {
  return {
    me: {
      sub: "user_1",
      tenantID: TENANT,
      displayName: "Test User",
      globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate }
    },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}
```

- [ ] **Step 2: Write the failing tests for the links**

`web/src/features/templates/templateLinks.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { githubLinks, rootPathOf } from "./templateLinks";
import { revision } from "./testSupport";

describe("githubLinks", () => {
  it("links the repository, the module's folder at the ref, the ref and the commit", () => {
    expect(githubLinks(revision())).toEqual({
      repository: "https://github.com/acme/infra-modules",
      tree: "https://github.com/acme/infra-modules/tree/main/aws/eks",
      ref: "https://github.com/acme/infra-modules/tree/main",
      commit: "https://github.com/acme/infra-modules/commit/3f9c2a1d8e7b6a5c4d3e2f1a0b9c8d7e6f5a4b3c"
    });
  });

  it.each([".", "", "./", " . "])("points a module at the root (%j) at the ref itself", (rootPath) => {
    const links = githubLinks(revision({ root_path: rootPath }));
    expect(links.tree).toBe("https://github.com/acme/infra-modules/tree/main");
  });

  it("keeps the slashes of a ref and a path, and drops . segments", () => {
    const links = githubLinks(revision({ source_ref: "release/1.2", root_path: "./modules/eks/" }));
    expect(links.ref).toBe("https://github.com/acme/infra-modules/tree/release/1.2");
    expect(links.tree).toBe("https://github.com/acme/infra-modules/tree/release/1.2/modules/eks");
  });

  it("encodes each segment, so a # or a space cannot end the URL early", () => {
    const links = githubLinks(revision({ source_ref: "feature#1", root_path: "my module" }));
    expect(links.tree).toBe("https://github.com/acme/infra-modules/tree/feature%231/my%20module");
  });
});

describe("rootPathOf", () => {
  it("returns the path, or null for a module at the repository root", () => {
    expect(rootPathOf("aws/eks")).toBe("aws/eks");
    expect(rootPathOf(" aws/eks ")).toBe("aws/eks");
    expect(rootPathOf(".")).toBeNull();
    expect(rootPathOf("")).toBeNull();
  });
});
```

- [ ] **Step 3: Run them to see them fail**

Run: `npx vitest run src/features/templates/templateLinks.test.ts`
Expected: FAIL, "Failed to resolve import ./templateLinks".

- [ ] **Step 4: Implement the links**

`web/src/features/templates/templateLinks.ts`:

```ts
import type { TemplateRevision } from "../../api/types";

const GITHUB = "https://github.com";

type LinkedRevision = Pick<TemplateRevision, "repo_owner" | "repo_name" | "source_ref" | "root_path" | "resolved_commit_sha">;

/** Where a template revision lives on GitHub, which every repository is cloned from. */
export interface GitHubLinks {
  repository: string;
  /** The module's folder at the ref; the ref itself for a module at the root. */
  tree: string;
  ref: string;
  commit: string;
}

// Each segment of a ref or a path is encoded on its own, so release/1.2 keeps
// its slash while a # or a space cannot end the URL early. "." and empty
// segments name nothing: ./modules/eks/ is modules/eks.
function segments(path: string): string {
  return path
    .trim()
    .split("/")
    .filter((segment) => segment !== "" && segment !== ".")
    .map(encodeURIComponent)
    .join("/");
}

export function githubLinks(revision: LinkedRevision): GitHubLinks {
  const repository = `${GITHUB}/${encodeURIComponent(revision.repo_owner.trim())}/${encodeURIComponent(revision.repo_name.trim())}`;
  const ref = `${repository}/tree/${segments(revision.source_ref)}`;
  const path = segments(revision.root_path);
  return {
    repository,
    ref,
    tree: path === "" ? ref : `${ref}/${path}`,
    commit: `${repository}/commit/${encodeURIComponent(revision.resolved_commit_sha.trim())}`
  };
}

/** The root path as written, or null for a module at the repository root. */
export function rootPathOf(rootPath: string): string | null {
  const trimmed = rootPath.trim();
  return trimmed === "" || trimmed === "." ? null : trimmed;
}
```

- [ ] **Step 5: Run the link tests to see them pass**

Run: `npx vitest run src/features/templates/templateLinks.test.ts`
Expected: PASS, 8 tests.

- [ ] **Step 6: Write the failing ExternalLink test**

`web/src/shared/ExternalLink.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import ExternalLink from "./ExternalLink";

afterEach(cleanup);

describe("ExternalLink", () => {
  it("opens a new tab without handing over the opener, and says where it goes", () => {
    render(
      <ExternalLink href="https://github.com/acme/infra-modules" site="GitHub">
        acme/infra-modules
      </ExternalLink>
    );
    const link = screen.getByRole("link", { name: "acme/infra-modules, on GitHub (opens in a new tab)" });
    expect(link.getAttribute("href")).toBe("https://github.com/acme/infra-modules");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noreferrer");
    expect(link.className).toContain("font-mono");
  });

  it("sets plain words in the sans face", () => {
    render(
      <ExternalLink href="https://github.com/acme/edge/tree/main" site="GitHub" mono={false}>
        the repository root
      </ExternalLink>
    );
    expect(screen.getByRole("link").className).not.toContain("font-mono");
  });
});
```

- [ ] **Step 7: Run it to see it fail**

Run: `npx vitest run src/shared/ExternalLink.test.tsx`
Expected: FAIL, "Failed to resolve import ./ExternalLink".

- [ ] **Step 8: Implement ExternalLink**

`web/src/shared/ExternalLink.tsx`:

```tsx
import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A link out of openplan, as openplan UI draws one: the value it names in the
// foreground, a small arrow, and words for a screen reader saying where it
// goes and that it opens a new tab. Machine values (a repository, a commit)
// are mono; words are not.
export default function ExternalLink({
  href,
  site,
  mono = true,
  testId,
  children
}: {
  href: string;
  /** Where the link goes, as a person names it: "GitHub". */
  site: string;
  mono?: boolean;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      data-testid={testId}
      className={cn(
        "inline-flex max-w-full items-center gap-1 text-meta text-foreground wrap-anywhere hover:text-primary hover:underline",
        mono && "font-mono"
      )}
    >
      {children}
      <ArrowUpRight aria-hidden="true" strokeWidth={2.25} className="size-3 shrink-0 text-subtle-foreground" />
      <span className="sr-only">, on {site} (opens in a new tab)</span>
    </a>
  );
}
```

- [ ] **Step 9: Run both test files to see them pass**

Run: `npx vitest run src/shared/ExternalLink.test.tsx src/features/templates/templateLinks.test.ts`
Expected: PASS, 10 tests.

- [ ] **Step 10: Commit**

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

- [ ] **Step 1: Write the failing route tests**

`web/src/features/templates/templateRoutes.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { templatePath, templateTabOf } from "./templateRoutes";

describe("templateTabOf", () => {
  it.each([
    ["/templates", "variables"],
    ["/templates/new", "variables"],
    ["/templates/tpl_1", "variables"],
    ["/templates/tpl_1/variables", "variables"],
    ["/templates/tpl_1/revisions", "revisions"]
  ])("reads %s as %s", (pathname, tab) => {
    expect(templateTabOf(pathname)).toBe(tab);
  });
});

describe("templatePath", () => {
  it("addresses a template's tab, Variables by default", () => {
    expect(templatePath("tpl_1")).toBe("/templates/tpl_1/variables");
    expect(templatePath("tpl_1", "revisions")).toBe("/templates/tpl_1/revisions");
  });

  it("encodes an id that is the identity tuple of a template with no source id", () => {
    const id = JSON.stringify(["acme", "edge", ".", "main"]);
    expect(templatePath(id)).toBe(`/templates/${encodeURIComponent(id)}/variables`);
  });
});
```

- [ ] **Step 2: Add the failing filter tests**

In `web/src/features/templates/templateWorkflow.test.ts`, add `matchesTemplateFilter` and `sourceTemplateKey` to the import from `./templateWorkflow`, add `import { revision } from "./testSupport";` (the file's own helper is `templateRevision`, so the names do not clash), and append:

```ts
describe("matchesTemplateFilter", () => {
  const [network] = groupTemplatesByRepository([
    revision({ name: "network", root_path: "aws/network", description: "A VPC with public and private subnets.", tags: ["networking"] })
  ])[0].sourceTemplates;

  it.each(["network", "acme/infra", "aws/net", "private subnets", "networking", ""])("matches %j", (query) => {
    expect(matchesTemplateFilter(network, query)).toBe(true);
  });

  it("ignores case and the spaces around what was typed", () => {
    expect(matchesTemplateFilter(network, "  NETWORKING  ")).toBe(true);
    expect(matchesTemplateFilter(network, "VPC")).toBe(true);
  });

  it("does not match what is not there", () => {
    expect(matchesTemplateFilter(network, "kafka")).toBe(false);
  });
});

describe("sourceTemplateKey", () => {
  it("is the source template id, or the identity tuple when there is none", () => {
    expect(sourceTemplateKey(revision({ source_template_id: "tpl_9" }))).toBe("tpl_9");
    expect(sourceTemplateKey(revision({ source_template_id: "", root_path: "." }))).toBe(
      JSON.stringify(["acme", "infra-modules", ".", "main"])
    );
  });
});
```

- [ ] **Step 3: Run them to see them fail**

Run: `npx vitest run src/features/templates/templateRoutes.test.ts src/features/templates/templateWorkflow.test.ts`
Expected: FAIL: `./templateRoutes` does not resolve, and `matchesTemplateFilter` / `sourceTemplateKey` are not exported.

- [ ] **Step 4: Implement the routes**

`web/src/features/templates/templateRoutes.ts`:

```ts
import { matchPath } from "react-router-dom";

/** The tabs of a template's panel, each its own route below the template. */
export type TemplateTab = "variables" | "revisions";

/** The tab a path belongs to; anywhere else on the page, Variables. */
export function templateTabOf(pathname: string): TemplateTab {
  return matchPath("/templates/:sourceTemplateId/revisions/*", pathname) ? "revisions" : "variables";
}

/**
 * Where a template's tab lives. The id is encoded: a template registered
 * before source ids existed is keyed on its identity tuple, which is JSON.
 */
export function templatePath(sourceTemplateId: string, tab: TemplateTab = "variables"): string {
  return `/templates/${encodeURIComponent(sourceTemplateId)}/${tab}`;
}
```

- [ ] **Step 5: Implement the filter and export the key**

In `web/src/features/templates/templateWorkflow.ts`, change `function sourceTemplateKey(` to `export function sourceTemplateKey(` (its comment stays), and add after `revisionsForSourceTemplate`:

```ts
/**
 * Whether a template matches what someone typed in the list's filter: its
 * name, repository, root path, description or tags, ignoring case and the
 * spaces around the query. An empty query matches everything.
 */
export function matchesTemplateFilter(sourceTemplate: SourceTemplateGroup, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const latest = sourceTemplate.latestRevision;
  return [sourceTemplate.name, `${latest.repo_owner}/${latest.repo_name}`, latest.root_path, latest.description, ...latest.tags].some(
    (text) => text.toLowerCase().includes(needle)
  );
}
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `npx vitest run src/features/templates/templateRoutes.test.ts src/features/templates/templateWorkflow.test.ts`
Expected: PASS.

- [ ] **Step 7: Commit**

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

- [ ] **Step 1: Write the failing test**

`web/src/shared/UnderlineTabs.test.tsx`:

```tsx
// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import UnderlineTabs from "./UnderlineTabs";

afterEach(cleanup);

describe("UnderlineTabs", () => {
  it("draws its tabs as links in a named nav, the current one marked, with optional counts", () => {
    render(
      <MemoryRouter>
        <UnderlineTabs
          label="Template sections"
          tabs={[
            { to: "/templates/tpl_1/variables", label: "Variables", current: true, count: 5 },
            { to: "/templates/tpl_1/revisions", label: "Revisions", current: false }
          ]}
        />
      </MemoryRouter>
    );
    const nav = screen.getByRole("navigation", { name: "Template sections" });
    const variables = within(nav).getByRole("link", { name: "Variables 5" });
    expect(variables.getAttribute("href")).toBe("/templates/tpl_1/variables");
    expect(variables.getAttribute("aria-current")).toBe("page");
    expect(variables.className).toContain("border-primary");

    const revisions = within(nav).getByRole("link", { name: "Revisions" });
    expect(revisions.getAttribute("aria-current")).toBeNull();
    expect(revisions.className).toContain("border-transparent");
  });
});
```

- [ ] **Step 2: Run it to see it fail**

Run: `npx vitest run src/shared/UnderlineTabs.test.tsx`
Expected: FAIL, "Failed to resolve import ./UnderlineTabs".

- [ ] **Step 3: Implement UnderlineTabs**

`web/src/shared/UnderlineTabs.tsx`:

```tsx
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";

export interface UnderlineTab {
  to: string;
  label: string;
  current: boolean;
  /** A plain count after the label, as a section title has. */
  count?: number;
  testId?: string;
}

// openplan UI's Tabs: underline links in a nav named for what they switch,
// so each tab has its own address. The current tab is foreground with a 2px
// primary line drawn over the header's divider; the rest are muted until
// hovered. The focus ring is drawn inside, where the header cannot clip it.
export default function UnderlineTabs({ label, tabs }: { label: string; tabs: UnderlineTab[] }) {
  return (
    <nav aria-label={label} className="flex flex-wrap items-center gap-x-6">
      {tabs.map((tab) => (
        <Link
          key={tab.to}
          to={tab.to}
          aria-current={tab.current ? "page" : undefined}
          data-testid={tab.testId}
          className={cn(
            "-mb-px inline-flex h-10 items-center gap-2 border-b-2 text-sm font-medium transition-colors focus-visible:-outline-offset-2 pointer-coarse:h-11",
            tab.current ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
          )}
        >
          {tab.label}
          {tab.count !== undefined && <span className="text-xs font-medium text-subtle-foreground">{tab.count}</span>}
        </Link>
      ))}
    </nav>
  );
}
```

- [ ] **Step 4: Run it to see it pass**

Run: `npx vitest run src/shared/UnderlineTabs.test.tsx`
Expected: PASS.

- [ ] **Step 5: Draw the stack page's tabs with it**

Replace `web/src/features/stacks/TemplateTabs.tsx` with:

```tsx
import { useLocation } from "react-router-dom";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import UnderlineTabs from "../../shared/UnderlineTabs";
import { stackTemplatePath, templateTabOf } from "./templateSelection";
import type { TemplateTab } from "./templateSelection";

// A template's tabs on the stack's page, one per route below the template.
// The route decides the current tab, so a run keeps Runs lit and Change
// revision keeps Settings lit. Credentials shows only to people who may
// manage access, the capability its route requires; while the stack's
// capabilities load, it stays hidden, as RequireCapability would keep it.
export default function TemplateTabs({ stackId, stackTemplateId }: { stackId: string; stackTemplateId: string }) {
  const current = templateTabOf(useLocation().pathname);
  const canManageAccess = useStackCapabilities(stackId)?.canManageAccess === true;
  const tabs: [TemplateTab, string][] = [
    ["runs", "Runs"],
    ["variables", "Variables"],
    ...(canManageAccess ? ([["credentials", "Credentials"]] as [TemplateTab, string][]) : []),
    ["settings", "Settings"]
  ];

  return (
    <UnderlineTabs
      label="Template sections"
      tabs={tabs.map(([value, label]) => ({ to: stackTemplatePath(stackId, stackTemplateId, value), label, current: value === current }))}
    />
  );
}
```

- [ ] **Step 6: Run the stack page's tests to see nothing moved**

Run: `npx vitest run src/features/stacks src/shared/UnderlineTabs.test.tsx`
Expected: PASS, every existing stack test unchanged.

- [ ] **Step 7: Commit**

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

- [ ] **Step 1: Write the failing tests**

`web/src/features/templates/TemplatesPage.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRevision } from "../../api/types";
import TemplatesPage from "./TemplatesPage";
import { authValue, jsonResponse, revision, TENANT, testQueryClient } from "./testSupport";

// The page through the same route shape as router.tsx, with the panel's
// routes stubbed: these tests are about the frame, the header and the list.

const eks = revision({ id: "rev_eks", source_template_id: "tpl_eks", name: "eks", root_path: "aws/eks" });
const network = revision({
  id: "rev_net",
  source_template_id: "tpl_net",
  name: "network",
  root_path: "aws/network",
  source_ref: "v2.3.0",
  description: "A VPC with public and private subnets.",
  tags: ["networking"]
});
const cdn = revision({ id: "rev_cdn", source_template_id: "tpl_cdn", repo_name: "edge", name: "cloudfront-site", root_path: ".", source_ref: "v2.0.1", tags: [] });

function seed(revisions: TemplateRevision[] = [eks, network, cdn]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), revisions);
  return queryClient;
}

function renderPage(queryClient: QueryClient, path: string, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [
      {
        path: "/templates",
        element: <TemplatesPage />,
        children: [
          { index: true, element: <p data-testid="index-content">index</p> },
          { path: "new", element: <p data-testid="register-content">register</p> },
          { path: ":sourceTemplateId/variables", element: <p data-testid="variables-content">variables</p> },
          { path: ":sourceTemplateId/revisions", element: <p data-testid="revisions-content">revisions</p> }
        ]
      }
    ],
    { initialEntries: [path] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
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

describe("TemplatesPage header", () => {
  it("titles the page and counts templates, not revisions", () => {
    renderPage(seed([revision({ id: "rev_eks_2", source_template_id: "tpl_eks" }), eks, network, cdn]), "/templates");

    expect(screen.getByRole("heading", { level: 1, name: "Templates" })).toBeTruthy();
    expect(screen.getByTestId("page-count").textContent).toBe("3");
  });

  it("offers Register template only to people who can publish templates", () => {
    renderPage(seed(), "/templates");
    expect(screen.getByTestId("register-template-link").getAttribute("href")).toBe("/templates/new");
    cleanup();

    renderPage(seed(), "/templates", false);
    expect(screen.queryByTestId("register-template-link")).toBeNull();
  });
});

describe("TemplatesPage list", () => {
  it("lists templates under their repository, each with its path and ref", () => {
    renderPage(seed(), "/templates");

    const infra = screen.getByTestId("template-group-acme/infra-modules");
    expect(within(infra).getByRole("heading", { name: "acme/infra-modules" })).toBeTruthy();
    expect(within(infra).getAllByRole("link")).toHaveLength(2);
    expect(screen.getByTestId("template-link-tpl_eks").textContent).toContain("aws/eks · main");
    // A module at the root shows its ref alone.
    expect(screen.getByTestId("template-link-tpl_cdn").textContent).toBe("cloudfront-sitev2.0.1");
  });

  it("opens on the first template without changing the URL", () => {
    const router = renderPage(seed(), "/templates");

    expect(screen.getByTestId("template-link-tpl_eks").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("template-link-tpl_net").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("index-content")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/templates");
  });

  it("selects the template in the URL", () => {
    renderPage(seed(), "/templates/tpl_net/revisions");

    expect(screen.getByTestId("template-link-tpl_net").getAttribute("aria-current")).toBe("true");
    expect(screen.getByTestId("template-link-tpl_eks").getAttribute("aria-current")).toBeNull();
    expect(screen.getByTestId("revisions-content")).toBeTruthy();
  });

  it.each([
    ["/templates", "/templates/tpl_net/variables"],
    ["/templates/tpl_eks/revisions", "/templates/tpl_net/revisions"],
    ["/templates/new", "/templates/tpl_net/variables"]
  ])("from %s, another template's row opens %s", (from, to) => {
    renderPage(seed(), from);
    expect(screen.getByTestId("template-link-tpl_net").getAttribute("href")).toBe(to);
  });

  it("selects nothing while registering", () => {
    renderPage(seed(), "/templates/new");
    expect(screen.getByTestId("register-content")).toBeTruthy();
    expect(screen.queryAllByRole("link", { current: true })).toHaveLength(0);
  });

  it("narrows the list as the person types, and says when nothing matches", () => {
    renderPage(seed(), "/templates");
    const filter = screen.getByRole("searchbox", { name: "Filter templates" });

    fireEvent.change(filter, { target: { value: "Networking" } });
    expect(screen.getByTestId("template-link-tpl_net")).toBeTruthy();
    expect(screen.queryByTestId("template-link-tpl_eks")).toBeNull();
    expect(screen.queryByTestId("template-group-acme/edge")).toBeNull();

    fireEvent.change(filter, { target: { value: "edge" } });
    expect(screen.getByTestId("template-link-tpl_cdn")).toBeTruthy();
    expect(screen.queryByTestId("template-link-tpl_net")).toBeNull();

    fireEvent.change(filter, { target: { value: "zzz" } });
    expect(screen.getByTestId("templates-filter-empty").textContent).toBe("No templates match this filter.");
  });

  it("says when there are no templates at all", () => {
    renderPage(seed([]), "/templates");
    expect(screen.getByTestId("templates-none").textContent).toBe("No templates yet.");
    expect(screen.getByTestId("page-count").textContent).toBe("0");
  });

  // Review focus 1: a template from before source ids is keyed on its
  // identity tuple, which is JSON, so its link must be encoded and must come
  // back to the same template.
  it("links and selects a template whose id is its identity tuple", () => {
    const legacy = revision({ id: "rev_old", source_template_id: "", repo_name: "legacy", root_path: "." });
    const id = JSON.stringify(["acme", "legacy", ".", "main"]);
    renderPage(seed([eks, legacy]), "/templates");

    const row = screen.getByTestId(`template-link-${id}`);
    expect(row.getAttribute("href")).toBe(`/templates/${encodeURIComponent(id)}/variables`);
    fireEvent.click(row);
    expect(screen.getByTestId(`template-link-${id}`).getAttribute("aria-current")).toBe("true");
  });
});

// On a phone the page shows the list or the panel, never both. It is CSS, so
// these read the classes that switch it.
describe("TemplatesPage on a phone", () => {
  it("shows only the list on /templates", () => {
    renderPage(seed(), "/templates");

    expect(screen.getByTestId("templates-list-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("templates-panel-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.queryByRole("link", { name: "Templates" })).toBeNull();
  });

  it("shows only the panel on a template's address, with a way back to the list", () => {
    renderPage(seed(), "/templates/tpl_eks/variables");

    expect(screen.getByTestId("templates-list-column").className).toMatch(/(^| )hidden( |$)/);
    expect(screen.getByTestId("templates-panel-column").className).not.toMatch(/(^| )hidden( |$)/);
    expect(screen.getByRole("link", { name: "Templates" }).getAttribute("href")).toBe("/templates");
  });
});

describe("TemplatesPage loading and errors", () => {
  it("says it is loading while the templates load", () => {
    renderPage(testQueryClient(), "/templates");
    expect(screen.getByTestId("templates-loading").textContent).toContain("Loading templates…");
  });

  it("offers a retry when the templates fail to load", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ error: "boom", message: "boom" }, 500));
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("templates-error")).toBeTruthy());
    expect(screen.getByTestId("templates-error").textContent).toContain("Something went wrong while loading templates.");
    expect(screen.getByTestId("templates-retry")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("hands a refused request to the shared boundary", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ error: "forbidden", message: "forbidden" }, 403));
    renderPage(testQueryClient(), "/templates");

    await waitFor(() => expect(screen.getByTestId("route-access-denied")).toBeTruthy());
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/templates/TemplatesPage.test.tsx`
Expected: FAIL, "Failed to resolve import ./TemplatesPage".

- [ ] **Step 3: Write the outlet context**

`web/src/features/templates/templatesPageOutlet.ts`:

```ts
import { useOutletContext } from "react-router-dom";

/** What the templates page hands the routes drawn into its panel. */
export interface TemplatesPageOutletContext {
  /** The template /templates itself shows: the first in the list, or none. */
  indexTemplateId: string | null;
}

export function useIndexTemplateId(): string | null {
  return useOutletContext<TemplatesPageOutletContext | undefined>()?.indexTemplateId ?? null;
}
```

- [ ] **Step 4: Write the list**

`web/src/features/templates/TemplateList.tsx`:

```tsx
import { useId, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import { cn } from "@/lib/utils";
import { templatePath, templateTabOf } from "./templateRoutes";
import { matchesTemplateFilter, templateRootPathLabel } from "./templateWorkflow";
import type { SourceTemplateGroup, TemplateRepositoryGroup } from "./templateWorkflow";

// The left side of /templates: every template under the repository it comes
// from, narrowed as the person types. A row links to the same tab of its
// template, and pushes a history entry: a template is a working page, and on
// a phone its own screen, so Back returns to the list.
export default function TemplateList({ groups, selectedId }: { groups: TemplateRepositoryGroup[]; selectedId: string | null }) {
  const [filter, setFilter] = useState("");
  const headingId = useId();
  const tab = templateTabOf(useLocation().pathname);
  const visible = groups
    .map((group) => ({ ...group, sourceTemplates: group.sourceTemplates.filter((template) => matchesTemplateFilter(template, filter)) }))
    .filter((group) => group.sourceTemplates.length > 0);

  return (
    <div className="flex min-w-0 flex-col">
      <div className="border-b border-divider p-3">
        <SearchField label="Filter templates" value={filter} onChange={setFilter} testId="templates-filter" />
      </div>
      <div className="flex flex-col gap-0.5 p-2">
        {groups.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="templates-none">
            No templates yet.
          </p>
        ) : visible.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="templates-filter-empty">
            No templates match this filter.
          </p>
        ) : (
          visible.map((group, index) => (
            <section
              key={group.key}
              aria-labelledby={`${headingId}-${index}`}
              className="flex flex-col gap-0.5"
              data-testid={`template-group-${group.key}`}
            >
              <h2 id={`${headingId}-${index}`} className="px-3 pt-2.5 pb-1 font-mono text-xs font-normal text-muted-foreground wrap-anywhere">
                {group.key}
              </h2>
              <ul className="flex flex-col gap-0.5">
                {group.sourceTemplates.map((template) => (
                  <li key={template.sourceTemplateID}>
                    <TemplateRow
                      template={template}
                      to={templatePath(template.sourceTemplateID, tab)}
                      selected={template.sourceTemplateID === selectedId}
                    />
                  </li>
                ))}
              </ul>
            </section>
          ))
        )}
      </div>
    </div>
  );
}

// One template: its name, then where it lives in the repository and the ref
// it tracks. The path is left out for a module at the root, or when it is
// already the name.
function TemplateRow({ template, to, selected }: { template: SourceTemplateGroup; to: string; selected: boolean }) {
  const path = templateRootPathLabel(template.rootPath, template.name);
  const where = path === "" ? template.sourceRef : `${path} · ${template.sourceRef}`;

  return (
    <Link to={to} aria-current={selected ? "true" : undefined} className={listItemClass(selected)} data-testid={`template-link-${template.sourceTemplateID}`}>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate text-sm leading-label font-medium", selected && "text-primary-strong")} title={template.name}>
          {template.name}
        </span>
        <span className="truncate font-mono text-xs text-muted-foreground" title={where}>
          {where}
        </span>
      </span>
    </Link>
  );
}
```

- [ ] **Step 5: Write the page**

`web/src/features/templates/TemplatesPage.tsx`:

```tsx
import { ArrowLeft, Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, Outlet, useMatch } from "react-router-dom";
import { useTemplateRevisionsQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { cn } from "@/lib/utils";
import TemplateList from "./TemplateList";
import type { TemplatesPageOutletContext } from "./templatesPageOutlet";
import { groupTemplatesByRepository } from "./templateWorkflow";

const registerLinkClass = cn(buttonClass("primary", "lg"), "w-full pointer-coarse:h-11 md:w-auto");

// /templates: every registered template on the left, grouped by repository;
// on the right, the selected template's panel, which is the route below
// rendered into <Outlet />. With no template in the URL, the index draws the
// first one without changing the URL.
//
// On a phone the page shows the list or the panel, never both: the list on
// /templates, the panel on a template's address or /templates/new, with a
// link back. The frame mirrors StackPage's classes; the two pages differ in
// their guards, ids and outlet contexts, so they share the look, not a
// component.
export default function TemplatesPage() {
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);
  const templateMatch = useMatch("/templates/:sourceTemplateId/*");

  if (templateRevisionsQuery.status === "pending") {
    return (
      <section data-testid="templates-loading">
        <p className="flex items-center gap-2 text-meta text-muted-foreground" role="status">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading templates…
        </p>
      </section>
    );
  }

  if (templateRevisionsQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section data-testid="templates-error">
        <PageHeader title="Templates" />
        <div className="flex flex-col items-start gap-3">
          <ErrorLine live={false}>Something went wrong while loading templates.</ErrorLine>
          <button
            type="button"
            className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
            data-testid="templates-retry"
            onClick={() => void templateRevisionsQuery.refetch()}
          >
            <RefreshCw data-icon="inline-start" aria-hidden="true" />
            Retry
          </button>
        </div>
      </section>
    );
  }

  const groups = groupTemplatesByRepository(templateRevisionsQuery.data);
  const count = groups.reduce((total, group) => total + group.sourceTemplates.length, 0);
  const first = groups[0]?.sourceTemplates[0] ?? null;
  const onPanel = templateMatch !== null;
  const registering = templateMatch?.params.sourceTemplateId === "new";
  const selectedId = onPanel ? (registering ? null : templateMatch.params.sourceTemplateId ?? null) : first?.sourceTemplateID ?? null;
  const outletContext: TemplatesPageOutletContext = { indexTemplateId: first?.sourceTemplateID ?? null };

  return (
    <section data-testid="templates-page">
      <PageHeader
        title="Templates"
        count={count}
        action={
          <RequireCapability capability="canPublishTemplate">
            <Link to="/templates/new" className={registerLinkClass} data-testid="register-template-link">
              <Plus data-icon="inline-start" aria-hidden="true" />
              Register template
            </Link>
          </RequireCapability>
        }
      />
      <div className="flex flex-col overflow-clip rounded-panel border bg-card md:flex-row">
        <div className={cn("min-w-0 flex-col md:flex md:w-90 md:shrink-0 md:border-r", onPanel ? "hidden" : "flex")} data-testid="templates-list-column">
          <TemplateList groups={groups} selectedId={selectedId} />
        </div>
        <div className={cn("min-w-0 flex-1 flex-col md:flex", onPanel ? "flex" : "hidden")} data-testid="templates-panel-column">
          {onPanel && (
            <Link
              to="/templates"
              className="flex min-h-11 items-center gap-1.5 self-start px-7 pt-4 text-meta font-medium text-primary hover:underline md:hidden"
            >
              <ArrowLeft aria-hidden="true" className="size-3.5" />
              Templates
            </Link>
          )}
          <Outlet context={outletContext} />
        </div>
      </div>
    </section>
  );
}
```

- [ ] **Step 6: Note the mirror in StackPage**

In `web/src/features/stacks/StackPage.tsx`, add one line to the end of the comment above `export default function StackPage()`:

```tsx
// TemplatesPage mirrors this list-and-panel frame and its phone rules.
```

- [ ] **Step 7: Run the tests to see them pass**

Run: `npx vitest run src/features/templates/TemplatesPage.test.tsx`
Expected: PASS, 17 tests.

- [ ] **Step 8: Commit**

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

- [ ] **Step 1: Write the failing tests**

`web/src/features/templates/TemplatePanel.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRevision } from "../../api/types";
import TemplatePanel from "./TemplatePanel";
import { authValue, revision, TENANT, testQueryClient, variable } from "./testSupport";

const latest = revision({ id: "rev_2", resolved_commit_sha: "9e7d3b2aaaabbbbccccddddeeeeffff000011112", created_at: "2026-10-05T09:30:00Z" });
const older = revision({ id: "rev_1" });
const rds = revision({ id: "rev_rds", source_template_id: "tpl_rds", name: "rds", root_path: "aws/rds", description: "", tags: [] });
const cdn = revision({ id: "rev_cdn", source_template_id: "tpl_cdn", repo_name: "edge", name: "cloudfront-site", root_path: ".", source_ref: "v2.0.1" });

function seed(revisions: TemplateRevision[] = [latest, older, rds, cdn]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), revisions);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_2"), [
    variable({ template_revision_id: "rev_2" }),
    variable({ template_revision_id: "rev_2", name: "node_desired_size", type_expression: "number", required: false, has_default: true })
  ]);
  return queryClient;
}

function renderPanel(queryClient: QueryClient, path: string, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [
      {
        path: "/templates/:sourceTemplateId",
        element: <TemplatePanel />,
        children: [
          { path: "variables", element: <p data-testid="variables-content">variables</p> },
          { path: "revisions", element: <p data-testid="revisions-content">revisions</p> }
        ]
      }
    ],
    { initialEntries: [path] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplatePanel header", () => {
  it("names the template and shows its latest revision's description and tags", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");

    expect(screen.getByRole("heading", { level: 2, name: "eks" })).toBeTruthy();
    expect(screen.getByTestId("template-description").textContent).toBe("An EKS cluster with one managed node group.");
    expect(screen.getByText("aws")).toBeTruthy();
    expect(screen.getByText("kubernetes")).toBeTruthy();
  });

  it("tells publishers, and only publishers, how to add a description", () => {
    renderPanel(seed(), "/templates/tpl_rds/variables");
    expect(screen.getByTestId("template-no-description").textContent).toContain("template.yaml");
    expect(screen.queryByTestId("template-description")).toBeNull();
    cleanup();

    renderPanel(seed(), "/templates/tpl_rds/variables", false);
    expect(screen.queryByTestId("template-no-description")).toBeNull();
  });

  it("links the repository, root path, ref and latest commit on GitHub, with the commit's registration date", () => {
    renderPanel(seed(), "/templates/tpl_1/variables");
    const details = within(screen.getByTestId("template-details"));

    expect(details.getByRole("link", { name: /^acme\/infra-modules,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules");
    expect(details.getByRole("link", { name: /^aws\/eks,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules/tree/main/aws/eks");
    expect(details.getByRole("link", { name: /^main,/ }).getAttribute("href")).toBe("https://github.com/acme/infra-modules/tree/main");
    const commit = details.getByRole("link", { name: /^9e7d3b2,/ });
    expect(commit.getAttribute("href")).toBe("https://github.com/acme/infra-modules/commit/9e7d3b2aaaabbbbccccddddeeeeffff000011112");
    expect(screen.getByTestId("template-details").textContent).toMatch(/5 Oct(ober)? 2026/);
    for (const link of details.getAllByRole("link")) {
      expect(link.getAttribute("target")).toBe("_blank");
    }
  });

  it("calls a module at the root the repository root", () => {
    renderPanel(seed(), "/templates/tpl_cdn/variables");
    const root = within(screen.getByTestId("template-details")).getByRole("link", { name: /^the repository root,/ });
    expect(root.getAttribute("href")).toBe("https://github.com/acme/edge/tree/v2.0.1");
  });

  it("says so when the URL names no registered template", () => {
    renderPanel(seed(), "/templates/gone/variables");
    expect(screen.getByTestId("template-missing").textContent).toBe("That template is not registered.");
  });
});

describe("TemplatePanel tabs", () => {
  it("marks the current tab and counts the variables and revisions", () => {
    renderPanel(seed(), "/templates/tpl_1/revisions");
    const nav = within(screen.getByRole("navigation", { name: "Template sections" }));

    const variables = nav.getByRole("link", { name: "Variables 2" });
    expect(variables.getAttribute("href")).toBe("/templates/tpl_1/variables");
    expect(variables.getAttribute("aria-current")).toBeNull();

    const revisions = nav.getByRole("link", { name: "Revisions 2" });
    expect(revisions.getAttribute("href")).toBe("/templates/tpl_1/revisions");
    expect(revisions.getAttribute("aria-current")).toBe("page");
    expect(screen.getByTestId("revisions-content")).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/templates/TemplatePanel.test.tsx`
Expected: FAIL, "Failed to resolve import ./TemplatePanel".

- [ ] **Step 3: Write the context**

`web/src/features/templates/templateContext.ts`:

```ts
import { createContext, useContext } from "react";
import type { TemplateRevision } from "../../api/types";

export interface TemplateContextValue {
  sourceTemplateId: string;
  /** Every revision of the template, newest first. */
  revisions: TemplateRevision[];
  /** The newest revision, which names and describes the template. */
  latest: TemplateRevision;
}

// The template a panel is about, for the tabs and controls inside it. A React
// context rather than the outlet's: /templates itself draws the first
// template's Variables tab without a route of its own.
export const TemplateContext = createContext<TemplateContextValue | null>(null);

export function useTemplate(): TemplateContextValue {
  const value = useContext(TemplateContext);
  if (value === null) {
    throw new Error("useTemplate must be used inside a TemplatePanel");
  }
  return value;
}
```

- [ ] **Step 4: Write the panel**

`web/src/features/templates/TemplatePanel.tsx`:

```tsx
import type { ReactNode } from "react";
import { Outlet, useLocation, useParams } from "react-router-dom";
import { useTemplateRevisionsQuery, useTemplateRevisionVariablesQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import ExternalLink from "../../shared/ExternalLink";
import { formatTimestamp } from "../../shared/formatTimestamp";
import UnderlineTabs from "../../shared/UnderlineTabs";
import { TemplateContext } from "./templateContext";
import { githubLinks, rootPathOf } from "./templateLinks";
import { templatePath, templateTabOf } from "./templateRoutes";
import { revisionsForSourceTemplate, shortCommitSHA, templateDisplayName } from "./templateWorkflow";

// The right side of /templates: one template, read from its newest revision.
// Its name, what it is for, where it lives on GitHub, then its Variables and
// Revisions tabs. The route below draws the tab into <Outlet />, except on
// /templates itself, which passes the first template's Variables tab as
// children.
//
// TemplatesPage has loaded the revisions before any panel renders, so the
// template is read from cache. The content is keyed on the template, so a
// tab's local state never carries over to another.
export default function TemplatePanel({ sourceTemplateId, children }: { sourceTemplateId?: string; children?: ReactNode }) {
  const params = useParams<{ sourceTemplateId: string }>();
  const id = sourceTemplateId ?? params.sourceTemplateId ?? "";
  const revisions = revisionsForSourceTemplate(useTemplateRevisionsQuery(tenantID).data ?? [], id);
  const latest = revisions[0];
  const variableCount = useTemplateRevisionVariablesQuery(tenantID, latest?.id ?? "").data?.length;
  const tab = templateTabOf(useLocation().pathname);

  if (!latest) {
    return (
      <p className="px-7 py-6 text-meta text-muted-foreground" data-testid="template-missing">
        That template is not registered.
      </p>
    );
  }

  const description = latest.description.trim();
  const links = githubLinks(latest);
  const rootPath = rootPathOf(latest.root_path);

  return (
    <TemplateContext.Provider value={{ sourceTemplateId: id, revisions, latest }}>
      <div className="flex min-w-0 flex-col" data-testid="template-panel">
        <div className="flex flex-col gap-5 border-b border-divider px-7 pt-6">
          <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
            <div className="flex min-w-0 flex-1 basis-80 flex-col gap-3">
              <h2 className="font-heading text-panel-title font-semibold tracking-title wrap-anywhere">{templateDisplayName(latest)}</h2>
              {description !== "" ? (
                <p className="max-w-170 text-sm text-foreground" data-testid="template-description">
                  {description}
                </p>
              ) : (
                <RequireCapability capability="canPublishTemplate">
                  <p className="max-w-170 text-meta text-muted-foreground" data-testid="template-no-description">
                    No description. Add a <span className="font-mono text-code-foreground">template.yaml</span> beside the module with a name, a
                    description and tags.
                  </p>
                </RequireCapability>
              )}
              {latest.tags.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {latest.tags.map((tag) => (
                    <span key={tag} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-xs text-tag-foreground">
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>
            <div data-testid="template-sync-slot" />
          </div>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4 rounded-lg border px-5 py-4 lg:grid-cols-4" data-testid="template-details">
            <Detail term="Repository">
              <ExternalLink href={links.repository} site="GitHub">
                {latest.repo_owner}/{latest.repo_name}
              </ExternalLink>
            </Detail>
            <Detail term="Root path">
              <ExternalLink href={links.tree} site="GitHub" mono={rootPath !== null}>
                {rootPath ?? "the repository root"}
              </ExternalLink>
            </Detail>
            <Detail term="Ref">
              <ExternalLink href={links.ref} site="GitHub">
                {latest.source_ref}
              </ExternalLink>
            </Detail>
            <Detail term="Latest commit">
              <ExternalLink href={links.commit} site="GitHub">
                {shortCommitSHA(latest.resolved_commit_sha)}
              </ExternalLink>
              <span aria-hidden="true" className="text-separator">
                ·
              </span>
              <time dateTime={latest.created_at} title={latest.created_at}>
                {formatTimestamp(latest.created_at)}
              </time>
            </Detail>
          </dl>
          <UnderlineTabs
            label="Template sections"
            tabs={[
              { to: templatePath(id, "variables"), label: "Variables", current: tab === "variables", count: variableCount },
              { to: templatePath(id, "revisions"), label: "Revisions", current: tab === "revisions", count: revisions.length }
            ]}
          />
        </div>
        <div key={id} className="flex min-w-0 flex-col gap-3 px-7 pt-5 pb-7">
          {children ?? <Outlet />}
        </div>
      </div>
    </TemplateContext.Provider>
  );
}

// One fact about the template: its term over its value, as openplan UI's
// DetailList draws them.
function Detail({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className="flex min-w-0 flex-wrap items-center gap-1.5 text-meta text-muted-foreground">{children}</dd>
    </div>
  );
}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `npx vitest run src/features/templates/TemplatePanel.test.tsx`
Expected: PASS, 6 tests.

- [ ] **Step 6: Commit**

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

- [ ] **Step 1: Write the failing Variables tests**

`web/src/features/templates/VariablesTab.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateVariable } from "../../api/types";
import TemplatePanel from "./TemplatePanel";
import VariablesTab from "./VariablesTab";
import { authValue, jsonResponse, revision, TENANT, testQueryClient, variable } from "./testSupport";

function seed(variables?: TemplateVariable[]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [revision()]);
  if (variables) {
    queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), variables);
  }
  return queryClient;
}

function renderTab(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [{ path: "/templates/:sourceTemplateId", element: <TemplatePanel />, children: [{ path: "variables", element: <VariablesTab /> }] }],
    { initialEntries: ["/templates/tpl_1/variables"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("VariablesTab", () => {
  it("lists the latest revision's variables: name, type, required or optional, description", () => {
    renderTab(
      seed([
        variable(),
        variable({ name: "subnet_ids", type_expression: "list(string)", description: "Private subnets." }),
        variable({ name: "node_desired_size", type_expression: "number", required: false, has_default: true, description: "Nodes." }),
        variable({ name: "tags", type_expression: "", required: false, has_default: true, description: "" })
      ])
    );

    expect(screen.getByTestId("template-variables").textContent).toContain("From the latest revision, 3f9c2a1.");
    const required = within(screen.getByTestId("template-variable-cluster_name"));
    expect(required.getByText("cluster_name")).toBeTruthy();
    expect(required.getByText("string")).toBeTruthy();
    expect(required.getByText("required").className).toContain("font-medium");
    expect(within(screen.getByTestId("template-variable-subnet_ids")).getByText("list(string)")).toBeTruthy();
    expect(within(screen.getByTestId("template-variable-node_desired_size")).getByText("optional")).toBeTruthy();
    // A variable with no type takes any value, as in Terraform.
    expect(within(screen.getByTestId("template-variable-tags")).getByText("any")).toBeTruthy();
    expect(screen.getByRole("table").querySelector("colgroup")).not.toBeNull();
  });

  it("says when the template takes no variables", () => {
    renderTab(seed([]));
    expect(screen.getByTestId("template-variables-none").textContent).toBe("This template takes no variables.");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("says it is loading while the variables load", () => {
    renderTab(seed());
    expect(screen.getByTestId("template-variables-loading").textContent).toContain("Loading variables…");
  });

  it("offers a retry when the variables fail to load", async () => {
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse({ error: "boom", message: "boom" }, 500));
    renderTab(seed());

    await waitFor(() => expect(screen.getByTestId("template-variables-error")).toBeTruthy());
    vi.mocked(globalThis.fetch).mockImplementation(async () => jsonResponse([variable()]));
    fireEvent.click(screen.getByTestId("template-variables-retry"));
    await waitFor(() => expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy());
  });

  it("follows the newest revision when a sync registers one", async () => {
    const queryClient = seed([variable()]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_2"), [variable({ template_revision_id: "rev_2", name: "cluster_version" })]);
    renderTab(queryClient);
    expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy();

    act(() => {
      queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [
        revision({ id: "rev_2", resolved_commit_sha: "9e7d3b2aaaa" }),
        revision()
      ]);
    });
    await waitFor(() => expect(screen.getByTestId("template-variable-cluster_version")).toBeTruthy());
    expect(screen.getByTestId("template-variables").textContent).toContain("From the latest revision, 9e7d3b2.");
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/templates/VariablesTab.test.tsx`
Expected: FAIL, "Failed to resolve import ./VariablesTab".

- [ ] **Step 3: Write the Variables tab**

`web/src/features/templates/VariablesTab.tsx`:

```tsx
import { Loader2, RefreshCw } from "lucide-react";
import { useTemplateRevisionVariablesQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { useTemplate } from "./templateContext";
import { shortCommitSHA } from "./templateWorkflow";

// 8px either side of a column boundary makes the 16px between columns, and
// 16px at the row's ends. Name 200, type 120 and value 90, plus that padding.
const cellClass = "px-2 py-2 first:pl-4 last:pr-4";

// /templates/:id/variables: what the template asks for, read from its newest
// revision. The API says whether a variable has a default, not what it is, so
// a variable is required or optional.
export default function VariablesTab() {
  const { latest } = useTemplate();
  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, latest.id);
  const boundary = useQueryErrorBoundary(variablesQuery.error);

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

  const variables = variablesQuery.data;
  if (variables.length === 0) {
    return (
      <p className="text-meta text-muted-foreground" data-testid="template-variables-none">
        This template takes no variables.
      </p>
    );
  }

  return (
    <section className="flex min-w-0 flex-col gap-3" data-testid="template-variables">
      <p className="text-meta text-muted-foreground">
        From the latest revision, <span className="font-mono text-code-foreground">{shortCommitSHA(latest.resolved_commit_sha)}</span>. A required
        variable needs a value when the template is added to a stack.
      </p>
      {/* Fixed columns in a frame that scrolls sideways on a phone. */}
      <div className="overflow-x-auto rounded-lg border">
        <Table className="min-w-2xl">
          <colgroup>
            <col className="w-56" />
            <col className="w-34" />
            <col className="w-26" />
            <col />
          </colgroup>
          <TableHeader>
            <TableRow className="border-divider hover:bg-transparent">
              {["Name", "Type", "Value", "Description"].map((heading) => (
                <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-xs font-medium text-muted-foreground")}>
                  {heading}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {variables.map((templateVariable) => (
              <TableRow key={templateVariable.name} className="h-12 border-divider hover:bg-transparent" data-testid={`template-variable-${templateVariable.name}`}>
                <TableCell className={cn(cellClass, "font-mono text-meta whitespace-normal wrap-anywhere")}>{templateVariable.name}</TableCell>
                <TableCell className={cn(cellClass, "font-mono text-xs whitespace-normal text-muted-foreground wrap-anywhere")}>
                  {templateVariable.type_expression || "any"}
                </TableCell>
                <TableCell className={cn(cellClass, "text-meta", templateVariable.required ? "font-medium text-foreground" : "text-muted-foreground")}>
                  {templateVariable.required ? "required" : "optional"}
                </TableCell>
                <TableCell className={cn(cellClass, "text-meta whitespace-normal text-muted-foreground")}>{templateVariable.description}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
```

- [ ] **Step 4: Run the Variables tests to see them pass**

Run: `npx vitest run src/features/templates/VariablesTab.test.tsx`
Expected: PASS, 5 tests.

- [ ] **Step 5: Write the failing index tests**

`web/src/features/templates/TemplatesIndexPanel.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import type { TemplateRevision } from "../../api/types";
import TemplatesIndexPanel from "./TemplatesIndexPanel";
import TemplatesPage from "./TemplatesPage";
import { authValue, revision, TENANT, testQueryClient, variable } from "./testSupport";

function seed(revisions: TemplateRevision[]): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), revisions);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), [variable()]);
  return queryClient;
}

function renderIndex(queryClient: QueryClient, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [{ path: "/templates", element: <TemplatesPage />, children: [{ index: true, element: <TemplatesIndexPanel /> }] }],
    { initialEntries: ["/templates"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplatesIndexPanel", () => {
  it("shows the first template on its Variables tab", () => {
    renderIndex(seed([revision(), revision({ id: "rev_net", source_template_id: "tpl_net", name: "network" })]));

    expect(screen.getByRole("heading", { level: 2, name: "eks" })).toBeTruthy();
    expect(screen.getByTestId("template-variable-cluster_name")).toBeTruthy();
    const nav = within(screen.getByRole("navigation", { name: "Template sections" }));
    expect(nav.getByRole("link", { name: /^Variables/ }).getAttribute("aria-current")).toBe("page");
  });

  it("invites a publisher to register the first template", () => {
    renderIndex(seed([]));

    const empty = screen.getByTestId("templates-empty");
    expect(within(empty).getByRole("heading", { name: "No templates yet" })).toBeTruthy();
    expect(empty.textContent).toContain("Register a Terraform module from a Git repository to make it available to your stacks.");
    expect(within(empty).getByRole("link", { name: "Register template" }).getAttribute("href")).toBe("/templates/new");
  });

  it("tells everyone else who registers templates", () => {
    renderIndex(seed([]), false);

    const empty = screen.getByTestId("templates-empty");
    expect(empty.textContent).toContain("Templates appear here once someone who can publish registers one.");
    expect(within(empty).queryByRole("link")).toBeNull();
  });
});
```

- [ ] **Step 6: Run them to see them fail**

Run: `npx vitest run src/features/templates/TemplatesIndexPanel.test.tsx`
Expected: FAIL, "Failed to resolve import ./TemplatesIndexPanel".

- [ ] **Step 7: Write the index panel**

`web/src/features/templates/TemplatesIndexPanel.tsx`:

```tsx
import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { cn } from "@/lib/utils";
import TemplatePanel from "./TemplatePanel";
import { useIndexTemplateId } from "./templatesPageOutlet";
import VariablesTab from "./VariablesTab";

// /templates itself: the first template's panel on its Variables tab, drawn
// here rather than redirected to, so the URL stays /templates and a phone can
// show the list at it. With no templates, openplan UI's EmptyState says what
// to do, or who does it.
export default function TemplatesIndexPanel() {
  const selectedId = useIndexTemplateId();

  if (!selectedId) {
    return (
      <div className="p-7">
        <Empty className="gap-3 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="templates-empty">
          <EmptyHeader className="gap-3">
            <h2 className="text-sm font-medium">No templates yet</h2>
            <RequireCapability
              capability="canPublishTemplate"
              fallback={<EmptyDescription className="text-meta">Templates appear here once someone who can publish registers one.</EmptyDescription>}
            >
              <EmptyDescription className="text-meta">Register a Terraform module from a Git repository to make it available to your stacks.</EmptyDescription>
            </RequireCapability>
          </EmptyHeader>
          <RequireCapability capability="canPublishTemplate">
            <Link to="/templates/new" className={cn(buttonClass("primary"), "pointer-coarse:h-11")}>
              <Plus data-icon="inline-start" aria-hidden="true" />
              Register template
            </Link>
          </RequireCapability>
        </Empty>
      </div>
    );
  }

  return (
    <TemplatePanel sourceTemplateId={selectedId}>
      <VariablesTab />
    </TemplatePanel>
  );
}
```

- [ ] **Step 8: Run both files to see them pass**

Run: `npx vitest run src/features/templates/VariablesTab.test.tsx src/features/templates/TemplatesIndexPanel.test.tsx`
Expected: PASS, 8 tests.

- [ ] **Step 9: Commit**

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

- [ ] **Step 1: Write the failing tests**

`web/src/features/templates/RevisionsTab.test.tsx`:

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import RevisionsTab from "./RevisionsTab";
import TemplatePanel from "./TemplatePanel";
import { authValue, revision, TENANT, testQueryClient } from "./testSupport";

const newest = revision({ id: "rev_3", resolved_commit_sha: "e3a7c4d000", created_at: "2026-09-30T10:00:00Z" });
const middle = revision({ id: "rev_2", resolved_commit_sha: "b51f08a000", created_at: "2026-09-14T10:00:00Z", status: "validating" });
const oldest = revision({ id: "rev_1", resolved_commit_sha: "0c9d2e6000", created_at: "2026-08-28T10:00:00Z" });

function seed(): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [newest, middle, oldest]);
  return queryClient;
}

function renderTab(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [{ path: "/templates/:sourceTemplateId", element: <TemplatePanel />, children: [{ path: "revisions", element: <RevisionsTab /> }] }],
    { initialEntries: ["/templates/tpl_1/revisions"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue()}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("RevisionsTab", () => {
  it("lists every commit registered from the ref, newest first, the newest marked latest", () => {
    renderTab(seed());

    expect(screen.getByTestId("template-revisions").textContent).toContain("Every commit registered from main, newest first.");
    const rows = screen.getAllByTestId(/^revision-row-/);
    expect(rows.map((row) => row.getAttribute("data-testid"))).toEqual(["revision-row-rev_3", "revision-row-rev_2", "revision-row-rev_1"]);
    expect(screen.getAllByTestId("revision-latest")).toHaveLength(1);
    expect(within(rows[0]).getByTestId("revision-latest").textContent).toBe("latest");
    expect(rows[2].textContent).toMatch(/28 Aug(ust)? 2026/);
    expect(screen.getByRole("table").querySelector("colgroup")).not.toBeNull();
  });

  it("links each commit on GitHub", () => {
    renderTab(seed());
    const link = within(screen.getByTestId("revision-row-rev_2")).getByRole("link", { name: /^b51f08a,/ });
    expect(link.getAttribute("href")).toBe("https://github.com/acme/infra-modules/commit/b51f08a000");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("says nothing about an active revision, and names any other state", () => {
    renderTab(seed());
    expect(within(screen.getByTestId("revision-row-rev_2")).getByText("validating")).toBeTruthy();
    expect(within(screen.getByTestId("revision-row-rev_1")).queryByText(/active|validating|failed/)).toBeNull();
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/templates/RevisionsTab.test.tsx`
Expected: FAIL, "Failed to resolve import ./RevisionsTab".

- [ ] **Step 3: Write the Revisions tab**

`web/src/features/templates/RevisionsTab.tsx`:

```tsx
import ExternalLink from "../../shared/ExternalLink";
import { formatTimestamp } from "../../shared/formatTimestamp";
import StatusLabel from "../../shared/StatusLabel";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { revisionIndicator } from "./revisionIndicator";
import { useTemplate } from "./templateContext";
import { githubLinks } from "./templateLinks";
import { shortCommitSHA } from "./templateWorkflow";

const cellClass = "px-2 py-2 first:pl-4 last:pr-4";

// /templates/:id/revisions: every commit registered from the template's ref,
// in the API's order, newest first. Each commit links to GitHub. An active
// revision needs no word; any other state says what it is.
export default function RevisionsTab() {
  const { revisions, latest } = useTemplate();

  return (
    <section className="flex min-w-0 flex-col gap-3" data-testid="template-revisions">
      <p className="text-meta text-muted-foreground">
        Every commit registered from <span className="font-mono text-code-foreground">{latest.source_ref}</span>, newest first. A stack keeps the
        revision it was added with until someone changes it.
      </p>
      <div className="overflow-x-auto rounded-lg border">
        <Table className="min-w-md">
          <colgroup>
            <col className="w-56" />
            <col />
          </colgroup>
          <TableHeader>
            <TableRow className="border-divider hover:bg-transparent">
              {["Commit", "Registered"].map((heading) => (
                <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-xs font-medium text-muted-foreground")}>
                  {heading}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((templateRevision, index) => {
              const indicator = revisionIndicator(templateRevision.status);
              return (
                <TableRow key={templateRevision.id} className="h-12 border-divider hover:bg-transparent" data-testid={`revision-row-${templateRevision.id}`}>
                  <TableCell className={cellClass}>
                    <span className="flex flex-wrap items-center gap-2">
                      <ExternalLink href={githubLinks(templateRevision).commit} site="GitHub">
                        {shortCommitSHA(templateRevision.resolved_commit_sha)}
                      </ExternalLink>
                      {index === 0 && (
                        <span className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-xs text-tag-foreground" data-testid="revision-latest">
                          latest
                        </span>
                      )}
                      {indicator && (
                        <StatusLabel icon={indicator.icon} tone={indicator.tone} strong={indicator.strong}>
                          {indicator.label}
                        </StatusLabel>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className={cn(cellClass, "text-meta text-muted-foreground")}>
                    <time dateTime={templateRevision.created_at} title={templateRevision.created_at}>
                      {formatTimestamp(templateRevision.created_at)}
                    </time>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `npx vitest run src/features/templates/RevisionsTab.test.tsx`
Expected: PASS, 3 tests.

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Write the failing tests**

`web/src/features/templates/TemplateSync.test.tsx` (the behaviour tests of `TemplateDetailScreen.test.tsx`, moved, plus the new result line and Review focus 2):

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import RevisionsTab from "./RevisionsTab";
import TemplatePanel from "./TemplatePanel";
import { authValue, jsonResponse, registration, revision, TENANT, testQueryClient } from "./testSupport";

const current = revision();
const other = revision({ id: "rev_net", source_template_id: "tpl_net", name: "network", root_path: "aws/network" });

function seed(): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [current, other]);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_1"), []);
  queryClient.setQueryData(queryKeys.templateRevisionVariables(TENANT, "rev_net"), []);
  return queryClient;
}

function renderPanel(queryClient: QueryClient, canPublishTemplate = true) {
  const router = createMemoryRouter(
    [{ path: "/templates/:sourceTemplateId", element: <TemplatePanel />, children: [{ path: "revisions", element: <RevisionsTab /> }] }],
    { initialEntries: ["/templates/tpl_1/revisions"] }
  );
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue(canPublishTemplate)}>
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </QueryClientProvider>
  );
  return router;
}

function syncButton(): HTMLButtonElement {
  return screen.getByTestId("template-sync") as HTMLButtonElement;
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("TemplateSync", () => {
  it("re-registers exactly the template's identity", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse(registration());
      return jsonResponse([current, other]);
    });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(fetchMock.mock.calls.some(([, init]) => init?.method === "POST")).toBe(true));
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(String(post?.[0])).toContain("/v1/tenants/tenant_123/template-revisions");
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ repo_owner: "acme", repo_name: "infra-modules", source_ref: "main", root_path: "aws/eks" });
  });

  it("disables Sync while one runs, so clicks do not queue workflows", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse(registration({ status: "running" }));
      return jsonResponse([current, other]);
    });
    renderPanel(seed());
    expect(syncButton().disabled).toBe(false);

    fireEvent.click(syncButton());
    await waitFor(() => expect(syncButton().disabled).toBe(true));
    fireEvent.click(syncButton());
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === "POST")).toHaveLength(1);
  });

  it("names the new commit, which becomes the latest", async () => {
    const fresh = revision({ id: "rev_2", resolved_commit_sha: "f17f983444455556666" });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) {
        return jsonResponse(registration({ template_revision_id: "rev_2", resolved_commit_sha: "f17f983444455556666" }));
      }
      return jsonResponse([fresh, current, other]);
    });
    renderPanel(seed());
    fireEvent.click(syncButton());

    // The real flow: a pending POST, then a poll POLL_INTERVAL_MS later.
    await waitFor(() => expect(screen.getByTestId("revision-row-rev_2")).toBeTruthy(), { timeout: 3000 });
    expect(screen.getByTestId("template-sync-result").textContent).toBe("Registered commit f17f983.");
    expect(screen.getAllByTestId(/^revision-row-/)[0].getAttribute("data-testid")).toBe("revision-row-rev_2");
  });

  it("says the template is already up to date when the ref still names a known commit", async () => {
    const unchanged = registration({ template_revision_id: "rev_1" });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      init?.method === "POST" || String(input).includes("/template-registrations/") ? jsonResponse(unchanged) : jsonResponse([current, other])
    );
    renderPanel(seed());
    expect(screen.getByTestId("template-sync-result").textContent).toBe("");

    fireEvent.click(syncButton());
    await waitFor(() => expect(screen.getByTestId("template-sync-result").textContent).toBe("Already up to date."));
  });

  it.each([
    [registration({ status: "failed", template_revision_id: "", error_summary: "ref not found: main" }), "ref not found: main"],
    [registration({ status: "invalid", template_revision_id: "", error_summary: "" }), "Sync failed"]
  ])("shows why a sync failed, and offers Sync again", async (failed, message) => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      init?.method === "POST" || String(input).includes("/template-registrations/") ? jsonResponse(failed) : jsonResponse([current, other])
    );
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain(message));
    expect(syncButton().disabled).toBe(false);
  });

  it("shows a refused request rather than leaving the button spinning", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse({ error: "forbidden", message: "forbidden" }, 403) : jsonResponse([current, other])
    );
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain("forbidden"));
    expect(syncButton().disabled).toBe(false);
  });

  it("shows a failing poll rather than leaving the button spinning", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending", template_revision_id: "" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse({ error: "forbidden", message: "forbidden" }, 403);
      return jsonResponse([current, other]);
    });
    renderPanel(seed());
    fireEvent.click(syncButton());

    await waitFor(() => expect(screen.getByTestId("template-sync-error").textContent).toContain("forbidden"), { timeout: 3000 });
    expect(syncButton().disabled).toBe(false);
  });

  it("is not offered to people who cannot publish templates", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed(), false);
    expect(screen.queryByTestId("template-sync")).toBeNull();
  });

  // Review focus 2: a sync belongs to its template. Moving to another must
  // not carry its spinner, result or error along.
  it("starts fresh on another template while a sync runs", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse(registration({ status: "running" }));
      return jsonResponse([current, other]);
    });
    const router = renderPanel(seed());
    fireEvent.click(syncButton());
    await waitFor(() => expect(syncButton().disabled).toBe(true));

    await act(async () => {
      await router.navigate("/templates/tpl_net/revisions");
    });
    expect(screen.getByRole("heading", { level: 2, name: "network" })).toBeTruthy();
    expect(syncButton().disabled).toBe(false);
    expect(screen.getByTestId("template-sync-result").textContent).toBe("");
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/templates/TemplateSync.test.tsx`
Expected: FAIL: `template-sync` is not in the document (and `./TemplateSync` is not imported anywhere yet).

- [ ] **Step 3: Write Sync**

`web/src/features/templates/TemplateSync.tsx`:

```tsx
import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw } from "lucide-react";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useRegisterTemplateMutation, useTemplateRegistrationQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import { useTemplate } from "./templateContext";
import { shortCommitSHA } from "./templateWorkflow";

// Sync re-registers the template's own identity, so a branch it tracks picks
// up a new commit. Retyping the four fields at /templates/new could mint a
// second template from a typo; the panel already holds every field the POST
// needs.
//
// It polls the registration until it settles, then says what happened in
// words, in a status line mounted empty so the words are announced when they
// arrive: the commit it registered, or that nothing was new. A new commit can
// only be told from an old one by the revisions on screen when Sync was
// pressed: a registration that resolves to a commit the template has reuses
// that revision, and both read "completed".
//
// The panel mounts one per template (key), so a sync never follows the person
// to another template.
export default function TemplateSync() {
  const { revisions, latest } = useTemplate();
  const queryClient = useQueryClient();
  const registerTemplateMutation = useRegisterTemplateMutation(tenantID);
  const [registrationID, setRegistrationID] = useState("");
  const [requestError, setRequestError] = useState("");
  const revisionIDsBefore = useRef<ReadonlySet<string>>(new Set());
  const registrationQuery = useTemplateRegistrationQuery(tenantID, registrationID);

  const registration = registrationQuery.data ?? null;
  const status = registration?.status ?? null;
  const settled = status !== null && isTerminalRegistrationStatus(status);
  // React Query keeps the last `pending` data through a failing poll, so
  // without this the flow would read as busy forever.
  const pollError =
    registrationID !== "" && registrationQuery.isError
      ? registrationQuery.error instanceof Error
        ? registrationQuery.error.message
        : "Request failed"
      : "";
  const syncing = registerTemplateMutation.isPending || (registrationID !== "" && !settled && pollError === "");
  const syncedRevisionID = registration?.template_revision_id ?? "";
  const completed = status === "completed" && syncedRevisionID !== "";
  const somethingNew = completed && !revisionIDsBefore.current.has(syncedRevisionID);
  const failed = settled && status !== "completed";
  const errorMessage = requestError || pollError || (failed ? registration?.error_summary || "Sync failed" : "");

  useEffect(() => {
    if (status !== "completed") {
      return;
    }
    // The workflow writes the revision; only a refetch of the tenant's list
    // shows it.
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
  }, [status, syncedRevisionID, queryClient]);

  async function handleSync() {
    if (syncing) {
      return;
    }
    setRequestError("");
    // Drop any earlier attempt, so this one polls its own registration.
    setRegistrationID("");
    revisionIDsBefore.current = new Set(revisions.map((templateRevision) => templateRevision.id));
    try {
      const next = await registerTemplateMutation.mutateAsync({
        repo_owner: latest.repo_owner,
        repo_name: latest.repo_name,
        source_ref: latest.source_ref,
        root_path: latest.root_path
      });
      setRegistrationID(next.id);
    } catch (error) {
      setRequestError(error instanceof Error ? error.message : "Request failed");
    }
  }

  return (
    <div className="flex max-w-sm shrink-0 flex-col items-end gap-1.5">
      <button
        type="button"
        className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")}
        disabled={syncing}
        onClick={() => void handleSync()}
        data-testid="template-sync"
      >
        {syncing ? (
          <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
        ) : (
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
        )}
        Sync
      </button>
      <p className="min-h-4 text-right text-meta text-muted-foreground" role="status" aria-live="polite" data-testid="template-sync-result">
        {completed &&
          (somethingNew ? (
            <>
              Registered commit <span className="font-mono text-code-foreground">{shortCommitSHA(registration?.resolved_commit_sha ?? "")}</span>.
            </>
          ) : (
            "Already up to date."
          ))}
      </p>
      {errorMessage !== "" && <ErrorLine testId="template-sync-error">{errorMessage}</ErrorLine>}
    </div>
  );
}
```

- [ ] **Step 4: Mount it in the panel**

In `web/src/features/templates/TemplatePanel.tsx`, add `import TemplateSync from "./TemplateSync";` to the imports, and replace:

```tsx
            <div data-testid="template-sync-slot" />
```

with:

```tsx
            {/* Hidden rather than disabled without the capability: the POST
                would be refused, so there is nothing anyone could do to make
                it work. Keyed on the template, so a sync stays with it. */}
            <RequireCapability capability="canPublishTemplate">
              <TemplateSync key={id} />
            </RequireCapability>
```

- [ ] **Step 5: Run Sync's and the panel's tests to see them pass**

Run: `npx vitest run src/features/templates/TemplateSync.test.tsx src/features/templates/TemplatePanel.test.tsx`
Expected: PASS (the panel tests seed no registration and never press Sync).

- [ ] **Step 6: Commit**

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

- [ ] **Step 1: Write the failing tests**

`web/src/features/templates/RegisterTemplatePanel.test.tsx` (the behaviour tests of `TemplateRegistrationScreen.test.tsx`, moved and updated to the new outcome, plus Review focus 3 and 4):

```tsx
// @vitest-environment jsdom
import { QueryClientProvider } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { createMemoryRouter, RouterProvider, useParams } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "../../auth/AuthContext";
import { queryKeys } from "../../api/queryKeys";
import RegisterTemplatePanel from "./RegisterTemplatePanel";
import { authValue, jsonResponse, registration, revision, TENANT, testQueryClient } from "./testSupport";

function Opened() {
  const { sourceTemplateId = "" } = useParams<{ sourceTemplateId: string }>();
  return <p data-testid="opened">{sourceTemplateId}</p>;
}

function seed(): QueryClient {
  const queryClient = testQueryClient();
  queryClient.setQueryData(queryKeys.templateRevisions(TENANT), [revision()]);
  return queryClient;
}

function renderPanel(queryClient: QueryClient) {
  const router = createMemoryRouter(
    [
      { path: "/templates", element: <p data-testid="templates-stub">templates</p> },
      { path: "/templates/new", element: <RegisterTemplatePanel /> },
      { path: "/templates/:sourceTemplateId/variables", element: <Opened /> }
    ],
    { initialEntries: ["/templates/new"] }
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

function submit() {
  fireEvent.click(screen.getByTestId("register-template-submit"));
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("RegisterTemplatePanel", () => {
  it("asks for the repository, ref and root path, with today's defaults and a way back", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed());

    expect(screen.getByRole("heading", { level: 2, name: "Register template" })).toBeTruthy();
    expect((screen.getByLabelText("Owner") as HTMLInputElement).value).toBe("hashicorp");
    expect((screen.getByLabelText("Repository") as HTMLInputElement).value).toBe("");
    expect((screen.getByLabelText("Ref") as HTMLInputElement).value).toBe("main");
    expect((screen.getByLabelText("Root path") as HTMLInputElement).value).toBe(".");
    expect(screen.getByLabelText("Root path").getAttribute("aria-describedby")).toBeTruthy();
    expect(screen.getByText("The module's directory in the repository, or . when the module is at its root.")).toBeTruthy();
    expect(screen.getByTestId("register-template-cancel").getAttribute("href")).toBe("/templates");
  });

  it("marks itself unsaved once a field is edited, so a background sign-in waits", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    renderPanel(seed());

    expect(document.querySelector("[data-unsaved='true']")).toBeNull();
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "edge" } });
    expect(document.querySelector("[data-unsaved='true']")).not.toBeNull();
  });

  it("posts the fields, says it is registering, then opens the new template", async () => {
    const added = revision({ id: "rev_9", source_template_id: "tpl_9", repo_name: "edge", root_path: "." });
    let listed = [revision()];
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "running", template_revision_id: "" }));
      if (String(input).includes("/template-registrations/")) {
        listed = [added, revision()];
        return jsonResponse(registration({ template_revision_id: "rev_9" }));
      }
      return jsonResponse(listed);
    });
    renderPanel(seed());

    fireEvent.change(screen.getByLabelText("Owner"), { target: { value: "acme" } });
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "edge" } });
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-progress").textContent).toContain("Registering acme/edge at main"));
    expect((screen.getByLabelText("Repository") as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(true);

    await waitFor(() => expect(screen.getByTestId("opened").textContent).toBe("tpl_9"), { timeout: 3000 });
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === "POST");
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ repo_owner: "acme", repo_name: "edge", source_ref: "main", root_path: "." });
  });

  // Review focus 4: the identity was registered already. The registration
  // completes with that template's revision, which is on screen.
  it("opens the existing template when its identity was registered already", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) =>
      init?.method === "POST" || String(input).includes("/template-registrations/")
        ? jsonResponse(registration({ template_revision_id: "rev_1" }))
        : jsonResponse([revision()])
    );
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("opened").textContent).toBe("tpl_1"));
  });

  it.each([
    [registration({ status: "invalid", template_revision_id: "", error_summary: 'root path "aws/sqs-queues": directory does not exist' }), 'root path "aws/sqs-queues": directory does not exist'],
    [registration({ status: "failed", template_revision_id: "", error_summary: "" }), "Registration failed"]
  ])("says why a registration failed and keeps the fields", async (failed, reason) => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => jsonResponse(failed));
    renderPanel(seed());

    fireEvent.change(screen.getByLabelText("Root path"), { target: { value: "aws/sqs-queues" } });
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error")).toBeTruthy());
    const error = within(screen.getByTestId("register-template-error"));
    expect(error.getByRole("alert").textContent).toContain("This template could not be registered. Check the fields and register it again.");
    expect(screen.getByTestId("register-template-error").querySelector("pre")?.textContent).toBe(reason);
    expect((screen.getByLabelText("Root path") as HTMLInputElement).value).toBe("aws/sqs-queues");
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
    expect(screen.queryByTestId("opened")).toBeNull();
  });

  it("shows a refused request the same way", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (_input, init) =>
      init?.method === "POST" ? jsonResponse({ error: "invalid_request", message: "repo_name is required" }, 400) : jsonResponse([revision()])
    );
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error").textContent).toContain("repo_name is required"));
    expect((screen.getByTestId("register-template-submit") as HTMLButtonElement).disabled).toBe(false);
  });

  // Review focus 3: a poll that keeps failing must not leave the form busy.
  it("stops and says why when the registration cannot be read", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      if (init?.method === "POST") return jsonResponse(registration({ status: "pending", template_revision_id: "" }));
      if (String(input).includes("/template-registrations/")) return jsonResponse({ error: "forbidden", message: "forbidden" }, 403);
      return jsonResponse([revision()]);
    });
    renderPanel(seed());
    submit();

    await waitFor(() => expect(screen.getByTestId("register-template-error").textContent).toContain("forbidden"), { timeout: 3000 });
    expect(screen.queryByTestId("register-template-progress")).toBeNull();
    expect((screen.getByLabelText("Owner") as HTMLInputElement).disabled).toBe(false);
  });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `npx vitest run src/features/templates/RegisterTemplatePanel.test.tsx`
Expected: FAIL, "Failed to resolve import ./RegisterTemplatePanel".

- [ ] **Step 3: Write the panel**

`web/src/features/templates/RegisterTemplatePanel.tsx`:

```tsx
import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useRegisterTemplateMutation, useTemplateRegistrationQuery, useTemplateRevisionsQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { rootPathOf } from "./templateLinks";
import { templatePath } from "./templateRoutes";
import { sourceTemplateKey } from "./templateWorkflow";

const DEFAULTS = { owner: "hashicorp", repo: "", ref: "main", path: "." };

// /templates/new: Register template, inside the page's panel, in place of a
// template's header. Registration is asynchronous: the POST returns a
// registration, which is polled until it settles. While it runs the fields
// are locked and a status row says what is happening. When it completes, the
// template opens once the refreshed list holds its revision; an identity that
// was registered already opens that template. A failure keeps the fields as
// typed, says so, and shows the server's reason as written.
export default function RegisterTemplatePanel() {
  const [repoOwner, setRepoOwner] = useState(DEFAULTS.owner);
  const [repoName, setRepoName] = useState(DEFAULTS.repo);
  const [sourceRef, setSourceRef] = useState(DEFAULTS.ref);
  const [rootPath, setRootPath] = useState(DEFAULTS.path);
  const [registrationID, setRegistrationID] = useState("");
  const [requestError, setRequestError] = useState("");

  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const registerTemplateMutation = useRegisterTemplateMutation(tenantID);
  const registrationQuery = useTemplateRegistrationQuery(tenantID, registrationID);
  const revisions = useTemplateRevisionsQuery(tenantID).data ?? [];

  const registration = registrationQuery.data ?? null;
  const status = registration?.status ?? null;
  const settled = status !== null && isTerminalRegistrationStatus(status);
  const pollError =
    registrationID !== "" && registrationQuery.isError
      ? registrationQuery.error instanceof Error
        ? registrationQuery.error.message
        : "Request failed"
      : "";
  const busy = registerTemplateMutation.isPending || (registrationID !== "" && !settled && pollError === "");
  const failed = settled && status !== "completed";
  const failure = requestError || pollError || (failed ? registration?.error_summary || "Registration failed" : "");

  const registeredRevisionID = status === "completed" ? registration?.template_revision_id ?? "" : "";
  const registered = registeredRevisionID === "" ? null : revisions.find((templateRevision) => templateRevision.id === registeredRevisionID) ?? null;
  const registeredKey = registered ? sourceTemplateKey(registered) : "";

  useEffect(() => {
    if (registeredRevisionID === "") {
      return;
    }
    // The workflow writes the revision; only a refetch of the list shows it.
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
  }, [registeredRevisionID, queryClient]);

  useEffect(() => {
    if (registeredKey !== "") {
      navigate(templatePath(registeredKey), { replace: true });
    }
  }, [registeredKey, navigate]);

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so a half-filled
  // form is never wiped out by a background sign-in redirect.
  const hasUnsaved = repoOwner !== DEFAULTS.owner || repoName !== DEFAULTS.repo || sourceRef !== DEFAULTS.ref || rootPath !== DEFAULTS.path;
  const path = rootPathOf(rootPath);
  const target = `${repoOwner.trim()}/${repoName.trim()}${path === null ? "" : ` · ${path}`} at ${sourceRef.trim()}`;

  async function handleRegister(event: FormEvent) {
    event.preventDefault();
    if (busy) {
      return;
    }
    setRequestError("");
    // Drop any earlier attempt, so a retry polls its own registration.
    setRegistrationID("");
    try {
      const next = await registerTemplateMutation.mutateAsync({ repo_owner: repoOwner, repo_name: repoName, source_ref: sourceRef, root_path: rootPath });
      setRegistrationID(next.id);
    } catch (error) {
      setRequestError(error instanceof Error ? error.message : "Request failed");
    }
  }

  return (
    <section className="flex min-w-0 flex-col" data-testid="register-template-panel" data-unsaved={hasUnsaved ? "true" : undefined}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b border-divider px-7 py-6">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="font-heading text-panel-title font-semibold tracking-title">Register template</h2>
          <p className="text-meta text-muted-foreground">from a Git repository</p>
        </div>
        <Link to="/templates" className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")} data-testid="register-template-cancel">
          Cancel
        </Link>
      </div>
      <form className="flex max-w-140 min-w-0 flex-col gap-5 px-7 pt-5 pb-7" onSubmit={(event) => void handleRegister(event)}>
        <p className="text-meta text-muted-foreground">
          openplan clones the repository, reads the module's variables and its <span className="font-mono text-code-foreground">template.yaml</span>, and
          registers the commit the ref points at.
        </p>
        <div className="flex flex-wrap gap-x-2 gap-y-4">
          <Field id="template-repo-owner" label="Owner" hint="The GitHub organization or user." value={repoOwner} onChange={setRepoOwner} disabled={busy} grow />
          <Field id="template-repo-name" label="Repository" hint="The repository name." value={repoName} onChange={setRepoName} disabled={busy} grow />
        </div>
        <Field
          id="template-source-ref"
          label="Ref"
          hint="A branch or a tag. On a branch, Sync picks up new commits."
          value={sourceRef}
          onChange={setSourceRef}
          disabled={busy}
        />
        <Field
          id="template-root-path"
          label="Root path"
          hint="The module's directory in the repository, or . when the module is at its root."
          value={rootPath}
          onChange={setRootPath}
          disabled={busy}
        />
        <button type="submit" className={cn(buttonClass("primary"), "self-start pointer-coarse:h-11")} disabled={busy} data-testid="register-template-submit">
          {busy ? <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" /> : <Plus data-icon="inline-start" aria-hidden="true" />}
          Register template
        </button>
        {busy && (
          <div role="status" className="flex items-start gap-3 rounded-lg border px-5 py-4" data-testid="register-template-progress">
            <Loader2 aria-hidden="true" className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />
            <span className="flex min-w-0 flex-col gap-1">
              <span className="text-sm leading-label font-medium">
                Registering <span className="font-mono text-meta wrap-anywhere">{target}</span>
              </span>
              <span className="text-meta text-muted-foreground">Cloning the repository and reading the module. The template opens here when it is registered.</span>
            </span>
          </div>
        )}
        {failure !== "" && !busy && (
          <div className="flex flex-col gap-2" data-testid="register-template-error">
            <ErrorLine>This template could not be registered. Check the fields and register it again.</ErrorLine>
            <pre className="m-0 rounded-lg border bg-canvas px-3 py-2.5 font-mono text-xs leading-relaxed whitespace-pre-wrap text-code-foreground wrap-anywhere">
              {failure}
            </pre>
          </div>
        )}
      </form>
    </section>
  );
}

// openplan UI's Field: a label over its control over what it is for. Owner
// and Repository grow side by side and wrap on a phone.
function Field({
  id,
  label,
  hint,
  value,
  onChange,
  disabled,
  grow = false
}: {
  id: string;
  label: string;
  hint: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
  grow?: boolean;
}) {
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", grow && "flex-1 basis-50")}>
      <Label htmlFor={id} className={fieldLabelClass}>
        {label}
      </Label>
      <Input id={id} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} aria-describedby={`${id}-hint`} className={inputClass} />
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">
        {hint}
      </p>
    </div>
  );
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `npx vitest run src/features/templates/RegisterTemplatePanel.test.tsx`
Expected: PASS, 8 tests.

- [ ] **Step 5: Commit**

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

- [ ] **Step 1: Update the router tests first**

In `web/src/app/router.test.tsx`, replace the test `"renders the template registry screen at /templates"` and the test `"renders the template registration screen at /templates/new"` with:

```tsx
  const registeredRevision = {
    id: "rev_1",
    tenant_id: "tenant_123",
    source_template_id: "tpl_1",
    repo_owner: "hashicorp",
    repo_name: "terraform-aws-vpc",
    source_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    root_path: ".",
    name: "VPC",
    description: "",
    tags: [],
    status: "active",
    created_at: "2026-07-19T00:00:00Z"
  };

  async function renderTemplatesRoute(path: string, canPublishTemplate = false) {
    vi.stubEnv("VITE_OPENPLAN_TENANT_ID", "tenant_123");
    const { routeConfig } = await import("./router");
    const { QueryClient, QueryClientProvider } = await import("@tanstack/react-query");
    const { queryKeys } = await import("../api/queryKeys");

    // retry: false + staleTime: Infinity: seeded data never triggers a real fetch().
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    queryClient.setQueryData(queryKeys.templateRevisions("tenant_123"), [registeredRevision]);
    queryClient.setQueryData(queryKeys.templateRevisionVariables("tenant_123", "rev_1"), []);

    const testRouter = createMemoryRouter(routeConfig, { initialEntries: [path] });
    const markup = renderToStaticMarkup(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider
          value={authValue({
            me: { sub: "user_1", tenantID: "tenant_123", displayName: "Test User", globalCapabilities: { isPlatformAdmin: false, canCreateStack: false, canPublishTemplate } }
          })}
        >
          <RouterProvider router={testRouter} />
        </AuthContext.Provider>
      </QueryClientProvider>
    );
    return { markup, testRouter };
  }

  it("renders the templates page at /templates, on the canvas, on the first template", async () => {
    const { markup } = await renderTemplatesRoute("/templates");

    expect(markup).toContain('data-testid="templates-page"');
    expect(markup).toContain('data-testid="template-panel"');
    expect(markup).toContain("VPC");
    expect(markup).toContain('data-canvas="true"');
  });

  it("opens a template's address on its Variables tab", async () => {
    const { testRouter } = await renderTemplatesRoute("/templates/tpl_1");
    await vi.waitFor(() => expect(testRouter.state.location.pathname).toBe("/templates/tpl_1/variables"));
  });

  it("renders a template's Revisions tab", async () => {
    const { markup } = await renderTemplatesRoute("/templates/tpl_1/revisions");
    expect(markup).toContain('data-testid="template-revisions"');
  });

  it("renders Register template at /templates/new for people who can publish templates", async () => {
    const { markup } = await renderTemplatesRoute("/templates/new", true);

    expect(markup).toContain('data-testid="register-template-panel"');
    expect(markup).toContain("Root path");
    expect(markup).toContain('data-canvas="true"');
  });

  it("refuses /templates/new to people who cannot publish templates", async () => {
    const { markup } = await renderTemplatesRoute("/templates/new");

    expect(markup).toContain('data-testid="route-access-denied"');
    expect(markup).not.toContain('data-testid="register-template-panel"');
  });
```

(`renderToStaticMarkup` runs no effects, so the template index is a loader redirect, as `/stacks/:stackId/templates` is, and the test polls the router's state with `vi.waitFor`.)

- [ ] **Step 2: Run the router tests to see the new ones fail**

Run: `npx vitest run src/app/router.test.tsx`
Expected: FAIL on the five new tests (the old screens are still routed).

- [ ] **Step 3: Route the new page**

In `web/src/app/router.tsx`, replace the three imports

```tsx
import TemplateRegistryScreen from "../features/templates/TemplateRegistryScreen";
import TemplateRegistrationScreen from "../features/templates/TemplateRegistrationScreen";
import TemplateDetailScreen from "../features/templates/TemplateDetailScreen";
```

with

```tsx
import RegisterTemplatePanel from "../features/templates/RegisterTemplatePanel";
import RevisionsTab from "../features/templates/RevisionsTab";
import RegistryTemplatePanel from "../features/templates/TemplatePanel";
import TemplatesIndexPanel from "../features/templates/TemplatesIndexPanel";
import TemplatesPage from "../features/templates/TemplatesPage";
import { templatePath } from "../features/templates/templateRoutes";
import RegistryVariablesTab from "../features/templates/VariablesTab";
```

(`RegistryTemplatePanel` and `RegistryVariablesTab` keep them apart from the stack page's `TemplatePanel` and `TemplateVariablesTab`, which this file also imports. `redirect` is already imported from `react-router-dom`.)

Then replace the three routes

```tsx
          { path: "templates", element: <TemplateRegistryScreen /> },
          { path: "templates/new", element: <TemplateRegistrationScreen /> },
          // After "templates/new", so the static segment is matched first
          // rather than being read as a source template id.
          { path: "templates/:sourceTemplateId", element: <TemplateDetailScreen /> },
```

with

```tsx
          // Every registered template, and the selected one's panel, which
          // each route below draws into. "new" is a static segment, so it is
          // matched before a template id.
          {
            path: "templates",
            element: <TemplatesPage />,
            handle: canvas,
            children: [
              { index: true, element: <TemplatesIndexPanel /> },
              {
                path: "new",
                element: <RequireCapability capability="canPublishTemplate" mode="route" />,
                children: [{ index: true, element: <RegisterTemplatePanel /> }]
              },
              {
                path: ":sourceTemplateId",
                element: <RegistryTemplatePanel />,
                children: [
                  // A loader redirect, so the old /templates/:id links land
                  // on Variables before anything renders.
                  { index: true, loader: ({ params }) => redirect(templatePath(params.sourceTemplateId ?? "")) },
                  { path: "variables", element: <RegistryVariablesTab /> },
                  { path: "revisions", element: <RevisionsTab /> }
                ]
              }
            ]
          },
```

- [ ] **Step 4: Delete the old screens and their tests**

```bash
git rm src/features/templates/TemplateRegistryScreen.tsx src/features/templates/TemplateRegistryScreen.test.tsx \
  src/features/templates/TemplateDetailScreen.tsx src/features/templates/TemplateDetailScreen.test.tsx \
  src/features/templates/TemplateRegistrationScreen.tsx src/features/templates/TemplateRegistrationScreen.test.tsx
```

- [ ] **Step 5: Drop `unsettledStatusTone`**

Confirm nothing else calls it:

Run: `grep -rn "unsettledStatusTone" src --include='*.ts' --include='*.tsx'`
Expected: only `templateWorkflow.ts` and `templateWorkflow.test.ts`.

In `web/src/features/templates/templateWorkflow.ts`, delete the two `statusTone` imports at the top and the `unsettledStatusTone` function with its comment. In `templateWorkflow.test.ts`, delete the `describe` block that tests `unsettledStatusTone` and its name from the import.

- [ ] **Step 6: Run the whole suite and the build**

Run: `npm test`
Expected: PASS, including `src/styles/*.guard.test.ts` (no arbitrary values, palette colours or colour literals; every table on shadcn's Table with a colgroup).

Run: `npm run build`
Expected: exit 0, no type errors.

- [ ] **Step 7: Commit**

```bash
git add -A src/app/router.tsx src/app/router.test.tsx src/features/templates
git commit -m "feat(web): route /templates to the new page and remove the old template screens

/templates/new now requires canPublishTemplate, which it never checked.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Check it in a browser

**Files:** none changed unless a check fails.

- [ ] **Step 1: Start the stack and the driver**

Follow the header of `scripts/drive-web.mjs`: the local stack running (`README.md`), `npm run dev` in `web/`, headless Chrome on port 9222, `OPENPLAN_USER` and `OPENPLAN_PASS` exported. Register at least one template first if the tenant has none (through the page itself, which is part of the check).

- [ ] **Step 2: Shoot each state at desktop width**

From the repository root:

```bash
node scripts/drive-web.mjs --click Templates --shot /tmp/templates-index.png
node scripts/drive-web.mjs --click Templates --click Revisions --shot /tmp/templates-revisions.png
node scripts/drive-web.mjs --click Templates --click "Register template" --shot /tmp/templates-register.png
node scripts/drive-web.mjs --respond '*v1/tenants/*template-revisions=[]' --click Templates --shot /tmp/templates-empty.png
```

Compare each with its K board: `TemplatesPage`, `TemplatesPageRevisions`, `TemplatesPageRegister`, `TemplatesPageEmpty`. Check that the details block's four links open GitHub in a new tab (`--probe 'Array.from(document.querySelectorAll("[data-testid=template-details] a")).map(a => a.target + " " + a.href)'`).

- [ ] **Step 3: Shoot the phone layout**

Restart Chrome with `--window-size=375,812` and repeat Step 2's first two commands. Expected: `/templates` shows only the list; a template shows only its panel with the "Templates" back link, and the variables table scrolls sideways inside its frame, not the page.

- [ ] **Step 4: Fix anything that differs, then commit**

If a check fails, fix it with a test first, and commit as `fix(web): …`. If nothing failed, there is nothing to commit.

---

## After merge

Sync the "openplan UI" design system artifact (https://claude.ai/artifact/4hCc6LddTF31cNeaqHwZn8), as the spec's "After this lands" lists: `arrow-up-right` in Icons, the external link pattern, a Templates page pattern in the README, `/templates` under Pages in `code.md` with the new files in its component table, and the registry removed from "Not yet".

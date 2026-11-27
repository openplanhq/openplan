import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import type { ListPage, StackTemplate, TemplateRevision, TemplateVariable } from "./types";

// Test support for the API layer: a query client, fetch responses and one
// builder per API type. Not a test file itself, so it carries no assertions and imports nothing from
// Vitest.

// retry: false and staleTime: Infinity: seeded data never triggers a refetch,
// so a test only reaches fetch() when it means to.
export function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(asListBody(body)), { status, headers: { "content-type": "application/json" } });
}

// Every list endpoint answers {items, next_cursor}, and nothing else the API
// sends is an array, so a test's fetch mock can give a list's items and have
// them sent as the one page they make.
export function asListBody(body: unknown): unknown {
  return Array.isArray(body) ? listPage(body) : body;
}

export function listPage<T>(items: T[], nextCursor: string | null = null): ListPage<T> {
  return { items, next_cursor: nextCursor };
}

/** A paged list's cache entry holding items as its one loaded page. */
export function pagesOf<T>(items: T[]): InfiniteData<ListPage<T>, string | undefined> {
  return { pages: [listPage(items)], pageParams: [undefined] };
}

export const TENANT = "tenant_123";

// One builder per API type, so a field the API gains is added once. The
// defaults are one installed module: stack_1 installs hashicorp/vpc at its
// root, revision rev_1, which asks for a region. A test that reads a value
// passes it, even when it is the default.

export function templateRevision(overrides: Partial<TemplateRevision> = {}): TemplateRevision {
  return {
    id: "rev_1",
    tenant_id: TENANT,
    template_id: "tmpl_src_1",
    repo_owner: "hashicorp",
    repo_name: "vpc",
    source_ref: "main",
    resolved_commit_sha: "abcdef1234567890",
    commit_subject: "",
    root_path: ".",
    name: "vpc",
    description: "",
    tags: [],
    status: "active",
    created_at: "2026-07-19T00:00:00Z",
    ...overrides
  };
}

export function templateVariable(overrides: Partial<TemplateVariable> = {}): TemplateVariable {
  return {
    template_revision_id: "rev_1",
    name: "region",
    type_expression: "string",
    description: "",
    required: true,
    has_default: false,
    sensitive: false,
    has_validation: false,
    ...overrides
  };
}

export function stackTemplate(overrides: Partial<StackTemplate> = {}): StackTemplate {
  return {
    id: "st_1",
    stack_id: "stack_1",
    key: "vpc",
    template_id: "tmpl_src_1",
    desired_template_revision_id: "rev_1",
    last_applied_template_revision_id: "",
    source_ref: "main",
    workspace_name: "ws-vpc",
    display_name: "",
    config: {},
    last_applied_run_id: "",
    pending_plan_run_id: "",
    plan_state: "none",
    live_state: "never",
    created_by: "user_123",
    lifecycle: "active",
    ...overrides
  };
}

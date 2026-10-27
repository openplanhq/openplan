import { QueryClient } from "@tanstack/react-query";
import { queryKeys } from "../../api/queryKeys";
import type { AuthContextValue } from "../../auth/AuthContext";
import type { Template, TemplateLatestSync, TemplateRevision, TemplateVariable } from "../../api/types";
import { asListBody } from "../../api/listTestSupport";

// Fixtures for the templates tests. Not a test file itself, so it carries no
// assertions and imports nothing from Vitest.

export const TENANT = "tenant_123";

export function revision(overrides: Partial<TemplateRevision> = {}): TemplateRevision {
  return {
    id: "rev_1",
    tenant_id: TENANT,
    template_id: "tpl_1",
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

// The template rev_1 belongs to, never synced unless latest_sync is given.
export function template(overrides: Partial<Template> = {}): Template {
  return {
    id: "tpl_1",
    tenant_id: TENANT,
    repo_owner: "acme",
    repo_name: "infra-modules",
    source_ref: "main",
    root_path: "aws/eks",
    name: "eks",
    description: "An EKS cluster with one managed node group.",
    tags: ["aws", "kubernetes"],
    latest_template_revision_id: "rev_1",
    latest_sync: null,
    created_at: "2026-09-22T10:00:00Z",
    ...overrides
  };
}

export function latestSync(overrides: Partial<TemplateLatestSync> = {}): TemplateLatestSync {
  return {
    status: "synced",
    error_summary: "",
    resolved_commit_sha: "3f9c2a1d8e7b6a5c4d3e2f1a0b9c8d7e6f5a4b3c",
    requested_at: "2026-10-05T09:00:00Z",
    ...overrides
  };
}

// The templates these revisions belong to, in the order first seen, each
// named and described by its first revision (the API lists them newest
// first).
export function templatesOf(revisions: TemplateRevision[]): Template[] {
  const templates = new Map<string, Template>();
  for (const candidate of revisions) {
    if (templates.has(candidate.template_id)) {
      continue;
    }
    templates.set(
      candidate.template_id,
      template({
        id: candidate.template_id,
        repo_owner: candidate.repo_owner,
        repo_name: candidate.repo_name,
        source_ref: candidate.source_ref,
        root_path: candidate.root_path,
        name: candidate.name,
        description: candidate.description,
        tags: candidate.tags,
        latest_template_revision_id: candidate.id
      })
    );
  }
  return [...templates.values()];
}

// Seeds what the templates screens read: the list, and each template's
// revisions.
export function seedTemplates(queryClient: QueryClient, revisions: TemplateRevision[], extra: Template[] = []): void {
  const templates = [...templatesOf(revisions), ...extra];
  queryClient.setQueryData(queryKeys.templates(TENANT), templates);
  for (const seeded of templates) {
    queryClient.setQueryData(
      queryKeys.templateRevisions(TENANT, seeded.id),
      revisions.filter((candidate) => candidate.template_id === seeded.id)
    );
  }
}

// Answers a GET of the templates list or of one template's revisions from
// these revisions, or null for any other request.
export function templateReadResponse(input: unknown, revisions: TemplateRevision[]): Response | null {
  const path = String(input).split("?")[0];
  if (path === `/v1/tenants/${TENANT}/templates`) {
    return jsonResponse(templatesOf(revisions));
  }
  const revisionsOf = path.match(/\/templates\/([^/]+)\/revisions$/);
  if (revisionsOf) {
    return jsonResponse(revisions.filter((candidate) => candidate.template_id === decodeURIComponent(revisionsOf[1])));
  }
  return null;
}

// retry: false and staleTime: Infinity: seeded data never triggers a refetch,
// so a test only reaches fetch() when it means to.
export function testQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

export function authValue(canManageTemplates = true): AuthContextValue {
  return {
    me: {
      sub: "user_1",
      tenant_id: TENANT,
      display_name: "Test User",
      global_capabilities: { can_create_stack: false, can_manage_templates: canManageTemplates, can_manage_platform_access: false }
    },
    status: "authenticated",
    login: () => {},
    logout: () => {}
  };
}

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(asListBody(body)), { status, headers: { "content-type": "application/json" } });
}

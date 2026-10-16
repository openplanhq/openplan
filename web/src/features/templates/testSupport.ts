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

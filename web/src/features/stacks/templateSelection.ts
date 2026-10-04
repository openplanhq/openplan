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

/** Where a template's tab lives, or Change revision, which sits under Settings. */
export function stackTemplatePath(stackId: string, stackTemplateId: string, section: TemplateTab | "upgrade" = "runs"): string {
  return `/stacks/${stackId}/templates/${stackTemplateId}/${section}`;
}

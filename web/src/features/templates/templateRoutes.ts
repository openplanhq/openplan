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

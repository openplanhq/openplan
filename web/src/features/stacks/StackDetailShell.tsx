import { matchPath, Outlet, useLocation, useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import type { Crumb } from "../../shared/Breadcrumb";
import { RouteTab, RouteTabs } from "../../shared/RouteTabs";
import { stackTemplateLabel } from "./stackWorkflow";

// The stack tab a path belongs to: the same match each tab's NavLink makes,
// so the selected tab is always the one marked aria-current. Templates owns
// everything below it; Overview is the stack's own path only.
function stackSection(pathname: string): string | null {
  if (matchPath("/stacks/:stackId", pathname)) return "overview";
  if (matchPath({ path: "/stacks/:stackId/templates", end: false }, pathname)) return "templates";
  if (matchPath("/stacks/:stackId/environment", pathname)) return "environment";
  if (matchPath("/stacks/:stackId/access", pathname)) return "access";
  return null;
}

// Where a page sits below the Templates tab, which the stack tabs alone
// cannot say. The template's own name is a crumb once you are on its page;
// run detail and change revision extend the trail past it.
function templateCrumbs(stackId: string, pathname: string, templateLabel: (id: string) => string): Crumb[] | null {
  const templates: Crumb = { label: "Templates", to: `/stacks/${stackId}/templates` };

  if (matchPath("/stacks/:stackId/templates/new", pathname)) {
    return [templates, { label: "Add template" }];
  }
  const run = matchPath("/stacks/:stackId/templates/:stackTemplateId/runs/:runNumber", pathname);
  if (run) {
    const id = run.params.stackTemplateId ?? "";
    return [templates, { label: templateLabel(id), to: `/stacks/${stackId}/templates/${id}` }, { label: `Run #${run.params.runNumber}` }];
  }
  const upgrade = matchPath("/stacks/:stackId/templates/:stackTemplateId/upgrade", pathname);
  if (upgrade) {
    const id = upgrade.params.stackTemplateId ?? "";
    return [templates, { label: templateLabel(id), to: `/stacks/${stackId}/templates/${id}` }, { label: "Change revision" }];
  }
  const template = matchPath({ path: "/stacks/:stackId/templates/:stackTemplateId", end: false }, pathname);
  if (template) {
    return [templates, { label: templateLabel(template.params.stackTemplateId ?? "") }];
  }
  return null;
}

// Layout route for /stacks/:stackId. The parent RequireCapability canView
// route guard has already resolved (and cached) the stack query before this
// renders, so the header reads from cache without its own loading state.
// Tab contents are owned by the nested routes rendered into <Outlet />.
export default function StackDetailShell() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackData = useStackQuery(tenantID, stackId).data;
  const stack = stackData?.stack;
  const { pathname } = useLocation();

  const templateLabel = (id: string) => {
    const stackTemplate = stackData?.templates.find((candidate) => candidate.id === id);
    return stackTemplate ? stackTemplateLabel(stackTemplate) : id;
  };
  const trail = templateCrumbs(stackId, pathname, templateLabel);

  // On a template's own page the template's tabs replace the stack's, the way
  // a project's tabs replace a team's: one row of tabs, for the thing you are
  // looking at, and the breadcrumb for the way back up. StackTemplateDetailShell
  // draws that row.
  const templateMatch = matchPath({ path: "/stacks/:stackId/templates/:stackTemplateId", end: false }, pathname);
  const currentTemplate =
    templateMatch && templateMatch.params.stackTemplateId !== "new"
      ? stackData?.templates.find((candidate) => candidate.id === templateMatch.params.stackTemplateId) ?? null
      : null;
  const stackCrumb: Crumb = { label: stack?.name ?? stackId };
  const crumbs: Crumb[] = trail
    ? [{ label: "Stacks", to: "/stacks" }, { ...stackCrumb, to: `/stacks/${stackId}` }, ...trail]
    : [{ label: "Stacks", to: "/stacks" }, stackCrumb];

  return (
    // No text colour here: the shell wraps screens still on the legacy
    // layer (templates, access), which inherit theirs from body until they
    // migrate. RouteTabs sets its own.
    <section data-testid="stack-detail-shell">
      <Breadcrumb items={crumbs} />
      {!currentTemplate && (
        <RouteTabs value={stackSection(pathname)} label="Stack sections">
          <RouteTab value="overview" to="." end>
            Overview
          </RouteTab>
          <RouteTab value="templates" to="templates">
            Templates
          </RouteTab>
          <RequireCapability capability="canManageAccess">
            <RouteTab value="environment" to="environment">
              Environment
            </RouteTab>
            <RouteTab value="access" to="access">
              Access
            </RouteTab>
          </RequireCapability>
        </RouteTabs>
      )}
      <Outlet />
    </section>
  );
}

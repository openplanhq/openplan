import { Loader2, RefreshCw } from "lucide-react";
import { matchPath, Outlet, useLocation, useOutletContext, useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import type { StackTemplate } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { RouteTab, RouteTabs } from "../../shared/RouteTabs";
import StatusBadge from "../../shared/StatusBadge";
import { findSelectedStackTemplate, stackTemplateStatus } from "./stackWorkflow";
import { Button } from "@/components/ui/button";

export interface StackTemplateOutletContext {
  stackId: string;
  stackTemplate: StackTemplate;
}

// The template every tab below this shell is about. Tabs only ever render
// inside the shell, which has already resolved the template, so this never
// has a missing value to handle.
// The template tab a path belongs to: the same match each tab's NavLink makes,
// so the selected tab is always the one marked aria-current. Runs owns
// everything below it, run detail included.
function templateSection(pathname: string): string | null {
  const base = "/stacks/:stackId/templates/:stackTemplateId";
  if (matchPath({ path: `${base}/runs`, end: false }, pathname)) return "runs";
  if (matchPath(`${base}/variables`, pathname)) return "variables";
  if (matchPath(`${base}/credentials`, pathname)) return "credentials";
  if (matchPath(`${base}/settings`, pathname)) return "settings";
  return null;
}

export function useStackTemplateOutlet(): StackTemplateOutletContext {
  return useOutletContext<StackTemplateOutletContext>();
}

// Layout route for /stacks/:stackId/templates/:stackTemplateId. Resolves the
// template from the stack query the stack's route guard has already cached and
// hands it to the tab routes rendered into <Outlet />. Its state sits at the
// far end of its tab row.
//
// Until PR 8, body keeps the legacy text colour, so each state sets its own.
//
// Runs is the first tab and the index redirects to it: operating the template
// is what people come here for. Run detail nests under runs/, so the Runs tab
// stays lit while reading one.
export default function StackTemplateDetailShell() {
  const { stackId = "", stackTemplateId = "" } = useParams<{ stackId: string; stackTemplateId: string }>();
  const { pathname } = useLocation();
  const stackQuery = useStackQuery(tenantID, stackId);
  const boundary = useQueryErrorBoundary(stackQuery.error);
  const stackTemplate = findSelectedStackTemplate(stackQuery.data?.templates ?? [], stackTemplateId);

  if (stackQuery.status === "pending") {
    return (
      <section className="text-foreground" data-testid="stack-template-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading template…
        </p>
      </section>
    );
  }

  if (stackQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4 text-foreground" data-testid="stack-template-error">
        <p className="text-muted-foreground">Something went wrong while loading the stack template.</p>
        <Button className="pointer-coarse:h-11" data-testid="stack-template-retry" onClick={() => stackQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  if (!stackTemplate) {
    return (
      <section className="text-foreground" data-testid="stack-template-missing">
        <p className="text-muted-foreground">That template is not installed on this stack.</p>
      </section>
    );
  }

  const context: StackTemplateOutletContext = { stackId, stackTemplate };
  const status = stackTemplateStatus(stackTemplate);
  const onRunPage = matchPath("/stacks/:stackId/templates/:stackTemplateId/runs/:runNumber", pathname) !== null;

  return (
    // The tab row, then the tab's content. grid-cols-1 is minmax(0, 1fr), so
    // a wide child such as the run table scrolls in its own frame instead of
    // widening the page.
    <section className="grid min-w-0 grid-cols-1 content-start gap-6 text-foreground" data-testid="stack-template-detail">
      {/* The template's tabs, with its state at the far end of the same
          row. On a phone the state wraps under the tabs. */}
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <RouteTabs value={templateSection(pathname)} label="Template sections" className="mb-0">
          <RouteTab value="runs" to="runs">
            Runs
          </RouteTab>
          <RouteTab value="variables" to="variables">
            Variables
          </RouteTab>
          <RequireCapability capability="canManageAccess">
            <RouteTab value="credentials" to="credentials">
              Credentials
            </RouteTab>
          </RequireCapability>
          <RouteTab value="settings" to="settings">
            Settings
          </RouteTab>
        </RouteTabs>
        {/* The template's state, on every tab; the description is on hover.
            Left off a run's page, where it would read as the run's state. */}
        {!onRunPage && (
          <StatusBadge tone={status.tone} title={status.description} data-testid="stack-template-state">
            {status.label}
          </StatusBadge>
        )}
      </div>
      {/* Keyed on the template so a tab's local state, such as unsaved
          variable edits, never carries over to another template's page. */}
      <Outlet key={stackTemplate.id} context={context} />
    </section>
  );
}

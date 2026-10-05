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

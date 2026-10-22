import { useEffect, useRef } from "react";
import { ArrowLeft, Info, Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, Outlet, useLocation, useMatch } from "react-router-dom";
import { useTemplatesQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { cn } from "@/lib/utils";
import TemplateList from "./TemplateList";
import type { TemplatesLocationState, TemplatesPageOutletContext } from "./templatesPageOutlet";
import { templatesByName } from "./templateWorkflow";
import RequestIdNote from "../../shared/RequestIdNote";

const registerLinkClass = cn(buttonClass("primary", "lg"), "w-full pointer-coarse:h-11 md:w-auto");

// /templates: every registered template on the left, by name;
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
  const tenantID = useTenantID();
  const templatesQuery = useTemplatesQuery(tenantID);
  const boundary = useQueryErrorBoundary(templatesQuery.error);
  const templateMatch = useMatch("/templates/:sourceTemplateId/*");
  const deleted = (useLocation().state as TemplatesLocationState | null)?.deletedTemplate ?? null;

  if (templatesQuery.status === "pending") {
    return (
      <section data-testid="templates-loading">
        <p className="flex items-center gap-2 text-meta text-muted-foreground" role="status">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading templates…
        </p>
      </section>
    );
  }

  if (templatesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section data-testid="templates-error">
        <PageHeader title="Templates" />
        <div className="flex flex-col items-start gap-3">
          <ErrorLine live={false}>Something went wrong while loading templates.</ErrorLine>
          <RequestIdNote error={templatesQuery.error} />
          <button
            type="button"
            className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
            data-testid="templates-retry"
            onClick={() => void templatesQuery.refetch()}
          >
            <RefreshCw data-icon="inline-start" aria-hidden="true" />
            Retry
          </button>
        </div>
      </section>
    );
  }

  // Every registered template, a first sync still running or failed
  // included: it exists from the moment it is registered.
  const templates = templatesByName(templatesQuery.data);
  const count = templates.length;
  const first = templates[0] ?? null;
  const onPanel = templateMatch !== null;
  const registering = templateMatch?.params.sourceTemplateId === "new";
  const selectedId = onPanel ? (registering ? null : templateMatch.params.sourceTemplateId ?? null) : first?.id ?? null;
  const outletContext: TemplatesPageOutletContext = { indexTemplateId: first?.id ?? null };

  return (
    <section data-testid="templates-page">
      <PageHeader
        title="Templates"
        count={count}
        action={
          <RequireCapability capability="can_publish_template">
            <Link to="/templates/new" className={registerLinkClass} data-testid="register-template-link">
              <Plus data-icon="inline-start" aria-hidden="true" />
              Register template
            </Link>
          </RequireCapability>
        }
      >
        {deleted !== null && <DeletedNote name={deleted} />}
      </PageHeader>
      <div className="flex flex-col overflow-clip rounded-panel border bg-card md:flex-row">
        <div className={cn("min-w-0 flex-col md:flex md:w-90 md:shrink-0 md:border-r", onPanel ? "hidden" : "flex")} data-testid="templates-list-column">
          <TemplateList templates={templates} selectedId={selectedId} />
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

// After a delete the panel that asked for it is gone and the first template
// left is showing, so the header says what happened, in a meta line led by
// info, as openplan UI does where a page sends someone on. Focus moves to it:
// the Delete that held focus has gone, and a screen reader hears the line.
// It lasts as long as the history entry: picking a template moves on.
function DeletedNote({ name }: { name: string }) {
  const ref = useRef<HTMLParagraphElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, []);
  return (
    <p
      ref={ref}
      tabIndex={-1}
      role="status"
      className="flex items-center gap-1.5 text-meta text-muted-foreground outline-none"
      data-testid="template-deleted"
    >
      <Info aria-hidden="true" className="size-3.5 shrink-0" />
      <span className="wrap-anywhere">Deleted {name}.</span>
    </p>
  );
}

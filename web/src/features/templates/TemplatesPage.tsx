import { useEffect, useRef } from "react";
import { Info, Plus } from "lucide-react";
import { Link, Outlet, useLocation, useMatch } from "react-router-dom";
import { useTemplatesQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import LoadError from "../../shared/LoadError";
import LoadingLine from "../../shared/LoadingLine";
import { cn } from "@/lib/utils";
import TemplateList from "./TemplateList";
import type { IndexOutletContext } from "../../shared/indexOutlet";
import type { TemplatesLocationState } from "./templatesPageOutlet";
import { templatesByName } from "./templateWorkflow";

const registerLinkClass = cn(buttonClass("primary", "lg"), "pointer-coarse:h-11");

// /templates: every registered template on the left, by name;
// on the right, the selected template's panel, which is the route below
// rendered into <Outlet />. With no template in the URL, the index draws the
// first one without changing the URL.
//
// The frame mirrors StackPage's classes, and both hand IndexOutletContext to
// the index; the two pages differ in their guards and ids, so they share the
// look, not a component.
export default function TemplatesPage() {
  const tenantID = useTenantID();
  const templatesQuery = useTemplatesQuery(tenantID);
  const boundary = useQueryErrorBoundary(templatesQuery.error);
  const templateMatch = useMatch("/templates/:templateId/*");
  const deleted = (useLocation().state as TemplatesLocationState | null)?.deletedTemplate ?? null;

  if (templatesQuery.status === "pending") {
    return (
      <section data-testid="templates-loading">
        <LoadingLine>Loading templates…</LoadingLine>
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
        <LoadError error={templatesQuery.error} onRetry={() => void templatesQuery.refetch()} retryTestId="templates-retry">
          Something went wrong while loading templates.
        </LoadError>
      </section>
    );
  }

  // Every registered template, a first sync still running or failed
  // included: it exists from the moment it is registered.
  const templates = templatesByName(templatesQuery.data);
  const first = templates[0] ?? null;
  const onPanel = templateMatch !== null;
  const registering = templateMatch?.params.templateId === "new";
  const selectedId = onPanel ? (registering ? null : templateMatch.params.templateId ?? null) : first?.id ?? null;
  const outletContext: IndexOutletContext = { indexTemplateId: first?.id ?? null };

  return (
    <section data-testid="templates-page">
      <PageHeader
        title="Templates"
        action={
          <RequireCapability capability="can_manage_templates">
            <Link to="/templates/new" className={registerLinkClass} data-testid="register-template-link">
              <Plus data-icon="inline-start" aria-hidden="true" />
              Register template
            </Link>
          </RequireCapability>
        }
      >
        {deleted !== null && <DeletedNote name={deleted} />}
      </PageHeader>
      <div className="flex overflow-clip rounded-panel border bg-card">
        <div className="flex w-90 min-w-0 shrink-0 flex-col border-r">
          <TemplateList templates={templates} selectedId={selectedId} />
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
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

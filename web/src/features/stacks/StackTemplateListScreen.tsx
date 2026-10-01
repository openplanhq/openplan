import { Layers, Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import { tenantID } from "../../config";
import RequireCapability from "../../auth/RequireCapability";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { statusGlyph, toneTextClass } from "../../shared/statusTone";
import { stackTemplateLabel, stackTemplateStatus } from "./stackWorkflow";
import { Button, buttonVariants } from "@/components/ui/button";
import { Empty, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// Preflight leaves a heading with the body's type, so each heading sets
// CardTitle's look: family, size, weight and tracking.
const headingClass = "font-heading text-base leading-snug font-medium tracking-normal";

// /stacks/:stackId/templates — the templates installed on a stack, and nothing
// else. Each row opens that template's own page at templates/:stackTemplateId,
// where its runs, variables, credentials and settings live on tabs.
//
export default function StackTemplateListScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackQuery = useStackQuery(tenantID, stackId);
  const boundary = useQueryErrorBoundary(stackQuery.error);
  const stackTemplates = stackQuery.data?.templates ?? [];

  if (stackQuery.status === "pending") {
    return (
      <section data-testid="stack-template-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading templates…
        </p>
      </section>
    );
  }

  if (stackQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4" data-testid="stack-template-error">
        <p className="text-muted-foreground">Something went wrong while loading the stack templates.</p>
        <Button className="pointer-coarse:h-11" data-testid="stack-template-retry" onClick={() => stackQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  return (
    <section data-testid="stack-template-list-screen">
      <div className="grid gap-5" data-testid="stack-template-list-content">
        {/* The heading and the page's action; on a phone the link takes the
            full width under the heading. */}
        <header
          className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between"
          data-testid="stack-template-panel-header"
        >
          <h2 className={headingClass}>Stack templates</h2>
          <RequireCapability capability="canOperate">
            <Link
              className={cn(buttonVariants(), "w-full pointer-coarse:h-11 md:w-auto")}
              to={`/stacks/${stackId}/templates/new`}
              data-testid="add-stack-template-link"
            >
              <Plus data-icon="inline-start" aria-hidden="true" />
              Add template
            </Link>
          </RequireCapability>
        </header>
        {stackTemplates.length === 0 ? (
          <Empty className="border" data-testid="stack-template-empty">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Layers aria-hidden="true" />
              </EmptyMedia>
              <h2 className="font-heading text-sm font-medium tracking-tight">No stack templates installed</h2>
            </EmptyHeader>
          </Empty>
        ) : (
          // One bordered list with a rule between rows, like a template's
          // runs. overflow-hidden clips the rows' hover fill to the corners.
          <div className="grid divide-y overflow-hidden rounded-lg border" data-testid="stack-template-items">
            {stackTemplates.map((item) => {
              const status = stackTemplateStatus(item);
              return (
                // The whole row is the link, 44px tall on a coarse pointer.
                // The name takes the slack and ends in an ellipsis, whole on
                // hover; the state's words keep their place at the far edge.
                // The focus outline is drawn inside, where the frame can't
                // clip it.
                <Link
                  key={item.id}
                  to={`/stacks/${stackId}/templates/${item.id}`}
                  data-testid={`stack-template-link-${item.id}`}
                  className="flex min-h-12 items-center gap-3 px-4 text-sm text-foreground hover:bg-muted focus-visible:-outline-offset-2"
                >
                  {/* The state icon, named for assistive technology; the
                      template's own page explains what the state means. */}
                  <span
                    className={cn("flex-none leading-none", toneTextClass(status.tone))}
                    data-testid={`stack-template-status-${item.id}`}
                    role="img"
                    aria-label={status.label}
                    title={status.label}
                  >
                    {statusGlyph(status.tone)}
                  </span>
                  <span className="min-w-0 flex-1 truncate" title={stackTemplateLabel(item)}>
                    {stackTemplateLabel(item)}
                  </span>
                  <small className="flex-none font-mono text-xs text-muted-foreground">{status.label}</small>
                </Link>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}

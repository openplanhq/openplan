import { ArrowRight, KeyRound, Loader2, Users } from "lucide-react";
import { Link } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import type { AttentionItem, StackListItem } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { attentionRunPath } from "./attention";
import NoTemplatesState from "./NoTemplatesState";
import StackMeta from "./StackMeta";
import StackTemplateStatusLabel, { stackTemplateActivity } from "./StackTemplateStatusLabel";
import { stackTemplateLabel } from "./stackWorkflow";
import { buttonClass } from "../../shared/buttonClass";
import RequestIdNote from "../../shared/RequestIdNote";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// openplan UI's ResourceRow as table cells: 8px of padding either side of a
// column boundary makes the 16px between columns, and 16px at the row's ends.
// Each column's width is its content's (status 190, time 170, action 100)
// plus that padding. Every cell holds meta-sized text and sets it as its own
// size, so all four share one baseline; the table's 14px would put the
// status and the action 1px below the name and the time.
const cellClass = "px-2 py-2 text-meta first:pl-4 last:pr-4";

// The header's buttons, as on the stack page.
const headerLinkClass = cn(buttonClass("outline", "lg"), "pointer-coarse:h-11");

// openplan UI's PreviewPanel: the right side of the stacks index, the
// selected stack at a glance, with links into its sections and its own page.
// The header's buttons are the stack page header's, Environment and Access,
// then Open stack, so the preview and the page they lead to agree.
// The header comes from the list row, so it shows at once; the templates wait
// for the stack's own read.
export default function StackPreview({
  stack,
  attention
}: {
  stack: StackListItem;
  /** Items keyed by stack template id, for the waiting plans' run links. */
  attention: Map<string, AttentionItem>;
}) {
  const stackQuery = useStackQuery(tenantID, stack.id);
  const templates = stackQuery.data?.templates ?? [];
  const base = `/stacks/${stack.id}`;

  return (
    <div className="flex min-w-0 flex-col" data-testid="stack-preview">
      <div className="flex flex-col gap-4 border-b border-divider px-7 py-6 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="font-heading text-title wrap-anywhere">{stack.name}</h2>
          {/* A derived slug only repeats the name; with no tags either, the
              line goes. */}
          {(!stack.slug_is_derived || Object.keys(stack.tags).length > 0) && (
            <StackMeta stack={stack} hideSlug={stack.slug_is_derived} />
          )}
        </div>
        {/* The buttons hold their row: a long stack name wraps instead. */}
        <div className="flex shrink-0 flex-wrap items-center gap-2" data-testid="stack-preview-actions">
          <RequireCapability capability="can_manage_access" stackId={stack.id}>
            <Link to={`${base}/environment`} className={headerLinkClass}>
              <KeyRound data-icon="inline-start" aria-hidden="true" />
              Environment
            </Link>
            <Link to={`${base}/access`} className={headerLinkClass}>
              <Users data-icon="inline-start" aria-hidden="true" />
              Access
            </Link>
          </RequireCapability>
          <Link to={base} className={headerLinkClass} data-testid="stack-preview-open">
            Open stack
            <ArrowRight data-icon="inline-end" aria-hidden="true" />
          </Link>
        </div>
      </div>

      <section aria-labelledby="stack-preview-templates" className="flex flex-col gap-3 px-7 pt-5 pb-7">
        <div className="flex items-center justify-between gap-3">
          <h3 id="stack-preview-templates" className="text-section-title">
            Templates
          </h3>
          <RequireCapability capability="can_operate" stackId={stack.id}>
            <Link to={`${base}/templates/new`} className="text-meta font-medium text-primary hover:underline">
              Add template
            </Link>
          </RequireCapability>
        </div>
        {stackQuery.status === "pending" ? (
          <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="stack-preview-loading">
            <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading templates…
          </p>
        ) : stackQuery.status === "error" ? (
          <div className="flex flex-col gap-1">
            <p className="text-meta text-muted-foreground" data-testid="stack-preview-error">
              Something went wrong while loading this stack's templates.
            </p>
            <RequestIdNote error={stackQuery.error} />
          </div>
        ) : templates.length === 0 ? (
          <NoTemplatesState stackId={stack.id} heading="h4" testId="stack-preview-empty" />
        ) : (
          // A table nested in the panel: fixed columns, so a status that
          // changes length never moves the ones after it.
          <div className="overflow-x-auto rounded-lg border" data-testid="stack-preview-templates">
            <Table className="min-w-2xl">
              <colgroup>
                <col />
                <col className="w-51.5" />
                <col className="w-46.5" />
                <col className="w-31" />
              </colgroup>
              <TableBody>
                {templates.map((stackTemplate) => {
                  const item = attention.get(stackTemplate.id);
                  const runPath = item ? attentionRunPath(item) : null;
                  return (
                    <TableRow key={stackTemplate.id} className="h-13 border-divider hover:bg-transparent">
                      <TableCell className={cellClass}>
                        <Link
                          to={`${base}/templates/${stackTemplate.id}`}
                          className="block truncate font-mono text-meta text-foreground hover:text-primary hover:underline focus-visible:-outline-offset-2"
                          title={stackTemplateLabel(stackTemplate)}
                        >
                          {stackTemplateLabel(stackTemplate)}
                        </Link>
                      </TableCell>
                      <TableCell className={cellClass}>
                        <StackTemplateStatusLabel stackTemplate={stackTemplate} attention={item} />
                      </TableCell>
                      <TableCell className={cn(cellClass, "truncate text-meta text-muted-foreground")}>{stackTemplateActivity(stackTemplate, item)}</TableCell>
                      <TableCell className={cn(cellClass, "text-right")}>
                        {runPath && (
                          <Link to={runPath} className="text-meta font-medium text-primary hover:underline">
                            {item?.kind === "waiting_approval" ? "Review plan" : "View run"}
                          </Link>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}
      </section>
    </div>
  );
}

import { ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import type { AttentionItem, StackListItem } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import { attentionRunPath } from "./attention";
import NoTemplatesState from "./NoTemplatesState";
import StackMeta from "./StackMeta";
import StackTemplateStatusLabel, { stackTemplateActivity } from "./StackTemplateStatusLabel";
import StackSectionLinks, { stackHeaderLinkClass } from "./StackSectionLinks";
import { stackTemplateLabel } from "./stackWorkflow";
import RequestIdNote from "../../shared/RequestIdNote";
import LoadingLine from "../../shared/LoadingLine";
import DataTable, { tableCellClass } from "../../shared/DataTable";
import { TableCell, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// openplan UI's ResourceRow as table cells. Each column's width is its
// content's (status 190, time 170, action 100) plus the table's padding. Every
// cell holds meta-sized text and sets it as its own size, so all four share
// one baseline; the table's 14px would put the status and the action 1px
// below the name and the time.
const cellClass = cn(tableCellClass, "text-meta");

// openplan UI's PreviewPanel: the right side of the stacks index, the
// selected stack at a glance, with links into its sections and its own page.
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
  const tenantID = useTenantID();
  const stackQuery = useStackQuery(tenantID, stack.id);
  const templates = stackQuery.data?.templates ?? [];
  const base = `/stacks/${stack.id}`;

  return (
    <div className="flex min-w-0 flex-col">
      <div className="flex flex-col gap-4 border-b border-divider px-7 py-6 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="font-heading text-title wrap-anywhere">{stack.name}</h2>
          <StackMeta stack={stack} />
        </div>
        <StackSectionLinks stackId={stack.id}>
          <Link to={base} className={stackHeaderLinkClass}>
            Open stack
            <ArrowRight data-icon="inline-end" aria-hidden="true" />
          </Link>
        </StackSectionLinks>
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
          <LoadingLine>Loading templates…</LoadingLine>
        ) : stackQuery.status === "error" ? (
          <div className="flex flex-col gap-1">
            <p className="text-meta text-muted-foreground">
              Something went wrong while loading this stack's templates.
            </p>
            <RequestIdNote error={stackQuery.error} />
          </div>
        ) : templates.length === 0 ? (
          <NoTemplatesState stackId={stack.id} heading="h4" />
        ) : (
          <DataTable columns={[undefined, "w-51.5", "w-46.5", "w-31"]} className="min-w-2xl">
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
          </DataTable>
        )}
      </section>
    </div>
  );
}

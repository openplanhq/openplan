import { Link } from "react-router-dom";
import { useTemplateRunsQuery } from "../../api/queries";
import type { TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { formatDateTime } from "../../shared/formatTimestamp";
import PlanDiff from "../../shared/PlanDiff";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import RunStatusLabel from "./RunStatusLabel";

// 8px either side of a column boundary makes the 16px between columns, and
// 16px at the row's ends. Each column's width is its content's (run 64,
// status 190, changes 110, time 110) plus that padding.
const cellClass = "px-2 py-2 first:pl-4 last:pr-4";

interface TemplateRunHistoryProps {
  stackId: string;
  stackTemplateId: string;
}

// Every run recorded for a template, newest first: its number (the link to
// it), where it is, what its plan would change, who started it and when. The
// rows take no actions: a plan is approved or discarded on its run, after
// someone has read it, and a run planning or applying cannot be stopped.
export default function TemplateRunHistory({ stackId, stackTemplateId }: TemplateRunHistoryProps) {
  const runs = useTemplateRunsQuery(tenantID, stackTemplateId).data ?? [];

  return (
    <div className="min-w-0" data-testid="template-run-history">
      {runs.length === 0 ? (
        <Empty className="gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="template-run-history-empty">
          <EmptyHeader className="gap-2">
            <h3 className="text-sm font-medium">No runs yet</h3>
            <EmptyDescription className="text-meta">Plan to see what this template would create.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        // Fixed columns in a frame that scrolls sideways on a phone, so a
        // status that changes length never moves the columns after it.
        <div className="overflow-x-auto rounded-lg border">
          <Table className="min-w-2xl">
            <colgroup>
              <col className="w-22" />
              <col className="w-51.5" />
              <col className="w-31.5" />
              <col />
              <col className="w-33.5" />
            </colgroup>
            <TableHeader>
              <TableRow className="border-divider hover:bg-transparent">
                {["Run", "Status", "Changes", "Started by", "Time"].map((heading) => (
                  <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-xs font-medium text-muted-foreground")}>
                    {heading}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((run) => (
                <RunRow key={run.id} run={run} to={`/stacks/${stackId}/templates/${stackTemplateId}/runs/${run.run_number}`} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function RunRow({ run, to }: { run: TemplateRun; to: string }) {
  return (
    <TableRow className="h-12 border-divider hover:bg-transparent" data-testid={`template-run-row-${run.id}`}>
      <TableCell className={cellClass}>
        <Link
          to={to}
          className="font-mono text-meta text-foreground hover:text-primary hover:underline focus-visible:-outline-offset-2 pointer-coarse:py-3"
          data-testid={`template-run-history-${run.id}`}
        >
          #{run.run_number}
        </Link>
      </TableCell>
      <TableCell className={cellClass}>
        <RunStatusLabel run={run} data-testid={`template-run-status-${run.id}`} />
      </TableCell>
      <TableCell className={cellClass} data-testid={`template-run-summary-${run.id}`}>
        {run.plan_summary && <PlanDiff summary={run.plan_summary} />}
      </TableCell>
      <TableCell className={cn(cellClass, "truncate text-meta text-muted-foreground")} title={run.trigger_actor_display_name}>
        {run.trigger_actor_display_name}
      </TableCell>
      <TableCell className={cn(cellClass, "text-meta text-muted-foreground")}>
        <time dateTime={run.created_at} title={run.created_at}>
          {formatDateTime(run.created_at)}
        </time>
      </TableCell>
    </TableRow>
  );
}

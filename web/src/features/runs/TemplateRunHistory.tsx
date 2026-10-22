import { RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { useTemplateRunsQuery } from "../../api/queries";
import type { TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { formatDateTime } from "../../shared/formatTimestamp";
import PlanDiff from "../../shared/PlanDiff";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import RunStatusLabel from "./RunStatusLabel";
import RequestIdNote from "../../shared/RequestIdNote";
import LoadMoreButton from "../../shared/LoadMoreButton";

// 8px either side of a column boundary makes the 16px between columns, and
// 16px at the row's ends. Each column's width is its content's (run 64,
// status 190, changes 110, time 110) plus that padding. Every cell holds
// meta-sized text, and sets it as its own size so all five share one
// baseline; the table's 14px would put a 13px name 1px off its neighbours.
const cellClass = "px-2 py-2 text-meta first:pl-4 last:pr-4";

interface TemplateRunHistoryProps {
  stackId: string;
  stackTemplateId: string;
}

// Every run recorded for a template, newest first, a page at a time with
// Load more under the table: its number (the link to
// it), where it is, what its plan would change, who started it and when. The
// rows take no actions: a plan is approved or discarded on its run, after
// someone has read it, and a run planning or applying cannot be stopped.
export default function TemplateRunHistory({ stackId, stackTemplateId }: TemplateRunHistoryProps) {
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplateId);
  const runs = runsQuery.data;
  const boundary = useQueryErrorBoundary(runsQuery.error);

  // "No runs yet" before the list arrives would tell someone a template with
  // a history has never run. While it loads this space stays empty (the
  // toolbar's note says the runs are loading to anyone who could start one);
  // if it never arrives, say that, with a retry. A failed poll keeps the runs
  // it had, and they stay on screen.
  if (runs === undefined) {
    if (runsQuery.status !== "error") {
      return null;
    }
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <div className="flex flex-col items-start gap-3" data-testid="template-run-history-error">
        <ErrorLine live={false}>Something went wrong while loading this template's runs.</ErrorLine>
        <RequestIdNote error={runsQuery.error} />
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="template-run-history-retry"
          onClick={() => void runsQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="min-w-0" data-testid="template-run-history">
      {runs.length === 0 ? (
        <Empty className="gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="template-run-history-empty">
          <EmptyHeader className="gap-2">
            <h3 className="text-label">No runs yet</h3>
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
                  <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-caption font-medium text-muted-foreground")}>
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
      {runsQuery.hasNextPage && (
        <LoadMoreButton
          className="mt-3"
          noun="runs"
          loading={runsQuery.isFetchingNextPage}
          failed={runsQuery.isFetchNextPageError}
          onClick={() => void runsQuery.fetchNextPage()}
          testId="template-run-history-load-more"
        />
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
        {/* A status that names its step can be wider than the column, so it
            wraps here rather than running into the changes beside it. */}
        <RunStatusLabel run={run} className="whitespace-normal" data-testid={`template-run-status-${run.id}`} />
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

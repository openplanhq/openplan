import { Link } from "react-router-dom";
import { useTemplateRunsQuery } from "../../api/queries";
import type { TemplateRun } from "../../api/types";
import { useTenantID } from "../../auth/useTenantID";
import { formatDateTime } from "../../shared/formatTimestamp";
import PlanDiff from "../../shared/PlanDiff";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import EmptyState from "../../shared/EmptyState";
import LoadError from "../../shared/LoadError";
import { stackTemplateRunPath } from "../stacks/templateSelection";
import StatusLabel from "../../shared/StatusLabel";
import { runIndicator } from "./runIndicator";
import DataTable, { tableCellClass } from "../../shared/DataTable";
import { TableCell, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import LoadMoreButton from "../../shared/LoadMoreButton";

// Each column's width is its content's (run 64, status 190, changes 110, time
// 110) plus the table's padding. Every cell holds meta-sized text, and sets it
// as its own size so all five share one baseline; the table's 14px would put a
// 13px name 1px off its neighbours.
const cellClass = cn(tableCellClass, "text-meta");

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
  const tenantID = useTenantID();
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
      <LoadError error={runsQuery.error} onRetry={() => void runsQuery.refetch()} testId="template-run-history-error" retryTestId="template-run-history-retry">
        Something went wrong while loading this template's runs.
      </LoadError>
    );
  }

  return (
    <div className="min-w-0" data-testid="template-run-history">
      {runs.length === 0 ? (
        <EmptyState title="No runs yet" titleAs="h3" description="Plan to see what this template would create." testId="template-run-history-empty" />
      ) : (
        <DataTable columns={["w-22", "w-51.5", "w-31.5", undefined, "w-33.5"]} headings={["Run", "Status", "Changes", "Started by", "Time"]} className="min-w-2xl">
          {runs.map((run) => (
            <RunRow key={run.id} run={run} to={stackTemplateRunPath(stackId, stackTemplateId, run.run_number)} />
          ))}
        </DataTable>
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
        <StatusLabel indicator={runIndicator(run)} className="whitespace-normal" data-testid={`template-run-status-${run.id}`} />
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

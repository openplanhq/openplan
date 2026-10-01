import { useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { useParams } from "react-router-dom";
import {
  useApproveRunMutation,
  useDiscardRunMutation,
  useTemplateRunLogsQuery,
  useTemplateRunQuery,
  useTemplateRunsQuery
} from "../../api/queries";
import { isTerminalRunStatus } from "../../api/polling";
import { tenantID } from "../../config";
import { formatDateTime } from "../../shared/formatTimestamp";
import StatusBadge from "../../shared/StatusBadge";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { statusTone } from "../../shared/statusTone";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { planSummaryLabel } from "../stacks/stackWorkflow";
import RunLogsPanel from "./RunLogsPanel";
import { runProgressTag, runStatusLabel } from "./runStatusLabel";
import { WaitingRunActions } from "./TemplateRunHistory";

// /stacks/:stackId/templates/:stackTemplateId/runs/:runNumber — plan/apply
// detail with per-phase logs, reached from the Runs tab. The URL carries the
// run's number within its template, which is what people see; the run's id,
// which every run endpoint takes, comes from the template's runs list. That
// list is the one the Runs tab already loaded, so arriving from there costs no
// extra request. The run's logs stack in the order their commands ran.
export default function RunDetailScreen() {
  const {
    stackId = "",
    stackTemplateId = "",
    runNumber = ""
  } = useParams<{ stackId: string; stackTemplateId: string; runNumber: string }>();
  const [errorMessage, setErrorMessage] = useState("");

  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplateId);
  const runId = runsQuery.data?.find((candidate) => String(candidate.run_number) === runNumber)?.id ?? "";

  const runQuery = useTemplateRunQuery(tenantID, runId, { poll: true });
  const boundary = useQueryErrorBoundary(runsQuery.error ?? runQuery.error);
  const run = runQuery.data ?? null;

  const progressTag = run ? runProgressTag(run) : "";
  const logsQuery = useTemplateRunLogsQuery(tenantID, runId, progressTag);

  const approveRunMutation = useApproveRunMutation(tenantID);
  const discardRunMutation = useDiscardRunMutation(tenantID);

  // Only a plan waiting for approval can be acted on: a run planning or
  // applying cannot be stopped.
  const canApprove = Boolean(run && run.status === "waiting_approval");

  async function runAction(action: () => Promise<void>) {
    setErrorMessage("");
    try {
      await action();
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  async function handleApprove() {
    await runAction(async () => {
      await approveRunMutation.mutateAsync(runId);
    });
  }

  async function handleDiscard() {
    await runAction(async () => {
      await discardRunMutation.mutateAsync({ runID: runId, body: { reason: "discarded from run detail" } });
    });
  }

  // A cached list can predate a run that was just started, so a number it
  // lacks only means "no such run" once a refetch has confirmed it.
  if (runsQuery.status === "success" && !runsQuery.isFetching && runId === "") {
    return (
      <section className="grid min-w-0 gap-6" data-testid="run-detail-missing">
        <p className="text-sm text-muted-foreground">This template has no run #{runNumber}.</p>
      </section>
    );
  }

  if (runsQuery.status === "error" || runQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid min-w-0 gap-6" data-testid="run-detail-error">
        <Alert variant="destructive">
          <AlertDescription>Something went wrong while loading the run.</AlertDescription>
        </Alert>
        <Button
          className="pointer-coarse:h-11"
          type="button"
          data-testid="run-detail-retry"
          onClick={() => (runsQuery.status === "error" ? runsQuery.refetch() : runQuery.refetch())}
        >
          <RefreshCw className="size-4" />
          Retry
        </Button>
      </section>
    );
  }

  if (runQuery.status === "pending") {
    return (
      <section className="grid min-w-0 gap-6" data-testid="run-detail-loading">
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Loading run…
        </p>
      </section>
    );
  }

  return (
    <section className="grid min-w-0 gap-6" data-testid="run-detail-screen">
      {run && (
        <>
          <header className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex min-w-0 flex-wrap items-center gap-3">
              <span className="font-heading text-xl font-semibold tracking-tight">Run #{run.run_number}</span>
              <StatusBadge tone={statusTone(run.status)} title={run.status} data-testid="run-detail-status">
                {runStatusLabel(run)}
              </StatusBadge>
            </div>
            {canApprove && (
              <div className="flex flex-wrap gap-2">
                <WaitingRunActions
                  run={run}
                  stackId={stackId}
                  approveBusy={approveRunMutation.isPending}
                  discardBusy={discardRunMutation.isPending}
                  onApprove={handleApprove}
                  onDiscard={handleDiscard}
                />
              </div>
            )}
          </header>
          {errorMessage && (
            <Alert variant="destructive">
              <AlertDescription>{errorMessage}</AlertDescription>
            </Alert>
          )}
          <Card className="gap-0">
            <dl className="grid min-w-0 gap-x-6 gap-y-4 p-4 sm:grid-cols-2 lg:grid-cols-3">
              <div className="min-w-0">
                <dt className="mb-1 text-xs text-muted-foreground">Started</dt>
                <dd className="break-words font-mono text-sm">
                  <time dateTime={run.created_at} title={run.created_at}>
                    {formatDateTime(run.created_at)}
                  </time>
                </dd>
              </div>
              <div className="min-w-0">
                <dt className="mb-1 text-xs text-muted-foreground">Completed</dt>
                <dd className="break-words font-mono text-sm">
                  {hasCompleted(run.completed_at) ? (
                    <time dateTime={run.completed_at} title={run.completed_at}>
                      {formatDateTime(run.completed_at ?? "")}
                    </time>
                  ) : (
                    "—"
                  )}
                </dd>
              </div>
              {run.plan_summary && (
                <div className="min-w-0">
                  <dt className="mb-1 text-xs text-muted-foreground">Changes</dt>
                  <dd className="break-words font-mono text-sm" title="To add, to change, to destroy">
                    {planSummaryLabel(run.plan_summary)}
                  </dd>
                </div>
              )}
              <div className="min-w-0">
                <dt className="mb-1 text-xs text-muted-foreground">Started by</dt>
                <dd className="break-words font-mono text-sm">{run.trigger_actor}</dd>
              </div>
              <div className="min-w-0">
                <dt className="mb-1 text-xs text-muted-foreground">Source</dt>
                <dd className="break-words font-mono text-sm">
                  {run.selected_ref} @ {run.resolved_commit_sha.slice(0, 7)}
                </dd>
              </div>
            </dl>
          </Card>
          {run.error_summary && (
            <Alert variant="destructive">
              <AlertDescription>{run.error_summary}</AlertDescription>
            </Alert>
          )}
        </>
      )}
      <RunLogsPanel
        key={runId}
        runId={runId}
        logs={logsQuery.data}
        failed={logsQuery.isError}
        finished={Boolean(run && isTerminalRunStatus(run.status))}
      />
    </section>
  );
}

// completed_at always arrives: an unfinished run reads as the zero time.
function hasCompleted(completedAt: string | undefined): boolean {
  return Boolean(completedAt) && !completedAt!.startsWith("0001-");
}

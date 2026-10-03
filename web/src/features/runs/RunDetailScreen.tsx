import { useState } from "react";
import type { ReactNode } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { Link, useParams } from "react-router-dom";
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
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import PlanDiff from "../../shared/PlanDiff";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { cn } from "@/lib/utils";
import RunLogsPanel from "./RunLogsPanel";
import { runProgressTag } from "./runIndicator";
import RunStatusLabel from "./RunStatusLabel";
import WaitingRunActions from "./WaitingRunActions";

// /stacks/:stackId/templates/:stackTemplateId/runs/:runNumber — plan/apply
// detail with per-phase logs, inside the template's panel under a trail back
// to its Runs tab. The URL carries the
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

  const trail = <RunTrail stackId={stackId} stackTemplateId={stackTemplateId} runNumber={runNumber} />;

  // A cached list can predate a run that was just started, so a number it
  // lacks only means "no such run" once a refetch has confirmed it.
  if (runsQuery.status === "success" && !runsQuery.isFetching && runId === "") {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-missing">
        {trail}
        <p className="text-meta text-muted-foreground">This template has no run #{runNumber}.</p>
      </section>
    );
  }

  if (runsQuery.status === "error" || runQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-error">
        {trail}
        <ErrorLine live={false}>Something went wrong while loading the run.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "self-start pointer-coarse:h-11")}
          data-testid="run-detail-retry"
          onClick={() => (runsQuery.status === "error" ? runsQuery.refetch() : runQuery.refetch())}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </section>
    );
  }

  if (runQuery.status === "pending") {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-loading">
        {trail}
        <p className="flex items-center gap-2 text-meta text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading run…
        </p>
      </section>
    );
  }

  return (
    <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-screen">
      {trail}
      {run && (
        <>
          <header className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex min-w-0 flex-wrap items-center gap-3">
              <h3 className="font-heading text-lg leading-title font-semibold tracking-title">Run #{run.run_number}</h3>
              <RunStatusLabel run={run} data-testid="run-detail-status" />
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
          {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
          <dl className="grid min-w-0 gap-x-6 gap-y-4 rounded-lg border px-5 py-4 sm:grid-cols-3">
            <Fact term="Started">
              <time dateTime={run.created_at} title={run.created_at}>
                {formatDateTime(run.created_at)}
              </time>
            </Fact>
            <Fact term="Finished">
              {hasCompleted(run.completed_at) ? (
                <time dateTime={run.completed_at} title={run.completed_at}>
                  {formatDateTime(run.completed_at ?? "")}
                </time>
              ) : (
                "Not finished"
              )}
            </Fact>
            {run.plan_summary && (
              <Fact term="Changes">
                <PlanDiff summary={run.plan_summary} />
              </Fact>
            )}
            <Fact term="Started by">{run.trigger_actor_display_name}</Fact>
            <Fact term="Source" mono>
              {run.selected_ref} @ {run.resolved_commit_sha.slice(0, 7)}
            </Fact>
          </dl>
          {run.error_summary && <ErrorLine live={false}>{run.error_summary}</ErrorLine>}
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

// The trail above a run: back to its template's runs, then the run itself.
function RunTrail({ stackId, stackTemplateId, runNumber }: { stackId: string; stackTemplateId: string; runNumber: string }) {
  return (
    <nav aria-label="Run" className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <Link to={`/stacks/${stackId}/templates/${stackTemplateId}/runs`} className="hover:text-foreground hover:underline">
        Runs
      </Link>
      <span aria-hidden="true" className="text-separator">
        /
      </span>
      <span aria-current="page" className="text-foreground">
        Run #{runNumber}
      </span>
    </nav>
  );
}

// One fact about a run: what it is, above its value.
function Fact({ term, mono = false, children }: { term: string; mono?: boolean; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className={cn("text-meta wrap-anywhere", mono && "font-mono")}>{children}</dd>
    </div>
  );
}

// completed_at always arrives: an unfinished run reads as the zero time.
function hasCompleted(completedAt: string | undefined): boolean {
  return Boolean(completedAt) && !completedAt!.startsWith("0001-");
}

import { useRef, useState } from "react";
import type { ReactNode } from "react";
import { Ban, Loader2, RefreshCw } from "lucide-react";
import { useParams } from "react-router-dom";
import {
  useApproveRunMutation,
  useDiscardRunMutation,
  useEndRunMutation,
  useTemplateRunChangesQuery,
  useTemplateRunByNumberQuery,
  useTemplateRunLogsQuery,
  useTemplateRunQuery,
  useTemplateRunsQuery
} from "../../api/queries";
import { ApiRequestError } from "../../api/client";
import { isTerminalRunStatus } from "../../api/polling";
import { apiErrorMessage } from "../../api/problems";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import { formatDateTime } from "../../shared/formatTimestamp";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import InlineConfirm from "../../shared/InlineConfirm";
import PlanDiff from "../../shared/PlanDiff";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { cn } from "@/lib/utils";
import PanelTrail from "../stacks/PanelTrail";
import { stackTemplatePath } from "../stacks/templateSelection";
import RunChanges, { RunViewFrame } from "./RunChanges";
import RunLogsPanel from "./RunLogsPanel";
import { defaultRunView } from "./planChanges";
import type { RunView } from "./planChanges";
import SegmentedControl from "../../shared/SegmentedControl";
import { runProgressTag } from "./runIndicator";
import RunStatusLabel from "./RunStatusLabel";
import WaitingRunActions from "./WaitingRunActions";
import RequestIdNote from "../../shared/RequestIdNote";

// /stacks/:stackId/templates/:stackTemplateId/runs/:runNumber — plan/apply
// detail with per-phase logs, inside the template's panel under a trail back
// to its Runs tab. The URL carries the
// run's number within its template, which is what people see; the run's id,
// which every run endpoint takes, comes from the template's runs list when the
// pages loaded so far hold it, as they do arriving from the Runs tab, at no
// extra request. A run beyond them, or one started since, is read by number.
// The run's logs stack in the order their commands ran.
export default function RunDetailScreen() {
  const tenantID = useTenantID();
  const {
    stackId = "",
    stackTemplateId = "",
    runNumber = ""
  } = useParams<{ stackId: string; stackTemplateId: string; runNumber: string }>();
  const [errorMessage, setErrorMessage] = useState("");

  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplateId);
  const listedId = runsQuery.data?.find((candidate) => String(candidate.run_number) === runNumber)?.id;
  const byNumberQuery = useTemplateRunByNumberQuery(tenantID, stackTemplateId, runNumber, {
    enabled: runsQuery.status !== "pending" && listedId === undefined
  });
  const runId = listedId ?? byNumberQuery.data?.id ?? "";
  // A number with no run is this page's own answer, not the app's not-found page.
  const missing = byNumberQuery.error instanceof ApiRequestError && byNumberQuery.error.status === 404;
  const lookupError = missing ? null : byNumberQuery.error;

  const runQuery = useTemplateRunQuery(tenantID, runId, { poll: true });
  const boundary = useQueryErrorBoundary(lookupError ?? runQuery.error);
  const run = runQuery.data ?? null;

  const progressTag = run ? runProgressTag(run) : "";
  const logsQuery = useTemplateRunLogsQuery(tenantID, runId, progressTag);

  const changesQuery = useTemplateRunChangesQuery(tenantID, runId, run?.plan_changes?.state ?? "");
  // The view the person picked holds while the run polls; until they pick,
  // the run's state picks. Keyed on the run, so another run starts fresh.
  const [picked, setPicked] = useState<{ runId: string; view: RunView } | null>(null);
  const view: RunView = picked?.runId === runId ? picked.view : run ? defaultRunView(run) : "logs";

  const approveRunMutation = useApproveRunMutation(tenantID);
  const discardRunMutation = useDiscardRunMutation(tenantID);
  const endRunMutation = useEndRunMutation(tenantID);

  // A plan waiting for approval is approved or discarded. A run planning or
  // applying cannot be stopped, but one whose workflow is gone can be ended;
  // the server tells the two apart, so the action is offered on both and
  // refused for a run still working.
  const canApprove = Boolean(run && run.status === "waiting_approval");
  const canEnd = Boolean(run && (run.status === "queued" || run.status === "running"));
  const [confirmingEnd, setConfirmingEnd] = useState(false);
  const endButtonRef = useRef<HTMLButtonElement>(null);

  async function runAction(action: () => Promise<void>) {
    setErrorMessage("");
    try {
      await action();
    } catch (error) {
      setErrorMessage(apiErrorMessage(error, "Request failed"));
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

  // The confirm closes either way: ended, the run polls into failed and the
  // action goes with it; refused, the server's reason shows under the header
  // and focus returns to the button that asked.
  async function handleEnd() {
    await runAction(async () => {
      try {
        await endRunMutation.mutateAsync({ runID: runId, stackTemplateID: stackTemplateId, body: { reason: "ended from run detail" } });
      } finally {
        closeEndConfirm();
      }
    });
  }

  function closeEndConfirm() {
    setConfirmingEnd(false);
    endButtonRef.current?.focus();
  }

  // The switch appears once the run has loaded; the logs show before that,
  // as they always have. Both views sit under the same toolbar row, so
  // switching never moves the switch.
  const toolbar = run && (
    <SegmentedControl
      label="Run views"
      value={view}
      onChange={(next) => setPicked({ runId, view: next })}
      options={[
        { value: "changes", label: "Changes" },
        { value: "logs", label: "Logs" }
      ]}
    />
  );
  const logsPanel = (
    <RunLogsPanel
      key={runId}
      runId={runId}
      logs={logsQuery.data}
      executions={run?.executions}
      failed={logsQuery.isError}
      finished={Boolean(run && isTerminalRunStatus(run.status))}
    />
  );
  let runView = logsPanel;
  if (run && view === "changes") {
    runView = (
      <RunChanges
        key={runId}
        run={run}
        changes={changesQuery.data}
        failed={changesQuery.isError}
        onRetry={() => void changesQuery.refetch()}
        toolbar={toolbar}
      />
    );
  } else if (run) {
    runView = <RunViewFrame toolbar={toolbar}>{logsPanel}</RunViewFrame>;
  }

  const trail = (
    <PanelTrail name="Run" parent={{ label: "Runs", to: stackTemplatePath(stackId, stackTemplateId, "runs") }} current={`Run #${runNumber}`} />
  );

  if (missing) {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-missing">
        {trail}
        <p className="text-meta text-muted-foreground">This template has no run #{runNumber}.</p>
      </section>
    );
  }

  if (lookupError || runQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-error">
        {trail}
        <ErrorLine live={false}>Something went wrong while loading the run.</ErrorLine>
        <RequestIdNote error={lookupError ?? runQuery.error} />
        <button
          type="button"
          className={cn(buttonClass("outline"), "self-start pointer-coarse:h-11")}
          data-testid="run-detail-retry"
          onClick={() => (lookupError ? byNumberQuery.refetch() : runQuery.refetch())}
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
              <h3 className="font-heading text-title">Run #{run.run_number}</h3>
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
            {canEnd && (
              <RequireCapability capability="can_operate" stackId={stackId}>
                <button
                  ref={endButtonRef}
                  type="button"
                  className={cn(buttonClass("outline"), "text-destructive hover:text-destructive pointer-coarse:h-11")}
                  aria-expanded={confirmingEnd}
                  onClick={() => setConfirmingEnd(true)}
                >
                  End run
                </button>
              </RequireCapability>
            )}
          </header>
          {canEnd && confirmingEnd && (
            <InlineConfirm
              question={`End run #${run.run_number}?`}
              confirmLabel="End run"
              confirmIcon={Ban}
              busy={endRunMutation.isPending}
              onConfirm={() => void handleEnd()}
              onCancel={closeEndConfirm}
              testId="run-end-confirm"
            >
              {run.operation === "plan"
                ? "Only for a run whose workflow is gone; a run still working is refused."
                : "Only for a run whose workflow is gone. If its apply had started, the infrastructure may have changed: plan again before relying on this stack."}
            </InlineConfirm>
          )}
          {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
          <dl className="grid min-w-0 gap-x-6 gap-y-4 rounded-lg border px-5 py-4 sm:grid-cols-3">
            <Fact term="Started">
              <time dateTime={run.created_at} title={run.created_at}>
                {formatDateTime(run.created_at)}
              </time>
            </Fact>
            <Fact term="Finished">
              {run.completed_at ? (
                <time dateTime={run.completed_at} title={run.completed_at}>
                  {formatDateTime(run.completed_at)}
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
      {runView}
    </section>
  );
}

// One fact about a run: what it is, above its value.
function Fact({ term, mono = false, children }: { term: string; mono?: boolean; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-caption text-muted-foreground">{term}</dt>
      <dd className={cn("text-meta wrap-anywhere", mono && "font-mono")}>{children}</dd>
    </div>
  );
}

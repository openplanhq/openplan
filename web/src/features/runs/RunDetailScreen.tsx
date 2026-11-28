import { useRef, useState } from "react";
import type { ReactNode } from "react";
import { Ban, Trash2 } from "lucide-react";
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
import { isTerminalRunStatus } from "../../api/polling";
import { apiErrorMessage, classifyQueryError } from "../../api/problems";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import { formatDateTime } from "../../shared/formatTimestamp";
import { buttonClass } from "../../shared/buttonClass";
import BusyIcon from "../../shared/BusyIcon";
import ErrorLine from "../../shared/ErrorLine";
import ErrorPanel from "../../shared/ErrorPanel";
import InlineConfirm from "../../shared/InlineConfirm";
import PlanDiff from "../../shared/PlanDiff";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import LoadError from "../../shared/LoadError";
import { shortCommitSHA } from "../templates/templateWorkflow";
import StatusLabel from "../../shared/StatusLabel";
import Breadcrumb from "../../shared/Breadcrumb";
import LoadingLine from "../../shared/LoadingLine";
import { pluralCount } from "../../shared/plural";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { stackTemplatePath } from "../stacks/templateSelection";
import RunChanges, { RunViewFrame } from "./RunChanges";
import RunLogsPanel from "./RunLogsPanel";
import { defaultRunView } from "./planChanges";
import type { RunView } from "./planChanges";
import type { TemplateRun, TemplateRunLog } from "../../api/types";
import SegmentedControl from "../../shared/SegmentedControl";
import { runIndicator, runProgressTag } from "./runIndicator";

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
  const missing = classifyQueryError(byNumberQuery.error) === 404;
  const lookupError = missing ? null : byNumberQuery.error;

  const runQuery = useTemplateRunQuery(tenantID, runId, { poll: true });
  const boundary = useQueryErrorBoundary(lookupError ?? runQuery.error);
  const run = runQuery.data ?? null;

  const progressTag = run ? runProgressTag(run) : "";
  const logsQuery = useTemplateRunLogsQuery(tenantID, runId, progressTag);

  const changesQuery = useTemplateRunChangesQuery(tenantID, runId, run?.plan_changes?.state ?? "");
  // The run's state picks the view once, when the run first loads, and then
  // it holds: nothing the run does moves it under the reader (issue 568).
  // Only the person moves it, by switching, or by approving, which starts the
  // apply whose logs come next. Keyed on the run, so another run starts fresh.
  const [shown, setShown] = useState<{ runId: string; view: RunView } | null>(null);
  if (run?.id === runId && shown?.runId !== runId) {
    setShown({ runId, view: defaultRunView(run) });
  }
  const view: RunView = shown?.runId === runId ? shown.view : "logs";

  const approveRunMutation = useApproveRunMutation(tenantID);
  const discardRunMutation = useDiscardRunMutation(tenantID);
  const endRunMutation = useEndRunMutation(tenantID);

  // A plan waiting for approval is approved or discarded. A run planning or
  // applying cannot be stopped, but one whose workflow is gone can be ended;
  // the server tells the two apart, so the action is offered on both and
  // refused for a run still working.
  const canApprove = Boolean(run && run.status === "waiting_approval");
  const canEnd = Boolean(run && (run.status === "queued" || run.status === "running"));
  // Ending a run and approving a destroy each ask first, in place under the
  // header. A run offers at most one of them, so one opener ref serves both;
  // the state names which, so a question asked of one status never opens on
  // the next.
  const [confirming, setConfirming] = useState<"end" | "destroy" | null>(null);
  const openerRef = useRef<HTMLButtonElement>(null);

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
      setShown({ runId, view: "logs" });
    });
  }

  async function handleDiscard() {
    await runAction(async () => {
      await discardRunMutation.mutateAsync({ runID: runId, body: { reason: "discarded from run detail" } });
    });
  }

  async function handleEnd() {
    await runAction(async () => {
      await endRunMutation.mutateAsync({ runID: runId, body: { reason: "ended from run detail" } });
    });
  }

  // A confirm closes either way: done, the run moves on and the action goes
  // with it; refused, the server's reason shows under the header and focus
  // returns to the button that asked. runAction never throws.
  async function confirmThen(action: () => Promise<void>) {
    await action();
    closeConfirm();
  }

  function closeConfirm() {
    setConfirming(null);
    openerRef.current?.focus();
  }

  // The switch appears once the run has loaded; the logs show before that,
  // as they always have. Both views sit under the same toolbar row, so
  // switching never moves the switch.
  const toolbar = run && (
    <SegmentedControl
      label="Run views"
      value={view}
      onChange={(next) => setShown({ runId, view: next })}
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
    <Breadcrumb label="Run" crumbs={[{ label: "Runs", to: stackTemplatePath(stackId, stackTemplateId, "runs") }]} current={`Run #${runNumber}`} />
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
      <LoadError
        as="section"
        className="min-w-0 gap-5"
        lead={trail}
        error={lookupError ?? runQuery.error}
        onRetry={() => void (lookupError ? byNumberQuery.refetch() : runQuery.refetch())}
        testId="run-detail-error"
        retryTestId="run-detail-retry"
      >
        Something went wrong while loading the run.
      </LoadError>
    );
  }

  if (runQuery.status === "pending") {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="run-detail-loading">
        {trail}
        <LoadingLine>Loading run…</LoadingLine>
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
              <StatusLabel indicator={runIndicator(run)} data-testid="run-detail-status" />
            </div>
            {canApprove && (
              // Discard throws the plan away; Approve applies exactly that
              // saved plan. On a destroy, approving destroys what the
              // template manages, so its button asks first, and names the
              // action alone: the count is the question's, where it reads as
              // a number of resources (issue 569).
              <div className="flex flex-wrap gap-2">
                <RequireCapability capability="can_operate" stackId={stackId}>
                  <button type="button" className={buttonClass("outline")} disabled={discardRunMutation.isPending} onClick={() => void handleDiscard()}>
                    <BusyIcon busy={discardRunMutation.isPending} />
                    Discard
                  </button>
                </RequireCapability>
                <RequireCapability capability="can_approve" stackId={stackId}>
                  {run.operation === "destroy" ? (
                    <Button
                      ref={openerRef}
                      variant="destructive"
                      title={destroyTitle(run)}
                      aria-expanded={confirming === "destroy"}
                      disabled={approveRunMutation.isPending}
                      onClick={() => setConfirming("destroy")}
                    >
                      <Trash2 data-icon="inline-start" aria-hidden="true" />
                      Destroy
                    </Button>
                  ) : (
                    <button type="button" className={buttonClass("primary")} disabled={approveRunMutation.isPending} onClick={() => void handleApprove()}>
                      <BusyIcon busy={approveRunMutation.isPending} />
                      Approve
                    </button>
                  )}
                </RequireCapability>
              </div>
            )}
            {canEnd && (
              <RequireCapability capability="can_operate" stackId={stackId}>
                <button
                  ref={openerRef}
                  type="button"
                  className={cn(buttonClass("outline"), "text-destructive hover:text-destructive pointer-coarse:h-11")}
                  aria-expanded={confirming === "end"}
                  onClick={() => setConfirming("end")}
                >
                  End run
                </button>
              </RequireCapability>
            )}
          </header>
          {canApprove && confirming === "destroy" && (
            <InlineConfirm
              question={`${destroyTitle(run)}?`}
              confirmLabel="Confirm destroy"
              confirmIcon={Trash2}
              busy={approveRunMutation.isPending}
              onConfirm={() => void confirmThen(handleApprove)}
              onCancel={closeConfirm}
              testId="run-destroy-confirm"
            >
              Approving this plan destroys these resources. This action cannot be undone.
            </InlineConfirm>
          )}
          {canEnd && confirming === "end" && (
            <InlineConfirm
              question={`End run #${run.run_number}?`}
              confirmLabel="End run"
              confirmIcon={Ban}
              busy={endRunMutation.isPending}
              onConfirm={() => void confirmThen(handleEnd)}
              onCancel={closeConfirm}
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
              {run.source_ref} @ {shortCommitSHA(run.resolved_commit_sha)}
            </Fact>
          </dl>
          {run.command_error.trim() ? (
            <CommandErrorPanel run={run} logs={logsQuery.data} />
          ) : (
            run.error_summary && <ErrorLine live={false}>{run.error_summary}</ErrorLine>
          )}
        </>
      )}
      {runView}
    </section>
  );
}

function destroyTitle(run: TemplateRun): string {
  return `Destroy ${pluralCount(run.plan_summary?.destroy ?? 0, "resource", "resources")}`;
}

// A run that failed in a command leads with what the command printed, under
// the command's name; the workflow's wrapper around it waits behind Show
// details (issue 522). The failed command is the last that ran, so its log is
// the last one; until the logs load, the title names no command.
function CommandErrorPanel({ run, logs }: { run: TemplateRun; logs: TemplateRunLog[] | undefined }) {
  const command = logs?.[logs.length - 1]?.command_line;
  return (
    <ErrorPanel
      title={command ? `${command} failed` : "The command failed"}
      output={run.command_error}
      details={run.error_summary}
      detailsCollapsed
      live={false}
      testId="run-detail-command-error"
    />
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

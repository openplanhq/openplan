import { Fragment, useId, useState } from "react";
import type { ReactNode } from "react";
import { Ban, FileText, SquareTerminal, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { templateRunLogEventsURL } from "../../api/client";
import { useTemplateRunLogQuery } from "../../api/queries";
import type { RunPhase, TemplateRunExecution, TemplateRunLog, TemplateRunLogLostReason } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import { formatDateTime } from "../../shared/formatTimestamp";
import { LogPhase, LogStep } from "../../shared/LogSteps";
import StatusLabel from "../../shared/StatusLabel";
import type { StatusLabelTone } from "../../shared/StatusLabel";
import { useLiveTemplateRunLog } from "./useLiveTemplateRunLog";
import type { LiveTemplateRunLog } from "./useLiveTemplateRunLog";

interface RunLogsPanelProps {
  runId: string;
  // Undefined until the run's log list first arrives.
  logs: TemplateRunLog[] | undefined;
  // Who started each phase and when, for its header. Undefined until the run
  // arrives; the headers then name only the phase.
  executions: TemplateRunExecution[] | undefined;
  failed: boolean;
  // A finished run records no more logs.
  finished: boolean;
}

// A run's logs, one row per command in the order they ran, grouped under the
// phase that ran them: a plan, then its apply. Each row reads the command that
// ran ("tofu init"); the phase header, always shown, is what tells apart the
// commands both phases run. A command has its row from the moment it starts,
// so the command still running is the live row at the end. The latest log
// opens as it arrives; after that every row stays as it is until someone
// toggles it, so a newer log never closes one being read. Only open rows
// fetch or follow their log.
export default function RunLogsPanel({ runId, logs, executions, failed, finished }: RunLogsPanelProps) {
  const headingId = useId();
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const latestPhase = logs?.[logs.length - 1]?.name ?? "";
  if (latestPhase !== "" && !(latestPhase in open)) {
    setOpen({ ...open, [latestPhase]: true });
  }

  let content;
  if (logs === undefined) {
    content = <p className="px-4 py-3 text-meta text-muted-foreground">{failed ? "Could not load this run's logs." : "Loading logs…"}</p>;
  } else if (logs.length === 0) {
    content = <p className="px-4 py-3 text-meta text-muted-foreground">{finished ? "No logs." : "No logs yet."}</p>;
  } else {
    content = (
      <div className="divide-y divide-divider">
        {groupByPhase(logs).map((group) => (
          <LogPhase key={group.phase} title={PHASE_TITLES[group.phase]} detail={phaseDetail(group.phase, executions)}>
            {group.logs.map((log) => (
              <LogStep
                key={log.name}
                name={log.name}
                label={log.command_line}
                open={open[log.name] ?? false}
                onOpenChange={(isOpen) => setOpen((current) => ({ ...current, [log.name]: isOpen }))}
              >
                <RunLogBody runId={runId} log={log} />
              </LogStep>
            ))}
          </LogPhase>
        ))}
      </div>
    );
  }

  return (
    <section aria-labelledby={headingId} className="overflow-hidden rounded-lg border" data-testid="run-logs-panel">
      <h4 id={headingId} className="flex items-center gap-2 border-b border-divider px-4 py-3 text-sm font-semibold">
        <SquareTerminal className="size-4 text-subtle-foreground" aria-hidden="true" />
        Logs
      </h4>
      {content}
    </section>
  );
}

const PHASE_TITLES: Record<RunPhase, string> = { plan: "Plan", apply: "Apply" };

// Logs arrive in the order their commands ran, and a run's phases run one
// after the other, so each phase's logs are one unbroken stretch.
function groupByPhase(logs: TemplateRunLog[]): { phase: RunPhase; logs: TemplateRunLog[] }[] {
  const groups: { phase: RunPhase; logs: TemplateRunLog[] }[] = [];
  for (const log of logs) {
    const last = groups[groups.length - 1];
    if (last?.phase === log.run_phase) {
      last.logs.push(log);
    } else {
      groups.push({ phase: log.run_phase, logs: [log] });
    }
  }
  return groups;
}

// "started by Priya Raman · 3 Oct, 15:00". An apply that follows a plan was
// started by approving it; an auto-approved apply, the run's only phase, was
// started like a plan. Null until the run's executions arrive, so the header
// names only the phase.
function phaseDetail(phase: RunPhase, executions: TemplateRunExecution[] | undefined): ReactNode {
  const execution = executions?.find((candidate) => candidate.phase === phase);
  if (!execution) {
    return null;
  }
  const approved = phase === "apply" && executions?.some((candidate) => candidate.phase === "plan");
  return (
    <>
      {approved ? "approved by" : "started by"} {execution.actor_display_name} ·{" "}
      <time dateTime={execution.started_at} title={execution.started_at}>
        {formatDateTime(execution.started_at)}
      </time>
    </>
  );
}

// A log reads by where its output is: still being written, stored, or never
// to be stored. Each kind is its own component so a live row's stream and a
// complete row's fetch start and stop with the row. A row that has streamed
// in this mount keeps its live body when its state moves on: swapping it for
// the stored copy or the lost notice would take away the output the reader
// is looking at.
function RunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  // Decided once: a row only ever leaves live, never returns to it.
  const [wasLive] = useState(log.state === "live");
  if (wasLive) {
    return <LiveRunLogBody runId={runId} log={log} />;
  }
  if (log.state === "lost") {
    return (
      <LogStatus icon={TriangleAlert} tone="failed">
        {lostNotice(log)}
      </LogStatus>
    );
  }
  return <CompleteRunLogBody runId={runId} log={log} />;
}

function lostNotice(log: TemplateRunLog): string {
  return log.lost_reason ? `Log lost: ${LOST_REASONS[log.lost_reason]}` : "Log lost.";
}

const LOST_REASONS: Record<TemplateRunLogLostReason, string> = {
  upload_failed: "the upload failed",
  executor_gone: "the executor stopped",
  run_terminated: "the run was canceled",
  record_failed: "its record was never written"
};

function CompleteRunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  const logQuery = useTemplateRunLogQuery(tenantID, runId, log.name);
  if (logQuery.data !== undefined) {
    return <>{logQuery.data || "No log body"}</>;
  }
  return <>{logQuery.isError ? "Could not load this log." : "Loading…"}</>;
}

// The output so far, in the same frame a stored log reads in, and under it
// what is still to come. Once the command finishes, what streamed may be cut
// short, so the stored body is offered rather than swapped in under the
// reader. The command has finished once the stream ends or the row's state
// moves on, whichever the page hears first; the row is the record, so it
// decides when it has.
//
// Loading the full log swaps the stream out for the stored body, which closes
// the stream: nothing it still carried is shown any more.
function LiveRunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  const [loadFull, setLoadFull] = useState(false);
  if (loadFull) {
    return <CompleteRunLogBody runId={runId} log={log} />;
  }
  const url = templateRunLogEventsURL(tenantID, runId, log.name);
  return <LiveRunLogStream key={url} url={url} log={log} onLoadFull={() => setLoadFull(true)} />;
}

function LiveRunLogStream({ url, log, onLoadFull }: { url: string; log: TemplateRunLog; onLoadFull: () => void }) {
  const live = useLiveTemplateRunLog(url);
  const outcome = log.state === "live" ? live.ended : log.state;
  return (
    <>
      {live.pieces.map((piece, index) => (
        // Pieces only ever append, so a piece's place is its identity.
        <Fragment key={index}>{piece}</Fragment>
      ))}
      {outcome === null &&
        (live.failed ? (
          <LogStatus icon={TriangleAlert} tone="failed">
            Could not load this log.
          </LogStatus>
        ) : (
          <StreamingDots />
        ))}
      {live.truncated && (
        <LogStatus icon={Ban} tone="idle">
          Live output stops here; load the full log once the command finishes.
        </LogStatus>
      )}
      {outcome === "lost" && (
        <LogStatus icon={TriangleAlert} tone="failed">
          {lostNotice(log)}
        </LogStatus>
      )}
      {outcome === "complete" && streamFellShort(live, log) && (
        <span className="mt-2 flex items-center gap-1.5 font-sans text-xs text-muted-foreground">
          {/* A truncated log has said why above. */}
          {!live.truncated && "Some output didn't stream."}
          <button type="button" className={buttonClass("quiet")} onClick={onLoadFull}>
            <FileText data-icon="inline-start" aria-hidden="true" />
            Load the full log
          </button>
        </span>
      )}
    </>
  );
}

// Whether the stored log holds more than the stream carried, which is the
// only time the stored copy is worth offering: usually the stream carried
// every byte and the screen already shows the whole log. A truncated stream
// always falls short, as does one that skipped bytes. Otherwise it is known
// only once the stream has stopped adding and the row gives the stored size,
// since a last piece may still be on its way; the size counts the same bytes
// the stream's ends do.
function streamFellShort(live: LiveTemplateRunLog, log: TemplateRunLog): boolean {
  if (live.truncated || live.gap) {
    return true;
  }
  const settled = live.ended !== null || live.failed;
  return settled && log.state === "complete" && live.streamedBytes < (log.size_bytes ?? 0);
}

// The log is still being written: dots that count from one to four in the
// log's own type, on the line where its next output will appear. A screen
// reader hears "Streaming" once; with reduced motion all four stand still.
function StreamingDots() {
  return (
    <span role="status" className="mt-1 block text-muted-foreground">
      <span className="sr-only">Streaming</span>
      <span aria-hidden="true" className="inline-block animate-log-dots overflow-hidden whitespace-nowrap align-bottom">
        ....
      </span>
    </span>
  );
}

// A log's state under its output, as openplan UI draws a state: an icon and a
// word. On its own line, in the interface's type rather than the log's.
function LogStatus({ icon, tone, children }: { icon: LucideIcon; tone: StatusLabelTone; children: ReactNode }) {
  return (
    <StatusLabel icon={icon} tone={tone} className="mt-1 flex w-fit font-sans whitespace-normal">
      {children}
    </StatusLabel>
  );
}

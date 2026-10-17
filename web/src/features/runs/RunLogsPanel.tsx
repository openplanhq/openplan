import { useId, useState } from "react";
import type { ReactNode } from "react";
import { SquareTerminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { templateRunLogEventsURL } from "../../api/client";
import { useTemplateRunLogQuery } from "../../api/queries";
import type { RunPhase, TemplateRunExecution, TemplateRunLog, TemplateRunLogLostReason } from "../../api/types";
import { tenantID } from "../../config";
import { formatDateTime } from "../../shared/formatTimestamp";
import { LogPhase, LogStep } from "../../shared/LogSteps";
import { useLiveTemplateRunLog } from "./useLiveTemplateRunLog";

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
  const [wasLive, setWasLive] = useState(log.state === "live");
  if (log.state === "live" && !wasLive) {
    setWasLive(true);
  }
  if (wasLive) {
    return <LiveRunLogBody runId={runId} log={log} />;
  }
  if (log.state === "lost") {
    return <>{lostNotice(log)}</>;
  }
  return <CompleteRunLogBody runId={runId} log={log} />;
}

function lostNotice(log: TemplateRunLog): string {
  return log.lost_reason ? `Log lost: ${LOST_REASONS[log.lost_reason]}` : "Log lost.";
}

const LOST_REASONS: Record<TemplateRunLogLostReason, string> = {
  upload_failed: "the upload failed",
  executor_gone: "the executor stopped",
  run_terminated: "the run was terminated"
};

function CompleteRunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  // A log whose stream ended complete before the list says so has no
  // uploaded_at yet; its body is keyed under "" until the list catches up.
  const logQuery = useTemplateRunLogQuery(tenantID, runId, log.name, log.uploaded_at ?? "", { enabled: true });
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
function LiveRunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  const live = useLiveTemplateRunLog(templateRunLogEventsURL(tenantID, runId, log.name));
  const [loadFull, setLoadFull] = useState(false);
  if (loadFull) {
    return <CompleteRunLogBody runId={runId} log={log} />;
  }
  const outcome = log.state === "live" ? live.ended : log.state;
  return (
    <>
      {live.text}
      {outcome === null && <span className="block text-muted-foreground">{live.failed ? "Could not load this log." : "Streaming…"}</span>}
      {live.truncated && (
        <span className="block text-muted-foreground">Live output stops here; the full log loads when the command finishes.</span>
      )}
      {outcome === "lost" && <span className="block text-muted-foreground">{lostNotice(log)}</span>}
      {outcome === "complete" && (
        <Button variant="outline" size="sm" className="mt-2 font-sans" onClick={() => setLoadFull(true)}>
          Load the full log
        </Button>
      )}
    </>
  );
}

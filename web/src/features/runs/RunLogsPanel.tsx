import { useId, useState } from "react";
import type { ReactNode } from "react";
import { SquareTerminal } from "lucide-react";
import { useTemplateRunLogQuery } from "../../api/queries";
import type { RunPhase, TemplateRunExecution, TemplateRunLog } from "../../api/types";
import { tenantID } from "../../config";
import { formatDateTime } from "../../shared/formatTimestamp";
import { LogPhase, LogStep } from "../../shared/LogSteps";

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
// commands both phases run. A command's log is recorded once it exits, so the
// command still running has no row yet. The latest log opens as it arrives;
// after that every row stays as it is until someone toggles it, so a newer
// log never closes one being read. Only open rows fetch their log.
export default function RunLogsPanel({ runId, logs, executions, failed, finished }: RunLogsPanelProps) {
  const headingId = useId();
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const latestPhase = logs?.[logs.length - 1]?.phase ?? "";
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
                key={log.phase}
                name={log.phase}
                label={log.command_line}
                open={open[log.phase] ?? false}
                onOpenChange={(isOpen) => setOpen((current) => ({ ...current, [log.phase]: isOpen }))}
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

function RunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  const logQuery = useTemplateRunLogQuery(tenantID, runId, log.phase, log.uploaded_at);
  if (logQuery.data !== undefined) {
    return <>{logQuery.data || "No log body"}</>;
  }
  return <>{logQuery.isError ? "Could not load this log." : "Loading…"}</>;
}

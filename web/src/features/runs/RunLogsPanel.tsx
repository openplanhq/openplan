import { useState } from "react";
import { SquareTerminal } from "lucide-react";
import { useTemplateRunLogQuery } from "../../api/queries";
import type { TemplateRunLog } from "../../api/types";
import { tenantID } from "../../config";
import { LogStep, LogSteps } from "../../shared/LogSteps";

interface RunLogsPanelProps {
  runId: string;
  // Undefined until the run's log list first arrives.
  logs: TemplateRunLog[] | undefined;
  failed: boolean;
  // A finished run records no more logs.
  finished: boolean;
}

// A run's logs, one row per phase in the order its commands ran. A phase's log
// is recorded once its command exits, so the command still running has no row
// yet. The latest phase opens as it arrives; after that every row stays as it
// is until someone toggles it, so a newer phase never closes a log being read.
// Only open rows fetch their log.
export default function RunLogsPanel({ runId, logs, failed, finished }: RunLogsPanelProps) {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const latestPhase = logs?.[logs.length - 1]?.phase ?? "";
  if (latestPhase !== "" && !(latestPhase in open)) {
    setOpen({ ...open, [latestPhase]: true });
  }

  let content;
  if (logs === undefined) {
    content = <p className="muted">{failed ? "Could not load this run's logs." : "Loading logs…"}</p>;
  } else if (logs.length === 0) {
    content = <p className="muted">{finished ? "No logs." : "No logs yet."}</p>;
  } else {
    content = (
      <LogSteps>
        {logs.map((log) => (
          <LogStep
            key={log.phase}
            name={log.phase}
            open={open[log.phase] ?? false}
            onOpenChange={(isOpen) => setOpen((current) => ({ ...current, [log.phase]: isOpen }))}
          >
            <RunLogBody runId={runId} log={log} />
          </LogStep>
        ))}
      </LogSteps>
    );
  }

  return (
    <section className="panel wide">
      <h2>
        <SquareTerminal size={18} />
        Logs
      </h2>
      {content}
    </section>
  );
}

function RunLogBody({ runId, log }: { runId: string; log: TemplateRunLog }) {
  const logQuery = useTemplateRunLogQuery(tenantID, runId, log.phase, log.uploaded_at);
  if (logQuery.data !== undefined) {
    return <>{logQuery.data || "No log body"}</>;
  }
  return <>{logQuery.isError ? "Could not load this log." : "Loading…"}</>;
}

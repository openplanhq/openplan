import { useId } from "react";
import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";

// Logs laid out as CI systems lay out a job's steps: stacked in the order they
// ran, each a row that opens onto its log. The caller decides which rows are
// open and supplies each log.
export function LogSteps({ children }: { children: ReactNode }) {
  return <ol className="log-steps">{children}</ol>;
}

interface LogStepProps {
  name: string;
  open: boolean;
  onToggle: () => void;
  // Mounted only while the row is open, so a log fetched by a component
  // passed here is fetched only once someone opens it.
  children: ReactNode;
}

export function LogStep({ name, open, onToggle, children }: LogStepProps) {
  const logId = useId();
  return (
    <li className="log-step">
      <button
        className="log-step__toggle"
        type="button"
        aria-expanded={open}
        aria-controls={open ? logId : undefined}
        onClick={onToggle}
      >
        <ChevronRight className="log-step__chevron" size={16} aria-hidden="true" />
        {name}
      </button>
      {open && (
        <pre className="log-step__log" id={logId}>
          {children}
        </pre>
      )}
    </li>
  );
}

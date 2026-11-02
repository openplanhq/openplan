import { Check, Diff, Hourglass, LoaderCircle, Minus, TriangleAlert } from "lucide-react";
import type { AttentionItem, StackTemplate } from "../../api/types";
import { formatDateTime, formatTimestamp } from "../../shared/formatTimestamp";
import StatusLabel from "../../shared/StatusLabel";
import type { StatusIndicator } from "../../shared/StatusLabel";
import type { StatusTone } from "../../shared/statusTone";
import { failedOperation } from "./attention";
import { stackTemplateStatus } from "./stackWorkflow";

const BY_TONE: Record<StatusTone, Omit<StatusIndicator, "label">> = {
  settled: { icon: Check, tone: "settled", strong: false },
  waiting: { icon: Diff, tone: "attention", strong: false },
  canceled: { icon: Minus, tone: "idle", strong: false },
  failed: { icon: TriangleAlert, tone: "failed", strong: true },
  progress: { icon: LoaderCircle, tone: "settled", strong: false }
};

// A plan waits while its run does, which only the attention list knows. The
// template's pending plan won't do: it stays pending through the apply it
// becomes, until that ends (issue 567). The list polls slowly, but the
// stack's page refreshes it as a plan starts to wait, and Approve and
// Discard refresh it as one stops.
function isWaiting(attention: AttentionItem | undefined): attention is AttentionItem & { kind: "waiting_approval" } {
  return attention?.kind === "waiting_approval";
}

/**
 * openplan UI's status for one installed template. A plan waiting for
 * approval outranks the live state: it is the one thing a person can act on.
 * Everything else is stackTemplateStatus, drawn with an icon instead of a dot.
 */
export function stackTemplateIndicator(stackTemplate: StackTemplate, attention: AttentionItem | undefined): StatusIndicator {
  if (isWaiting(attention)) {
    return { label: "waiting for approval", icon: Hourglass, tone: "attention", strong: true };
  }
  // A plan or apply that failed last is what needs a person now, over the
  // live state it left behind, applied or not.
  if (attention?.kind === "run_failed") {
    return { label: `${failedOperation(attention)} failed`, ...BY_TONE.failed };
  }
  const status = stackTemplateStatus(stackTemplate);
  return { label: status.label, ...BY_TONE[status.tone] };
}

/** What last happened to the template, for the row's time column. */
export function stackTemplateActivity(stackTemplate: StackTemplate, attention: AttentionItem | undefined): string {
  if (isWaiting(attention)) {
    return attention.at ? `Planned ${formatDateTime(attention.at)}` : "Planned";
  }
  if (stackTemplate.lifecycle === "failed" || attention?.kind === "run_failed") {
    return attention?.at ? `Failed ${formatDateTime(attention.at)}` : "";
  }
  if (stackTemplate.lifecycle === "destroying") {
    return "";
  }
  if (stackTemplate.last_applied_at) {
    return `Applied ${formatTimestamp(stackTemplate.last_applied_at)}`;
  }
  return stackTemplate.live_state === "never" ? "Never applied" : "";
}

export default function StackTemplateStatusLabel({
  stackTemplate,
  attention
}: {
  stackTemplate: StackTemplate;
  attention: AttentionItem | undefined;
}) {
  const indicator = stackTemplateIndicator(stackTemplate, attention);
  return (
    <StatusLabel icon={indicator.icon} tone={indicator.tone} strong={indicator.strong} data-testid={`stack-template-status-${stackTemplate.id}`}>
      {indicator.label}
    </StatusLabel>
  );
}

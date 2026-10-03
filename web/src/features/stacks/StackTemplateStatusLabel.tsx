import { Check, Diff, Hourglass, LoaderCircle, Minus, TriangleAlert } from "lucide-react";
import type { AttentionItem, StackTemplate } from "../../api/types";
import { formatDateTime, formatTimestamp } from "../../shared/formatTimestamp";
import StatusLabel from "../../shared/StatusLabel";
import type { StatusIndicator } from "../../shared/StatusLabel";
import type { StatusTone } from "../../shared/statusTone";
import { stackTemplateStatus } from "./stackWorkflow";

const BY_TONE: Record<StatusTone, Omit<StatusIndicator, "label">> = {
  settled: { icon: Check, tone: "settled", strong: false },
  waiting: { icon: Diff, tone: "attention", strong: false },
  canceled: { icon: Minus, tone: "idle", strong: false },
  failed: { icon: TriangleAlert, tone: "failed", strong: true },
  progress: { icon: LoaderCircle, tone: "settled", strong: false }
};

/**
 * openplan UI's status for one installed template. A plan waiting for
 * approval outranks the live state: it is the one thing a person can act on.
 * Everything else is stackTemplateStatus, drawn with an icon instead of a dot.
 */
export function stackTemplateIndicator(stackTemplate: StackTemplate, attention: AttentionItem | undefined): StatusIndicator {
  if (attention?.kind === "waiting_approval") {
    return { label: "waiting for approval", icon: Hourglass, tone: "attention", strong: true };
  }
  const status = stackTemplateStatus(stackTemplate);
  return { label: status.label, ...BY_TONE[status.tone] };
}

/** What last happened to the template, for the row's time column. */
export function stackTemplateActivity(stackTemplate: StackTemplate, attention: AttentionItem | undefined): string {
  if (attention?.kind === "waiting_approval") {
    const at = attention.at || stackTemplate.pending_plan_at;
    return at ? `Planned ${formatDateTime(at)}` : "Planned";
  }
  if (stackTemplate.lifecycle === "failed") {
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

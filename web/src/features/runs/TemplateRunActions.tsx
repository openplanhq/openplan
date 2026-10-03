import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { FileSearch, Loader2, Play } from "lucide-react";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useStartTemplateRunMutation, useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import { isRunInFlightError } from "./runErrors";
import { runInFlightReason } from "./useRunInFlight";

interface TemplateRunActionsProps {
  stackId: string;
  stackTemplate: StackTemplate;
}

/** The sentence beside Plan and Apply: what they do, or why they cannot be used now. */
export function runActionsNote({
  canOperate,
  lifecycle,
  activeRun
}: {
  canOperate: boolean;
  lifecycle: string;
  activeRun: TemplateRun | null;
}): string {
  if (!canOperate) {
    return "Starting a run requires operator access.";
  }
  if (lifecycle === "failed") {
    return "A template whose destroy failed cannot start runs.";
  }
  if (activeRun) {
    return `${runInFlightReason(activeRun, "starting another run")}.`;
  }
  return "Plan shows what would change. Apply saves a plan that waits for approval.";
}

// The toolbar of a template's Runs tab: a line that explains the buttons,
// then Plan and Apply, which work the way the Terraform CLI's do. Plan only
// shows what would change. Apply saves a plan that waits for approval, on its
// own run, where approving it applies it; Apply with Auto apply checked
// applies straight away. Destroy lives on the Settings tab.
//
// Run state comes from the server's run history (useTemplateRunsQuery), not
// local state, so it is the same for everyone who can view the stack. The
// stack refresh when a run settles is the panel's: useRefreshStackOnRunChange.
export default function TemplateRunActions({ stackId, stackTemplate }: TemplateRunActionsProps) {
  const [errorMessage, setErrorMessage] = useState("");
  const [autoApprove, setAutoApprove] = useState(false);
  const queryClient = useQueryClient();
  const canOperate = useStackCapabilities(stackId)?.canOperate === true;

  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  const runsReady = runsQuery.status === "success";
  const activeRun = runsReady ? runsQuery.data.find((candidate) => !isTerminalRunStatus(candidate.status)) ?? null : null;

  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const startingOperation = startRunMutation.isPending ? startRunMutation.variables?.body.operation : undefined;
  const disabled = !canOperate || !runsReady || activeRun !== null || stackTemplate.lifecycle !== "active" || startingOperation !== undefined;

  async function startRun(operation: "plan" | "apply") {
    setErrorMessage("");
    try {
      const body = operation === "apply" && autoApprove ? { operation, auto_approve: true } : { operation };
      await startRunMutation.mutateAsync({ stackTemplateID: stackTemplate.id, body });
      await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
      if (isRunInFlightError(error)) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
      }
    }
  }

  return (
    <div className="flex flex-col gap-3" data-testid="template-run-actions">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <p className="text-meta text-muted-foreground" data-testid="template-run-actions-note">
          {runActionsNote({ canOperate, lifecycle: stackTemplate.lifecycle, activeRun })}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {/* Auto apply is an approval given in advance, so it is offered
              only to someone who could approve a plan. */}
          <RequireCapability capability="canApprove" stackId={stackId}>
            <label
              className="flex h-8 cursor-pointer items-center gap-2 px-1 text-meta whitespace-nowrap pointer-coarse:h-11"
              data-testid="template-run-auto-approve"
            >
              <input className="size-4 accent-primary" type="checkbox" checked={autoApprove} onChange={(event) => setAutoApprove(event.target.checked)} />
              Auto apply
            </label>
          </RequireCapability>
          <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} disabled={disabled} onClick={() => void startRun("plan")}>
            {startingOperation === "plan" ? (
              <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
            ) : (
              <FileSearch data-icon="inline-start" aria-hidden="true" />
            )}
            Plan
          </button>
          <button type="button" className={cn(buttonClass("primary"), "pointer-coarse:h-11")} disabled={disabled} onClick={() => void startRun("apply")}>
            {startingOperation === "apply" ? (
              <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
            ) : (
              <Play data-icon="inline-start" aria-hidden="true" />
            )}
            Apply
          </button>
        </div>
      </div>
      {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
    </div>
  );
}

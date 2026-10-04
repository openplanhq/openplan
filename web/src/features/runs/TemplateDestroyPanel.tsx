import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useStartTemplateRunMutation } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import SettingsSection from "../stacks/SettingsSection";
import { stackTemplatePath } from "../stacks/templateSelection";
import { destroyLockReason, useLockState } from "./lockReasons";
import { isRunInFlightError } from "./runErrors";

interface TemplateDestroyPanelProps {
  stackId: string;
  stackTemplate: StackTemplate;
}

// Destroy lives on the Settings tab, a tab away from Plan, because it is the
// one operation that cannot be undone.
//
// Destroy here only plans the destroy: it shows what would be destroyed and
// destroys nothing. Destroying happens when that plan is approved, on its
// run, which is why the red "Destroy N resources" confirmation lives there. A
// destroy is never auto-approved, so that confirmation is always the
// irreversible click.
export default function TemplateDestroyPanel({ stackId, stackTemplate }: TemplateDestroyPanelProps) {
  const [errorMessage, setErrorMessage] = useState("");
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const reason = destroyLockReason(useLockState(stackId, stackTemplate));
  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const destroyBusy = startRunMutation.isPending;

  async function handleDestroy() {
    // Re-read the runs at click time: a run may have started since render.
    const currentRuns = queryClient.getQueryData<TemplateRun[]>(queryKeys.templateRuns(tenantID, stackTemplate.id));
    const currentRunActive = currentRuns?.some((candidate) => !isTerminalRunStatus(candidate.status)) ?? true;
    if (reason !== "" || currentRunActive) {
      return;
    }
    setErrorMessage("");
    try {
      await startRunMutation.mutateAsync({ stackTemplateID: stackTemplate.id, body: { operation: "destroy" } });
      await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
      // The plan, and the button that destroys, are on the Runs tab.
      navigate(stackTemplatePath(stackId, stackTemplate.id, "runs"));
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
      if (isRunInFlightError(error)) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplate.id) });
      }
    }
  }

  return (
    <SettingsSection
      title="Destroy"
      testId="template-destroy-panel"
      description="Plans the removal of everything this template manages. Nothing is removed until that plan is approved, and once it is, it cannot be undone."
      reason={reason}
      reasonTestId="template-destroy-disabled-reason"
      error={errorMessage ? <ErrorLine testId="template-destroy-error">{errorMessage}</ErrorLine> : null}
      action={
        <button
          type="button"
          className={cn(buttonClass("outline"), "text-destructive hover:text-destructive pointer-coarse:h-11")}
          disabled={reason !== "" || destroyBusy}
          onClick={() => void handleDestroy()}
        >
          {destroyBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
          Destroy
        </button>
      }
    />
  );
}

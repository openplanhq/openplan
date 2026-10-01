import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { FileSearch, Loader2, Play } from "lucide-react";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useStartTemplateRunMutation, useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { isRunInFlightError } from "./runErrors";

interface TemplateRunActionsProps {
  stackId: string;
  stackTemplate: StackTemplate;
}

// The header of a template's Runs tab: its title, Plan and Apply, which work
// the way the Terraform CLI's do. Plan only shows what would change. Apply
// saves a plan that waits for approval on its own row, where approving it
// applies it; the row is where the approving Apply and Discard live, so it is
// always plain which run they act on. Apply with Auto Apply checked applies
// straight away, with no plan to approve. Destroy lives on the Settings tab:
// see TemplateDestroyPanel.
//
// Run state is derived entirely from the server's run history
// (useTemplateRunsQuery), not from local component state, so it is visible to
// every user who can view the stack — not just the browser tab that started a
// run.
export default function TemplateRunActions({ stackId, stackTemplate }: TemplateRunActionsProps) {
  const [errorMessage, setErrorMessage] = useState("");
  const [autoApprove, setAutoApprove] = useState(false);
  const queryClient = useQueryClient();

  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplate.id);
  const runs = runsQuery.data ?? [];
  const runsReady = runsQuery.status === "success";
  const latestRun = runsReady ? runs[0] ?? null : null;
  const activeRun = runsReady ? runs.find((candidate) => !isTerminalRunStatus(candidate.status)) ?? null : null;

  const startRunMutation = useStartTemplateRunMutation(tenantID);
  const canStart = runsReady && !activeRun && stackTemplate.lifecycle === "active";

  // plan_state and live_state live on the stack template, but the thing that
  // changes them is a run finishing — and only the runs query polls. Without
  // this the page would keep rendering the state the template had when it
  // loaded.
  //
  // This effect owns the invalidation for the whole template page: the other
  // tabs read the same two queries, and this header is on the default tab.
  const settledRun = latestRun && isTerminalRunStatus(latestRun.status) ? `${latestRun.id}:${latestRun.status}` : "";
  const waitingRun = latestRun?.status === "waiting_approval" ? latestRun.id : "";
  useEffect(() => {
    if (settledRun === "" && waitingRun === "") {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackId) });
  }, [settledRun, waitingRun, stackId, queryClient]);

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

  const startingOperation = startRunMutation.isPending ? startRunMutation.variables?.body.operation : undefined;
  const runButtons = (disabledReason?: string) => {
    const disabled = Boolean(disabledReason) || !canStart || startingOperation !== undefined;
    return (
      <>
        {disabledReason && (
          <p className="text-sm text-muted-foreground" data-testid="template-run-actions-disabled-reason">
            {disabledReason}
          </p>
        )}
        <Button
          variant="outline"
          className="pointer-coarse:h-11"
          disabled={disabled}
          onClick={() => void startRun("plan")}
          type="button"
        >
          {startingOperation === "plan" ? <Loader2 className="size-4 animate-spin" /> : <FileSearch className="size-4" />}
          Plan
        </Button>
        <Button
          className="pointer-coarse:h-11"
          disabled={disabled}
          onClick={() => void startRun("apply")}
          type="button"
        >
          {startingOperation === "apply" ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4" />}
          Apply
        </Button>
      </>
    );
  };

  return (
    <Card className="gap-0" data-testid="template-run-actions">
      <CardHeader className="flex flex-wrap items-center justify-between gap-4 border-b py-4">
        <h2 className="font-heading text-base leading-snug font-medium tracking-normal">Runs</h2>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {/* Auto Apply is an approval given in advance, so it is offered
              only to someone who could approve a plan. It applies to Apply
              alone: a plan applies nothing. */}
          <RequireCapability capability="canApprove" stackId={stackId}>
            <Label
              className="flex min-h-8 cursor-pointer items-center gap-2 whitespace-nowrap pointer-coarse:min-h-11"
              data-testid="template-run-auto-approve"
            >
              <input
                className="size-4 accent-primary"
                type="checkbox"
                checked={autoApprove}
                onChange={(event) => setAutoApprove(event.target.checked)}
              />
              Auto Apply
            </Label>
          </RequireCapability>
          <RequireCapability capability="canOperate" stackId={stackId} fallback={runButtons("Starting a run requires operator access")}>
            {runButtons()}
          </RequireCapability>
        </div>
      </CardHeader>
      {errorMessage && (
        <CardContent className="pt-4">
          <Alert variant="destructive">
            <AlertDescription>{errorMessage}</AlertDescription>
          </Alert>
        </CardContent>
      )}
    </Card>
  );
}

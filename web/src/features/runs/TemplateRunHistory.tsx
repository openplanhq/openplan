import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, CircleStop, Loader2, Trash2 } from "lucide-react";
import { Link } from "react-router-dom";
import { queryKeys } from "../../api/queryKeys";
import { useApproveRunMutation, useDiscardRunMutation, useTemplateRunsQuery } from "../../api/queries";
import type { TemplateRun } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { formatDateTime } from "../../shared/formatTimestamp";
import StatusBadge from "../../shared/StatusBadge";
import { statusTone } from "../../shared/statusTone";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { planSummaryLabel } from "../stacks/stackWorkflow";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { runStatusLabel } from "./runStatusLabel";

interface TemplateRunHistoryProps {
  stackId: string;
  stackTemplateId: string;
}

// Every run recorded for a template, newest first, as a table: its number
// (the link to its detail), where it is (which names its operation, so there is
// no Type column), what its plan would change, who started it and when. A plan
// waiting for approval carries its own actions in a trailing column that only
// appears while some run has one: Approve (Destroy, on a destroy run), which
// applies exactly that saved plan, and Discard. A run planning or applying
// cannot be stopped, so it offers nothing. Actions a viewer may not take are
// left out rather than disabled.
export default function TemplateRunHistory({ stackId, stackTemplateId }: TemplateRunHistoryProps) {
  const [errorMessage, setErrorMessage] = useState("");
  const queryClient = useQueryClient();
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplateId);
  const runs = runsQuery.data ?? [];

  const approveRunMutation = useApproveRunMutation(tenantID);
  const discardRunMutation = useDiscardRunMutation(tenantID);

  async function runAction(action: () => Promise<void>) {
    setErrorMessage("");
    try {
      await action();
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
    await queryClient.invalidateQueries({ queryKey: queryKeys.templateRuns(tenantID, stackTemplateId) });
  }

  const hasActions = runs.some((run) => run.status === "waiting_approval");
  const rowActions = {
    stackId,
    approvingRunID: approveRunMutation.isPending ? approveRunMutation.variables : undefined,
    discardingRunID: discardRunMutation.isPending ? discardRunMutation.variables?.runID : undefined,
    onApprove: (run: TemplateRun) => void runAction(() => approveRunMutation.mutateAsync(run.id)),
    onDiscard: (run: TemplateRun) =>
      void runAction(() => discardRunMutation.mutateAsync({ runID: run.id, body: { reason: "discarded from the runs list" } }))
  };

  return (
    <div className="grid min-w-0 gap-4" data-testid="template-run-history">
      {errorMessage && (
        <Alert variant="destructive">
          <AlertDescription>{errorMessage}</AlertDescription>
        </Alert>
      )}
      {runs.length === 0 ? (
        <Empty className="rounded-lg border px-6 py-8" data-testid="template-run-history-empty">
          <EmptyHeader>
            <EmptyTitle>No runs yet</EmptyTitle>
            <EmptyDescription>Plan to see what this template would change.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="overflow-hidden rounded-lg border">
          <Table>
            <colgroup>
              <col className="w-20" />
              <col className="w-56" />
              <col className="w-32" />
              <col className="w-48" />
              <col className="w-32" />
              {hasActions && <col className="w-64" />}
            </colgroup>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Run</TableHead>
                <TableHead scope="col">Status</TableHead>
                <TableHead scope="col">Changes</TableHead>
                <TableHead scope="col">Actor</TableHead>
                <TableHead scope="col">Time</TableHead>
                {hasActions && (
                  <TableHead className="w-64" scope="col">
                    <span className="sr-only">Actions</span>
                  </TableHead>
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((run) => (
                <RunRow
                  key={run.id}
                  run={run}
                  to={`/stacks/${stackId}/templates/${stackTemplateId}/runs/${run.run_number}`}
                  hasActions={hasActions}
                  {...rowActions}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

interface RunRowProps {
  run: TemplateRun;
  to: string;
  hasActions: boolean;
  stackId: string;
  approvingRunID?: string;
  discardingRunID?: string;
  onApprove: (run: TemplateRun) => void;
  onDiscard: (run: TemplateRun) => void;
}

function RunRow({ run, to, hasActions, stackId, approvingRunID, discardingRunID, onApprove, onDiscard }: RunRowProps) {
  const tone = statusTone(run.status);
  const showApprove = run.status === "waiting_approval";
  const summary = planSummaryLabel(run.plan_summary);

  return (
    <TableRow data-testid={`template-run-row-${run.id}`}>
      <TableCell>
        <Link className="block font-medium text-primary hover:underline pointer-coarse:py-3" to={to} data-testid={`template-run-history-${run.id}`}>
          #{run.run_number}
        </Link>
      </TableCell>
      <TableCell>
        <StatusBadge tone={tone} title={run.status} data-testid={`template-run-status-${run.id}`}>
          {runStatusLabel(run)}
        </StatusBadge>
      </TableCell>
      <TableCell
        className="font-mono text-muted-foreground"
        title={summary ? "To add, to change, to destroy" : undefined}
        data-testid={`template-run-summary-${run.id}`}
      >
        {summary}
      </TableCell>
      <TableCell className="max-w-48 truncate font-mono text-sm text-muted-foreground" title={run.trigger_actor}>
        {run.trigger_actor}
      </TableCell>
      <TableCell className="font-mono text-sm text-muted-foreground">
        <time dateTime={run.created_at} title={run.created_at}>
          {formatDateTime(run.created_at)}
        </time>
      </TableCell>
      {hasActions && (
        <TableCell className="w-64">
          {showApprove && (
            <div className="flex flex-wrap gap-2">
              <WaitingRunActions
                run={run}
                stackId={stackId}
                approveBusy={approvingRunID === run.id}
                discardBusy={discardingRunID === run.id}
                onApprove={() => onApprove(run)}
                onDiscard={() => onDiscard(run)}
              />
            </div>
          )}
        </TableCell>
      )}
    </TableRow>
  );
}

interface WaitingRunActionsProps {
  run: TemplateRun;
  stackId: string;
  approveBusy: boolean;
  discardBusy: boolean;
  onApprove: () => void;
  onDiscard: () => void;
}

// WaitingRunActions are what a plan waiting for approval offers: Discard, which
// throws the plan away, and Approve, which applies exactly that saved plan.
//
// On a destroy run approving destroys what the template manages, so the
// red button opens an AlertDialog before the irreversible approval. The dialog
// offers Cancel and Confirm destroy; the Settings button only plans it.
export function WaitingRunActions({ run, stackId, approveBusy, discardBusy, onApprove, onDiscard }: WaitingRunActionsProps) {
  const count = run.plan_summary?.destroy ?? 0;
  const title = `Destroy ${count} ${count === 1 ? "resource" : "resources"}`;

  return (
    <>
      <RequireCapability capability="canOperate" stackId={stackId}>
        <Button variant="outline" className="pointer-coarse:h-11" disabled={discardBusy} onClick={onDiscard} type="button">
          {discardBusy ? <Loader2 className="size-4 animate-spin" /> : <CircleStop className="size-4" />}
          Discard
        </Button>
      </RequireCapability>
      <RequireCapability capability="canApprove" stackId={stackId}>
        {run.operation === "destroy" ? (
          <AlertDialog>
            <AlertDialogTrigger
              render={<Button variant="destructive" className="pointer-coarse:h-11" disabled={approveBusy} title={title} />}
            >
              <Trash2 className="size-4" />
              Destroy {count}
            </AlertDialogTrigger>
            <AlertDialogContent>
              <div className="grid gap-2">
                <AlertDialogTitle>{title}?</AlertDialogTitle>
                <AlertDialogDescription>
                  Approving this plan destroys these resources. This action cannot be undone.
                </AlertDialogDescription>
              </div>
              <div className="flex flex-wrap justify-end gap-2">
                <AlertDialogClose render={<Button variant="outline" className="pointer-coarse:h-11" />}>
                  Cancel
                </AlertDialogClose>
                <AlertDialogClose
                  render={<Button variant="destructive" className="pointer-coarse:h-11" disabled={approveBusy} />}
                  onClick={onApprove}
                >
                  {approveBusy ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
                  Confirm destroy
                </AlertDialogClose>
              </div>
            </AlertDialogContent>
          </AlertDialog>
        ) : (
          <Button className="pointer-coarse:h-11" disabled={approveBusy} onClick={onApprove} type="button">
            {approveBusy ? <Loader2 className="size-4 animate-spin" /> : <Check className="size-4" />}
            Approve
          </Button>
        )}
      </RequireCapability>
    </>
  );
}

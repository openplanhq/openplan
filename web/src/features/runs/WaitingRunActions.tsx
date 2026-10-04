import { Loader2, Trash2 } from "lucide-react";
import type { TemplateRun } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface WaitingRunActionsProps {
  run: TemplateRun;
  stackId: string;
  approveBusy: boolean;
  discardBusy: boolean;
  onApprove: () => void;
  onDiscard: () => void;
}

// WaitingRunActions are what a plan waiting for approval offers, on its run:
// Discard, which throws the plan away, and Approve, which applies exactly that
// saved plan.
//
// On a destroy run approving destroys what the template manages, so the
// red button opens an AlertDialog before the irreversible approval. The dialog
// offers Cancel and Confirm destroy; the Settings button only plans it.
export default function WaitingRunActions({ run, stackId, approveBusy, discardBusy, onApprove, onDiscard }: WaitingRunActionsProps) {
  const count = run.plan_summary?.destroy ?? 0;
  const title = `Destroy ${count} ${count === 1 ? "resource" : "resources"}`;

  return (
    <>
      <RequireCapability capability="canOperate" stackId={stackId}>
        <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} disabled={discardBusy} onClick={onDiscard}>
          {discardBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
          Discard
        </button>
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
          <button type="button" className={cn(buttonClass("primary"), "pointer-coarse:h-11")} disabled={approveBusy} onClick={onApprove}>
            {approveBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
            Approve
          </button>
        )}
      </RequireCapability>
    </>
  );
}

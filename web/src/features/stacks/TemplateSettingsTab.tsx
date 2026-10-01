import { RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import TemplateDestroyPanel from "../runs/TemplateDestroyPanel";
import { useStackTemplateOutlet } from "./StackTemplateDetailShell";
import { runInFlightReason, useRunInFlight } from "../runs/useRunInFlight";
import { isDestroyingStackTemplate } from "./stackWorkflow";
import { Button, buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/settings — the actions that
// change what the template is rather than run it: choosing another revision,
// and destroying it. They live here, a tab away from the Plan button, so the
// irreversible one is never under the cursor of routine work.
export default function TemplateSettingsTab() {
  const { stackId, stackTemplate } = useStackTemplateOutlet();
  const destroying = isDestroyingStackTemplate(stackTemplate);
  const runInFlight = useRunInFlight(stackTemplate.id);
  const revisionLockedReason = destroying ? "Destroy in progress" : runInFlight ? runInFlightReason(runInFlight, "changing the revision") : "";

  return (
    <div className="grid min-w-0 grid-cols-1 content-start gap-6" data-testid="template-settings-tab">
      {/* A secondary action, so a quiet band rather than a card: the label on
          one side, the control on the other, wrapping under it on a phone. */}
      <section
        className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-muted px-4 py-2"
        data-testid="stack-template-revision-action"
      >
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <RefreshCw aria-hidden="true" className="size-4" />
          Choose a template revision
        </p>
        <RequireCapability capability="canOperate">
          {revisionLockedReason ? (
            <div className="flex flex-wrap items-center gap-3">
              <Button variant="outline" className="pointer-coarse:h-11" disabled data-testid="change-stack-template-revision-link">
                Change revision
              </Button>
              <p className="text-sm text-muted-foreground" data-testid="upgrade-disabled-reason">
                {revisionLockedReason}
              </p>
            </div>
          ) : (
            <Link
              className={cn(buttonVariants({ variant: "outline" }), "pointer-coarse:h-11")}
              to={`/stacks/${stackId}/templates/${stackTemplate.id}/upgrade`}
              data-testid="change-stack-template-revision-link"
            >
              Change revision
            </Link>
          )}
        </RequireCapability>
      </section>
      <TemplateDestroyPanel stackId={stackId} stackTemplate={stackTemplate} />
    </div>
  );
}

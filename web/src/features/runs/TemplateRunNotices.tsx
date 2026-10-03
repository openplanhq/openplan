import { Hourglass, TriangleAlert } from "lucide-react";
import { Link } from "react-router-dom";
import { useTemplateRunsQuery } from "../../api/queries";
import type { StackTemplate, TemplateRun } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import { formatDateTime } from "../../shared/formatTimestamp";
import PlanDiff from "../../shared/PlanDiff";
import { cn } from "@/lib/utils";

const separator = (
  <span aria-hidden="true" className="text-separator">
    ·
  </span>
);

// What on a template waits on a person, above its runs: a plan waiting for
// approval, which Review plan opens, and a destroy that stopped before it
// finished, which View run opens. Nothing when neither is true.
export default function TemplateRunNotices({ stackId, stackTemplate }: { stackId: string; stackTemplate: StackTemplate }) {
  const runs = useTemplateRunsQuery(tenantID, stackTemplate.id).data ?? [];
  const waiting = runs.find((run) => run.status === "waiting_approval") ?? null;
  const failed = stackTemplate.lifecycle === "failed";
  const failedRun = failed ? runs.find((run) => run.operation === "destroy" && run.status === "failed") ?? null : null;
  const runPath = (run: TemplateRun) => `/stacks/${stackId}/templates/${stackTemplate.id}/runs/${run.run_number}`;

  if (!waiting && !failed) {
    return null;
  }

  return (
    <>
      {waiting && (
        <div className="flex flex-col gap-4 rounded-lg border px-5 py-4 md:flex-row md:items-center md:gap-6" data-testid="template-waiting-plan">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="flex items-center gap-2 text-sm leading-label font-medium">
              <Hourglass aria-hidden="true" className="size-4 shrink-0 text-warning" />
              {waiting.operation === "destroy" ? "Destroy plan waiting for approval" : "Plan waiting for approval"}
            </span>
            <span className="flex flex-wrap items-center gap-x-1.5 text-meta text-muted-foreground">
              <span>Run #{waiting.run_number}</span>
              {separator}
              <span>Planned by {waiting.trigger_actor_display_name}</span>
              {separator}
              <time dateTime={stackTemplate.pending_plan_at || waiting.created_at}>
                {formatDateTime(stackTemplate.pending_plan_at || waiting.created_at)}
              </time>
            </span>
          </div>
          {waiting.plan_summary && <PlanDiff summary={waiting.plan_summary} />}
          <Link
            to={runPath(waiting)}
            className={cn(buttonClass("primary"), "shrink-0 self-start pointer-coarse:h-11 md:self-auto")}
            data-testid="template-review-plan"
          >
            Review plan
          </Link>
        </div>
      )}
      {failed && (
        <div className="flex flex-col gap-4 rounded-lg border px-5 py-4 md:flex-row md:items-center md:gap-6" data-testid="template-failed-destroy">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="flex items-center gap-2 text-sm leading-label font-medium">
              <TriangleAlert aria-hidden="true" className="size-4 shrink-0 text-destructive" />
              Destroy failed
            </span>
            <span className="text-meta text-muted-foreground">The destroy run stopped before it finished. Some resources may still exist.</span>
          </div>
          {failedRun && (
            <Link
              to={runPath(failedRun)}
              className={cn(buttonClass("outline"), "shrink-0 self-start pointer-coarse:h-11 md:self-auto")}
              data-testid="template-view-failed-run"
            >
              View run
            </Link>
          )}
        </div>
      )}
    </>
  );
}

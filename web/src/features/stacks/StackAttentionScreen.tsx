import { Check, Hourglass, Loader2, RefreshCw, TriangleAlert } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { useAttentionQuery } from "../../api/queries";
import type { AttentionItem } from "../../api/types";
import { useTenantID } from "../../auth/useTenantID";
import { formatDateTime } from "../../shared/formatTimestamp";
import PageHeader from "../../shared/PageHeader";
import PlanDiff from "../../shared/PlanDiff";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { attentionRunPath, failedOperation } from "./attention";
import { stackTemplateLabel } from "./stackWorkflow";
import { buttonClass } from "../../shared/buttonClass";
import RequestIdNote from "../../shared/RequestIdNote";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

const trail = [{ label: "Stacks", to: "/stacks" }];

// /stacks/attention: everything that needs a person, on every stack the person
// can view, by kind. The stacks index links here from its summary line.
// Waiting plans come first and newest first, as the API orders them.
//
// Built to openplan UI: PageHeader with a Breadcrumb trail, a section title
// per kind over a card of AttentionRows, and the page-level EmptyState.
export default function StackAttentionScreen() {
  const tenantID = useTenantID();
  const { data: items, status, error, refetch } = useAttentionQuery(tenantID);
  const boundary = useQueryErrorBoundary(error);

  if (status === "pending") {
    return (
      <section data-testid="attention-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading…
        </p>
      </section>
    );
  }

  if (status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="mx-auto grid max-w-260 justify-items-start gap-4" data-testid="attention-error">
        <PageHeader title="Needs attention" trail={trail} />
        <p className="text-muted-foreground">Something went wrong while loading what needs attention.</p>
        <RequestIdNote error={error} />
        <Button className="pointer-coarse:h-11" data-testid="attention-retry" onClick={() => refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  const waiting = items.filter((item) => item.kind === "waiting_approval");
  const failed = items.filter((item) => item.kind === "destroy_failed");
  const runFailed = items.filter((item) => item.kind === "run_failed");

  return (
    <section className="mx-auto max-w-260">
      <PageHeader title="Needs attention" trail={trail} />
      {items.length === 0 ? (
        <Empty className="gap-3 rounded-panel border border-solid bg-card px-5 py-14" data-testid="attention-empty">
          <EmptyHeader className="gap-3">
            <EmptyMedia>
              <Check aria-hidden="true" className="size-5 text-muted-foreground" />
            </EmptyMedia>
            <h2 className="text-label">Nothing needs attention</h2>
            <EmptyDescription className="text-meta">Nothing is waiting for approval and nothing has failed.</EmptyDescription>
          </EmptyHeader>
          <Link to="/stacks" className={cn(buttonClass("outline"), "pointer-coarse:h-11")}>
            Back to stacks
          </Link>
        </Empty>
      ) : (
        <div className="flex flex-col gap-7">
          {waiting.length > 0 && (
            <AttentionGroup id="waiting" icon={Hourglass} iconClass="text-warning" title="Waiting for approval">
              {waiting.map((item) => (
                <AttentionRow key={item.stack_template.id} item={item} />
              ))}
            </AttentionGroup>
          )}
          {failed.length > 0 && (
            <AttentionGroup id="failed" icon={TriangleAlert} iconClass="text-destructive" title="Destroy failed">
              {failed.map((item) => (
                <AttentionRow key={item.stack_template.id} item={item} />
              ))}
            </AttentionGroup>
          )}
          {runFailed.length > 0 && (
            <AttentionGroup id="run-failed" icon={TriangleAlert} iconClass="text-destructive" title="Plan or apply failed">
              {runFailed.map((item) => (
                <AttentionRow key={item.stack_template.id} item={item} />
              ))}
            </AttentionGroup>
          )}
        </div>
      )}
    </section>
  );
}

function AttentionGroup({
  id,
  icon: Icon,
  iconClass,
  title,
  children
}: {
  id: string;
  icon: LucideIcon;
  iconClass: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={`attention-${id}`} className="flex flex-col gap-3" data-testid={`attention-group-${id}`}>
      <h2 id={`attention-${id}`} className="flex items-center gap-2 text-section-title">
        <Icon aria-hidden="true" className={cn("size-4", iconClass)} />
        {title}
      </h2>
      <ul className="divide-y divide-divider overflow-hidden rounded-panel border bg-card">{children}</ul>
    </section>
  );
}

// The row's second line: who planned a waiting plan, or what a failure means
// for what the template manages. A failed plan changed nothing; a failed apply
// may have changed part of what it set out to.
function attentionMeaning(item: AttentionItem): string | null {
  switch (item.kind) {
    case "waiting_approval":
      return item.run && `Planned by ${item.run.trigger_actor_display_name || item.run.trigger_actor}`;
    case "destroy_failed":
      return "The destroy run stopped before it finished. Some resources may still exist.";
    case "run_failed":
      return failedOperation(item) === "apply"
        ? "The apply failed. Some changes may have been made."
        : "The plan failed. Nothing was changed.";
  }
}

// One item: what it is, why it needs a person, when, and the action that
// resolves it. The columns stack on a phone.
function AttentionRow({ item }: { item: AttentionItem }) {
  const waiting = item.kind === "waiting_approval";
  const runPath = attentionRunPath(item);
  const templatePath = `/stacks/${item.stack.id}/templates/${item.stack_template.id}`;
  const templateName = stackTemplateLabel(item.stack_template);

  return (
    <li className="flex flex-col gap-3 px-5 py-4 md:flex-row md:items-center md:gap-5" data-testid={`attention-row-${item.stack_template.id}`}>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {/* Named in full, since the separator between the two is hidden. */}
        <Link
          to={templatePath}
          aria-label={`${item.stack.name} / ${templateName}`}
          className="group flex min-w-0 flex-wrap items-baseline gap-x-1.5 text-foreground focus-visible:-outline-offset-2"
        >
          <span className="text-label transition-colors group-hover:text-primary">{item.stack.name}</span>
          <span aria-hidden="true" className="text-separator">
            /
          </span>
          <span className="font-mono text-meta text-code-foreground group-hover:text-primary group-hover:underline">
            {templateName}
          </span>
        </Link>
        <span className="text-meta text-muted-foreground">{attentionMeaning(item)}</span>
      </div>
      {waiting && <div className="md:w-37.5 md:shrink-0">{item.run?.plan_summary && <PlanDiff summary={item.run.plan_summary} />}</div>}
      <div className="text-meta text-muted-foreground md:w-37.5 md:shrink-0">
        {item.at && <time dateTime={item.at}>{formatDateTime(item.at)}</time>}
      </div>
      <div className="flex md:w-30 md:shrink-0 md:justify-end">
        {runPath && (
          <Link
            to={runPath}
            className={cn(buttonClass(waiting ? "primary" : "outline"), "pointer-coarse:h-11")}
            data-testid={`attention-action-${item.stack_template.id}`}
          >
            {waiting ? "Review plan" : "View run"}
          </Link>
        )}
      </div>
    </li>
  );
}

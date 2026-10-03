import { ArrowRight, Hourglass, Layers, Loader2, Plus, RefreshCw, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useAttentionQuery, useStacksQuery } from "../../api/queries";
import type { AttentionItem, StackListItem } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import {
  attentionByStack,
  attentionByStackTemplate,
  attentionSummary,
  failedChipLabel,
  templateCountLabel,
  waitingChipLabel
} from "./attention";
import type { StackAttention } from "./attention";
import StackPreview from "./StackPreview";
import { buttonClass } from "../../shared/buttonClass";
import Chip from "../../shared/Chip";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// /stacks: every stack the person can view, with the one selected shown beside
// the list, and one line above them when anything, on any stack, needs a
// person. The list is authz-filtered by the backend (AUTH-013), as is the
// attention list; the screen renders what they return and never filters for
// access itself.
//
// The selection lives in the URL (?stack=<id>), so a refresh or a shared link
// keeps it; without one, the first stack is selected.
//
// Built to openplan UI: PageHeader, AttentionSummary and SplitView below, with
// the shared SearchField, ListItem and Chip; PreviewPanel in StackPreview.

export default function StacksListScreen() {
  const { data: stacks, status, error, refetch } = useStacksQuery(tenantID);
  // The attention list only decorates this page, so while it loads or if it
  // fails the stacks still show, without the summary line and chips.
  const attention = useAttentionQuery(tenantID).data ?? [];
  const boundary = useQueryErrorBoundary(error);
  const [searchParams] = useSearchParams();
  const [filter, setFilter] = useState("");

  if (status === "pending") {
    return (
      <section data-testid="stacks-list-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading stacks…
        </p>
      </section>
    );
  }

  if (status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4" data-testid="stacks-list-error">
        <Breadcrumb items={[{ label: "Stacks" }]} className="mb-0" />
        <p className="text-muted-foreground">Something went wrong while loading stacks.</p>
        <Button className="pointer-coarse:h-11" data-testid="stacks-list-retry" onClick={() => refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  const query = filter.trim().toLowerCase();
  const visible = query === "" ? stacks : stacks.filter((stack) => stack.name.toLowerCase().includes(query) || stack.slug.toLowerCase().includes(query));
  // Chosen from the filtered list, so the preview never shows a stack the
  // list has hidden; with nothing left to show, there is no preview.
  const selected: StackListItem | undefined = visible.find((stack) => stack.id === searchParams.get("stack")) ?? visible[0];
  const byStack = attentionByStack(attention);

  return (
    <section>
      <PageHeader
        title="Stacks"
        count={stacks.length}
        action={
          <RequireCapability capability="canCreateStack">
            {/* A link that looks like the page's primary action. Full width
                on a phone, where the header stacks. */}
            <Link
              className={cn(buttonClass("primary", "lg"), "w-full pointer-coarse:h-11 md:w-auto")}
              to="/stacks/new"
              data-testid="create-stack-link"
            >
              <Plus data-icon="inline-start" aria-hidden="true" />
              Create stack
            </Link>
          </RequireCapability>
        }
      >
        <AttentionSummary items={attention} />
      </PageHeader>
      {stacks.length === 0 ? (
        // openplan UI's page-level EmptyState: a card, its one icon plain
        // rather than on a tile.
        <Empty className="gap-3 rounded-panel border border-solid bg-card px-5 py-14" data-testid="stacks-list-empty">
          <EmptyHeader className="gap-3">
            <EmptyMedia>
              <Layers aria-hidden="true" className="size-5 text-muted-foreground" />
            </EmptyMedia>
            <h2 className="font-heading text-sm font-medium tracking-tight">No stacks yet</h2>
            <EmptyDescription className="text-meta">No stacks visible to you yet.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        // One panel: the list, then the selected stack. On a wide screen they
        // sit side by side and the preview stays in view while the list
        // scrolls; clip rather than hidden overflow, so the sticky still works.
        <div className="flex flex-col overflow-clip rounded-panel border bg-card md:flex-row" data-testid="stacks-list">
          <div className="flex min-w-0 flex-col border-b md:w-90 md:shrink-0 md:border-r md:border-b-0">
            <div className="border-b border-divider p-3">
              <SearchField label="Filter stacks" value={filter} onChange={setFilter} testId="stacks-filter" />
            </div>
            {visible.length === 0 ? (
              <p className="px-5 py-4 text-meta text-muted-foreground" data-testid="stacks-filter-empty">
                No stacks match this filter.
              </p>
            ) : (
              <ul className="flex flex-col gap-0.5 p-2" aria-label="Stacks">
                {visible.map((stack) => (
                  <li key={stack.id}>
                    <StackRow stack={stack} selected={stack.id === selected?.id} attention={byStack.get(stack.id)} />
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="min-w-0 flex-1 md:sticky md:top-16 md:self-start">
            {selected && <StackPreview stack={selected} attention={attentionByStackTemplate(attention)} />}
          </div>
        </div>
      )}
    </section>
  );
}

// One line under the title that says what needs a person, linking to the page
// that lists it. Nothing at all when nothing does.
function AttentionSummary({ items }: { items: AttentionItem[] }) {
  const summary = attentionSummary(items);
  if (!summary) {
    return null;
  }
  return (
    <Link
      to="/stacks/attention"
      className="group inline-flex min-h-7 w-fit flex-wrap items-center gap-x-2 text-sm text-muted-foreground"
      data-testid="attention-summary"
    >
      <Hourglass aria-hidden="true" className="size-4 shrink-0 text-warning" />
      <span className="font-medium text-warning underline-offset-3 group-hover:underline">{summary.lead}</span>
      <span aria-hidden="true" className="text-separator">
        ·
      </span>
      <span>{summary.breakdown}</span>
      <ArrowRight
        aria-hidden="true"
        className="size-4 shrink-0 text-subtle-foreground transition-transform group-hover:translate-x-0.5 motion-reduce:transition-none"
      />
    </Link>
  );
}

// A stack in the list. Choosing it changes the preview, not the page, so it
// replaces the history entry rather than adding one.
function StackRow({ stack, selected, attention }: { stack: StackListItem; selected: boolean; attention: StackAttention | undefined }) {
  return (
    <Link
      to={`?stack=${encodeURIComponent(stack.id)}`}
      replace
      aria-current={selected ? "true" : undefined}
      className={listItemClass(selected)}
      data-testid={`stack-row-${stack.id}`}
    >
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate text-sm leading-label font-medium", selected && "text-primary-strong")} title={stack.name}>
          {stack.name}
        </span>
        <span className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span className="truncate font-mono">{stack.slug}</span>
          <span aria-hidden="true" className="text-separator">
            ·
          </span>
          <span className="shrink-0">{templateCountLabel(stack.template_count)}</span>
        </span>
      </span>
      {attention && (
        <span className="flex shrink-0 flex-col items-end gap-1">
          {attention.waiting > 0 && (
            <Chip tone="warning" icon={Hourglass}>
              {waitingChipLabel(attention.waiting)}
            </Chip>
          )}
          {attention.failed > 0 && (
            <Chip tone="destructive" icon={TriangleAlert}>
              {failedChipLabel(attention.failed)}
            </Chip>
          )}
        </span>
      )}
    </Link>
  );
}

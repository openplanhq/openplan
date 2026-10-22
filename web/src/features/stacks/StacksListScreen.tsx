import { ArrowRight, Hourglass, Layers, Loader2, Plus, RefreshCw, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useAttentionQuery, useStacksQuery } from "../../api/queries";
import type { AttentionItem, StackListItem } from "../../api/types";
import { useAuth } from "../../auth/AuthContext";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import ErrorLine from "../../shared/ErrorLine";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import {
  attentionByStack,
  attentionByStackTemplate,
  attentionSummary,
  failedChipLabel,
  templateCountWord,
  waitingChipLabel
} from "./attention";
import type { StackAttention } from "./attention";
import StackPreview from "./StackPreview";
import { buttonClass } from "../../shared/buttonClass";
import Chip from "../../shared/Chip";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import RequestIdNote from "../../shared/RequestIdNote";
import LoadMoreButton from "../../shared/LoadMoreButton";
import { useDebounce } from "../../shared/useDebounce";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// /stacks: every stack the person can view, with the one selected shown beside
// the list, and one line above them when anything, on any stack, needs a
// person. The list is authz-filtered by the backend (AUTH-013), as is the
// attention list; the screen renders what they return and never filters for
// access itself.
//
// The selection lives in the URL (?stack=<id>), so a refresh or a shared link
// keeps it; without one, the first stack is selected. A stack beyond the
// loaded pages can't be selected from a link yet: issue 404 on
// openplanhq/openplan.
//
// The list comes a page at a time, newest first, with Load more under it. The
// filter goes to the server as q, so it finds a stack on a page not loaded.
//
// Built to openplan UI: PageHeader, AttentionSummary and SplitView below, with
// the shared SearchField, ListItem and Chip; PreviewPanel in StackPreview.

// How long the filter waits for typing to pause before it asks the server.
const FILTER_DEBOUNCE_MS = 250;

export default function StacksListScreen() {
  const tenantID = useTenantID();
  const [filter, setFilter] = useState("");
  const query = useDebounce(filter.trim(), FILTER_DEBOUNCE_MS);
  const stacksQuery = useStacksQuery(tenantID, query);
  const { data: stacks, status, error, refetch } = stacksQuery;
  // The header counts every stack, which only the unfiltered list knows, and
  // only once all its pages are in: there is no total to ask for, since a
  // count of the stacks someone can see means checking every one.
  const allStacks = useStacksQuery(tenantID);
  const count = allStacks.data !== undefined && !allStacks.hasNextPage ? allStacks.data.length : undefined;
  // The attention list only decorates this page, so while it loads or if it
  // fails the stacks still show, without the summary line and chips.
  const attention = useAttentionQuery(tenantID).data ?? [];
  const boundary = useQueryErrorBoundary(error);
  const canCreateStack = useAuth().me?.global_capabilities.can_create_stack === true;
  const [searchParams] = useSearchParams();

  // The header stands while the stacks load and when they fail, so the page
  // keeps its title and its one action, and only the panel below waits.
  const header = (count?: number) => (
    <PageHeader
      title="Stacks"
      count={count}
      action={
        <RequireCapability capability="can_create_stack">
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
  );

  if (status === "pending") {
    return (
      <section>
        {header()}
        <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="stacks-list-loading">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading stacks…
        </p>
      </section>
    );
  }

  // A page after the first that fails leaves the stacks already shown; only a
  // list with nothing to show is replaced by the error.
  if (status === "error" && stacks === undefined) {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    // openplan UI's ErrorLine in place of the panel, an outline Retry under it.
    return (
      <section>
        {header()}
        <div className="flex flex-col items-start gap-3" data-testid="stacks-list-error">
          <ErrorLine>Something went wrong while loading stacks.</ErrorLine>
          <RequestIdNote error={error} />
          <button
            type="button"
            className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
            data-testid="stacks-list-retry"
            onClick={() => void refetch()}
          >
            <RefreshCw data-icon="inline-start" aria-hidden="true" />
            Retry
          </button>
        </div>
      </section>
    );
  }

  if (stacks === undefined) {
    return null;
  }
  // Chosen from the filtered list, so the preview never shows a stack the
  // list has hidden; with nothing left to show, there is no preview.
  const selected: StackListItem | undefined = stacks.find((stack) => stack.id === searchParams.get("stack")) ?? stacks[0];
  const byStack = attentionByStack(attention);
  // While a new filter loads, the last one's stacks stay on screen, so only an
  // empty answer to no filter at all means the tenant has no stacks.
  const noStacks = stacks.length === 0 && query === "" && !stacksQuery.isPlaceholderData;

  return (
    <section>
      {header(count)}
      {noStacks ? (
        // openplan UI's page-level EmptyState: a card, its one icon plain
        // rather than on a tile. It is the first screen of a new install, so
        // it says what a stack is for and offers the action that makes one;
        // someone who may not create one learns how one reaches them.
        <Empty className="gap-3 rounded-panel border border-solid bg-card px-5 py-14" data-testid="stacks-list-empty">
          <EmptyHeader className="gap-3">
            <EmptyMedia>
              <Layers aria-hidden="true" className="size-5 text-muted-foreground" />
            </EmptyMedia>
            <h2 className="font-heading text-label">No stacks yet</h2>
            <EmptyDescription className="text-meta">
              {canCreateStack
                ? "Create a stack for each environment you deploy to, such as prod or staging."
                : "Stacks appear here once someone gives you access to one."}
            </EmptyDescription>
          </EmptyHeader>
          {canCreateStack && (
            <Link to="/stacks/new" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} data-testid="stacks-empty-create">
              <Plus data-icon="inline-start" aria-hidden="true" />
              Create stack
            </Link>
          )}
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
            {stacks.length === 0 ? (
              <p className="px-5 py-4 text-meta text-muted-foreground" data-testid="stacks-filter-empty">
                No stacks match this filter.
              </p>
            ) : (
              <ul className="flex flex-col gap-0.5 p-2" aria-label="Stacks">
                {stacks.map((stack) => (
                  <li key={stack.id}>
                    <StackRow stack={stack} selected={stack.id === selected?.id} attention={byStack.get(stack.id)} />
                  </li>
                ))}
              </ul>
            )}
            {stacksQuery.hasNextPage && (
              <LoadMoreButton
                className="px-2 pb-3"
                noun="stacks"
                loading={stacksQuery.isFetchingNextPage}
                failed={stacksQuery.isFetchNextPageError}
                onClick={() => void stacksQuery.fetchNextPage()}
                testId="stacks-load-more"
              />
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
      className="group inline-flex min-h-7 w-fit flex-wrap items-center gap-x-2 text-body text-muted-foreground"
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
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex min-w-0 items-baseline gap-2">
          {/* The name never shrinks: it is only capped, short of the line
              when a slug follows, so the slug is what gets cut. */}
          <span
            className={cn(
              "shrink-0 truncate text-label",
              stack.slug_is_derived ? "max-w-full" : "max-w-beside-slug",
              selected && "text-primary-strong"
            )}
            title={stack.name}
          >
            {stack.name}
          </span>
          {/* A slug the name already gives would only repeat it. */}
          {!stack.slug_is_derived && (
            <span className="min-w-0 truncate font-mono text-caption text-muted-foreground" title={stack.slug}>
              {stack.slug}
            </span>
          )}
        </span>
        <TemplateCount templates={stack.template_count} selected={selected} />
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

// A list row's second line: how many templates the stack holds, the number set
// apart. An empty stack reads quieter, in subtle grey, except on the selected
// row, where subtle grey on primary-soft would fall under 4.5:1.
function TemplateCount({ templates, selected }: { templates: number; selected: boolean }) {
  const empty = templates === 0;
  return (
    <span
      className={cn("flex items-center gap-1.25 text-caption", empty && !selected ? "text-subtle-foreground" : "text-muted-foreground")}
      data-testid="stack-row-templates"
    >
      <Layers aria-hidden="true" className="size-3 shrink-0" strokeWidth={2.25} />
      {empty ? (
        "No templates"
      ) : (
        <span>
          <span className="font-medium text-foreground">{templates}</span> {templateCountWord(templates)}
        </span>
      )}
    </span>
  );
}

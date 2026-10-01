import { Layers, Loader2, Plus, RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { useStacksQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Button, buttonVariants } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// The list is authz-filtered by the backend (AUTH-013) — the screen renders
// whatever listStacks returns and never filters client-side.
//
// Until PR 8, body keeps the legacy text colour and base.css styles bare a
// and h2 from the legacy layer, so the screen sets its own colour and the
// links and heading set their own type and decoration.
export default function StacksListScreen() {
  const { data: stacks, status, error, refetch } = useStacksQuery(tenantID);
  const boundary = useQueryErrorBoundary(error);

  if (status === "pending") {
    return (
      <section className="text-foreground" data-testid="stacks-list-loading">
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
      <section className="grid justify-items-start gap-4 text-foreground" data-testid="stacks-list-error">
        <Breadcrumb items={[{ label: "Stacks" }]} className="mb-0" />
        <p className="text-muted-foreground">Something went wrong while loading stacks.</p>
        <Button className="pointer-coarse:h-11" data-testid="stacks-list-retry" onClick={() => refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  return (
    <section className="text-foreground">
      <header className="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
        <Breadcrumb items={[{ label: "Stacks" }]} className="mb-0" />
        <RequireCapability capability="canCreateStack">
          {/* A link that looks like the page's primary action. Full width on
              a phone, where the header stacks. */}
          <Link
            className={cn(buttonVariants(), "w-full no-underline pointer-coarse:h-11 md:w-auto")}
            to="/stacks/new"
            data-testid="create-stack-link"
          >
            <Plus data-icon="inline-start" aria-hidden="true" />
            Create stack
          </Link>
        </RequireCapability>
      </header>
      {stacks.length === 0 ? (
        <Empty className="border" data-testid="stacks-list-empty">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Layers aria-hidden="true" />
            </EmptyMedia>
            <h2 className="font-heading text-sm font-medium tracking-tight">No stacks yet</h2>
            <EmptyDescription>No stacks visible to you yet.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        // shadcn's bordered table frame; overflow-hidden clips the rows'
        // hover fill to its rounded corners.
        <div className="overflow-hidden rounded-lg border" data-testid="stacks-list">
          <Table>
            <colgroup>
              <col />
              <col className="w-48" />
            </colgroup>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Slug</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stacks.map((stack) => (
                <TableRow key={stack.id}>
                  {/* The link fills its cell, so the whole cell is the
                      target: 44px tall on a coarse pointer. Fixed layout
                      keeps the column's width whatever the name, so a long
                      one ends in an ellipsis, whole on hover. The focus
                      outline is drawn inside, where the frame can't clip it. */}
                  <TableCell className="p-0">
                    <Link
                      className="block truncate p-2 font-medium text-foreground no-underline hover:underline focus-visible:-outline-offset-2 pointer-coarse:py-3"
                      to={`/stacks/${stack.id}`}
                      title={stack.name}
                    >
                      {stack.name}
                    </Link>
                  </TableCell>
                  <TableCell className="truncate font-mono text-xs text-muted-foreground" title={stack.slug}>
                    {stack.slug}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  );
}

import { Fragment, useId, useState } from "react";
import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import type { PlanChangeAction, PlanResourceChange, TemplateRun } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import LoadError from "../../shared/LoadError";
import { pluralWord } from "../../shared/plural";
import LoadingLine from "../../shared/LoadingLine";
import DataTable from "../../shared/DataTable";
import { TableCell, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { ACTION_ORDER, actionLook, changeReason, changesNote, diffLines, isApplied } from "./planChanges";

// Every cell sets its text in meta itself, as the runs table's do, so the
// three share one baseline: the table's own 14px would put a 13px cell off its
// neighbours. Each keeps 16px at both sides, header included, rather than the
// table rule's 8px between columns: ChangeDetail's inset counts on it.
const cellClass = "px-4 py-3.5 text-meta whitespace-normal";

// How many rows show before Show more, per filter.
const CHANGES_PAGE = 50;

type Filter = "all" | PlanChangeAction;

interface RunChangesProps {
  run: TemplateRun;
  // Undefined until the list arrives; only a run with a list asks for one.
  changes: PlanResourceChange[] | undefined;
  failed: boolean;
  onRetry: () => void;
  // The run's Changes/Logs switch: the filter sits beside it, on one row.
  toolbar: ReactNode;
}

/**
 * A run's Changes view: every resource its plan changes, removals first, as
 * openplan UI's plan review lays it out. A filter by action appears once the
 * plan mixes kinds of action; each row opens on what changes in it. A run with
 * no list says why in one line, and points at the log when the log is the
 * only record.
 */
export default function RunChanges({ run, changes, failed, onRetry, toolbar }: RunChangesProps) {
  const [filter, setFilter] = useState<Filter>("all");
  const [shown, setShown] = useState(CHANGES_PAGE);
  const [open, setOpen] = useState<Record<string, boolean>>({});

  const note = changesNote(run);
  if (note !== null) {
    return (
      <RunViewFrame toolbar={toolbar}>
        <p className="text-meta text-muted-foreground">
          {note}
        </p>
      </RunViewFrame>
    );
  }
  if (changes === undefined) {
    return (
      <RunViewFrame toolbar={toolbar}>
        {failed ? (
          <LoadError onRetry={onRetry}>
            Something went wrong while loading this plan's changes.
          </LoadError>
        ) : (
          <LoadingLine>Loading changes…</LoadingLine>
        )}
      </RunViewFrame>
    );
  }
  if (changes.length === 0) {
    // A plan whose only changes are its outputs lists no resources.
    return (
      <RunViewFrame toolbar={toolbar}>
        <p className="text-meta text-muted-foreground">
          This plan changes no resources.
        </p>
      </RunViewFrame>
    );
  }

  const past = isApplied(run);
  const counts = new Map<string, number>();
  for (const change of changes) {
    counts.set(change.action, (counts.get(change.action) ?? 0) + 1);
  }
  const kinds = [...counts.keys()].sort((a, b) => order(a) - order(b));
  // The filter only repeats the count when every change is one kind.
  const filtered = filter === "all" ? changes : changes.filter((change) => change.action === filter);
  const visible = filtered.slice(0, shown);
  const total = run.plan_changes?.total ?? changes.length;

  function pick(next: Filter) {
    setFilter(next);
    setShown(CHANGES_PAGE);
  }

  return (
    <RunViewFrame
      toolbar={toolbar}
      filter={
        kinds.length > 1 && (
          <div role="group" aria-label="Show changes by action" className="flex flex-wrap gap-1.5">
          <FilterButton pressed={filter === "all"} onClick={() => pick("all")} label="All" count={changes.length} />
          {kinds.map((kind) => {
            const look = actionLook(kind);
            const word = past ? look.past : look.present;
            return (
              <FilterButton
                key={kind}
                pressed={filter === kind}
                onClick={() => pick(kind)}
                glyph={look.glyph}
                glyphTone={look.tone}
                label={word.charAt(0).toUpperCase() + word.slice(1)}
                  count={counts.get(kind) ?? 0}
                />
              );
            })}
          </div>
        )
      }
    >
      {run.plan_changes?.state === "truncated" && (
        <p className="text-meta text-muted-foreground">
          Showing the first {changes.length} of {total} changes; the plan log has them all.
        </p>
      )}
      <DataTable columns={["w-30", undefined, "w-70"]} headings={["Action", "Resource", "Why"]} cellClass="px-4">
        {visible.map((change) => {
          const key = `${change.address}#${change.deposed}`;
          return (
            <ChangeRow
              key={key}
              change={change}
              past={past}
              open={open[key] ?? false}
              onToggle={() => setOpen((current) => ({ ...current, [key]: !current[key] }))}
            />
          );
        })}
      </DataTable>
      {filtered.length > shown && (
        <button type="button" className={cn(buttonClass("quiet"), "self-start")} onClick={() => setShown(shown + CHANGES_PAGE)}>
          Show {Math.min(CHANGES_PAGE, filtered.length - shown)} more
        </button>
      )}
    </RunViewFrame>
  );
}

/**
 * A run view under its toolbar: the Changes/Logs switch at the left and, on
 * Changes, the filter at the right, wrapping under it on a narrow panel; then
 * the view, 12px below. Logs uses it too, so switching never moves the switch.
 */
export function RunViewFrame({ toolbar, filter, children }: { toolbar: ReactNode; filter?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        {toolbar}
        {filter}
      </div>
      {children}
    </div>
  );
}

function order(action: string): number {
  const index = ACTION_ORDER.indexOf(action);
  return index === -1 ? ACTION_ORDER.length : index;
}

function FilterButton({
  pressed,
  onClick,
  label,
  count,
  glyph,
  glyphTone
}: {
  pressed: boolean;
  onClick: () => void;
  label: string;
  count: number;
  glyph?: string;
  glyphTone?: string;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={onClick}
      className={cn(
        "inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-meta font-medium transition-colors duration-150",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring pointer-coarse:h-11",
        pressed ? "border-primary/35 bg-primary-soft text-primary-strong" : "border-border bg-card text-foreground hover:bg-muted"
      )}
    >
      {glyph && (
        <span aria-hidden="true" className={cn("font-mono", glyphTone)}>
          {glyph}
        </span>
      )}
      {label}
      <span className="text-caption text-subtle-foreground">{count}</span>
    </button>
  );
}

function ChangeRow({ change, past, open, onToggle }: { change: PlanResourceChange; past: boolean; open: boolean; onToggle: () => void }) {
  const detailId = useId();
  const look = actionLook(change.action);
  const word = past ? look.past : look.present;
  return (
    <Fragment>
      {/* The whole row toggles for a pointer; the address is the button a
          keyboard and a screen reader reach, so the row is never the only
          control. */}
      <TableRow className="h-12 cursor-pointer border-divider" onClick={onToggle}>
        <TableCell className={cellClass}>
          <span className={cn("inline-flex gap-2", look.tone, look.removes && "font-medium")}>
            <span aria-hidden="true" className="w-2 font-mono">
              {look.glyph}
            </span>
            {word}
          </span>
        </TableCell>
        <TableCell className={cellClass}>
          <button
            type="button"
            aria-expanded={open}
            aria-controls={open ? detailId : undefined}
            onClick={(event) => {
              event.stopPropagation();
              onToggle();
            }}
            className="inline-flex max-w-full items-baseline gap-1.5 text-left font-mono text-meta text-foreground hover:text-primary focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring"
          >
            <ChevronRight
              aria-hidden="true"
              strokeWidth={2.25}
              className={cn("size-3.5 shrink-0 self-center text-subtle-foreground transition-transform duration-150", open && "rotate-90")}
            />
            <span className="min-w-0 wrap-anywhere">{change.address}</span>
          </button>
        </TableCell>
        <TableCell className={cn(cellClass, "text-muted-foreground")}>{changeReason(change)}</TableCell>
      </TableRow>
      {open && (
        <TableRow className="border-divider bg-canvas hover:bg-canvas">
          <TableCell colSpan={3} className="p-0">
            <ChangeDetail id={detailId} change={change} />
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}

// tofu's signs, said for a screen reader, which would read "+" as "plus".
const SIGN_WORDS = { "+": "added", "-": "removed", "~": "changed", " ": "" } as const;

// What changes in one resource, in tofu's own notation, lined up under the
// Resource column.
function ChangeDetail({ id, change }: { id: string; change: PlanResourceChange }) {
  const { lines, unchanged } = diffLines(change);
  return (
    <div id={id} className="py-3 pr-4 pl-39 font-mono text-log whitespace-pre-wrap text-code-foreground">
      {lines.length === 0 && unchanged === 0 && <p>No attributes to show.</p>}
      {lines.map((line) => (
        <p key={line.key} className="wrap-anywhere">
          <span aria-hidden="true">{line.sign} </span>
          <span className="sr-only">{SIGN_WORDS[line.sign]} </span>
          {line.key} = {line.text}
          {line.forcesReplacement && <span className="text-destructive"> # forces replacement</span>}
        </p>
      ))}
      {unchanged > 0 && (
        <p className="text-muted-foreground">
          # ({unchanged} unchanged {pluralWord(unchanged, "attribute", "attributes")} hidden)
        </p>
      )}
    </div>
  );
}

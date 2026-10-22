import { useId, useState } from "react";
import type { ReactNode } from "react";
import { ChevronRight, TriangleAlert } from "lucide-react";
import { buttonClass } from "./buttonClass";
import { cn } from "@/lib/utils";

// openplan UI's ErrorPanel: a failure that needs more than one line, such as a
// sync or a registration that the server refused. It is drawn as a Notice is,
// a bordered block of its own, so it can never be read as part of the text
// beside it: a headline after a destructive triangle, then what to do next,
// then the server's own words. Those sit straight inside the one border, in
// mono under the headline: a second box inside the first read as clutter
// (owner, 2026-10-21).
//
// Today the server only sends its raw error, so callers pass a generic
// headline and the raw text as details, shown open. Once failures carry a
// cause and a next step (issue 455), the details move behind "Show details"
// with detailsCollapsed, and the headline and next step say it in words.
//
// It is announced (role="alert") when it appears, as anything that just
// happened is.
export default function ErrorPanel({
  title,
  children,
  details,
  detailsCollapsed = false,
  testId
}: {
  /** What failed, in a few words: "Sync failed". */
  title: ReactNode;
  /** The next step, in one or two sentences. */
  children?: ReactNode;
  /** The server's own words, as written. */
  details?: string;
  /** Hide the details behind "Show details" until asked for. */
  detailsCollapsed?: boolean;
  testId?: string;
}) {
  const [open, setOpen] = useState(!detailsCollapsed);
  const detailsId = useId();
  const hasDetails = details !== undefined && details.trim() !== "";

  return (
    <div role="alert" className="flex min-w-0 items-start gap-3 rounded-lg border px-5 py-4" data-testid={testId}>
      <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="text-label text-foreground">{title}</p>
        {children !== undefined && <p className="text-meta text-muted-foreground">{children}</p>}
        {hasDetails && detailsCollapsed && (
          <button
            type="button"
            className={cn(buttonClass("quiet"), "mt-1 self-start")}
            aria-expanded={open}
            aria-controls={detailsId}
            onClick={() => setOpen((value) => !value)}
          >
            <ChevronRight data-icon="inline-start" aria-hidden="true" className={cn("size-3.5 transition-transform", open && "rotate-90")} />
            {open ? "Hide details" : "Show details"}
          </button>
        )}
        {hasDetails && open && (
          <pre
            id={detailsId}
            className="m-0 mt-1 font-mono text-log whitespace-pre-wrap text-code-foreground wrap-anywhere"
          >
            {details}
          </pre>
        )}
      </div>
    </div>
  );
}

import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Breakable, piecesAfter } from "./breakAfter";

// A link out of openplan, as openplan UI draws one: the value it names in the
// foreground, a small arrow, and words for a screen reader saying where it
// goes and that it opens a new tab. Machine values (a repository, a commit)
// are mono; words are not.
//
// A long value wraps only after a "/" or "-", so a line never ends inside a
// name, and is never cut: the whole value is what someone copies or checks.
// Its last piece is glued to the arrow, so the arrow never starts a line on
// its own. Only a single piece wider than the column breaks where it must.
export default function ExternalLink({
  href,
  site,
  term,
  mono = true,
  testId,
  className,
  children
}: {
  href: string;
  /** Where the link goes, as a person names it: "GitHub". */
  site: string;
  /** What the value is, for a screen reader, when an icon says it on screen. */
  term?: string;
  mono?: boolean;
  testId?: string;
  /** Overrides the meta size where the line around it is set smaller. */
  className?: string;
  children: string;
}) {
  const pieces = piecesAfter(children, "/-");
  const last = pieces.pop() ?? "";
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      data-testid={testId}
      className={cn("min-w-0 text-meta text-foreground wrap-anywhere hover:text-primary hover:underline", mono && "font-mono", className)}
    >
      {term && <span className="sr-only">{term}: </span>}
      <Breakable pieces={pieces} />
      {pieces.length > 0 && <wbr />}
      <span className="whitespace-nowrap">
        {last}
        <ArrowUpRight aria-hidden="true" strokeWidth={2.25} className="relative top-px ml-1 inline size-3 text-subtle-foreground" />
      </span>
      <span className="sr-only">, on {site} (opens in a new tab)</span>
    </a>
  );
}

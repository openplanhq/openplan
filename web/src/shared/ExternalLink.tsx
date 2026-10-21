import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A link out of openplan, as openplan UI draws one: the value it names in the
// foreground, a small arrow, and words for a screen reader saying where it
// goes and that it opens a new tab. Machine values (a repository, a commit)
// are mono; words are not. A truncated one keeps its arrow in view.
export default function ExternalLink({
  href,
  site,
  mono = true,
  truncate = false,
  testId,
  children
}: {
  href: string;
  /** Where the link goes, as a person names it: "GitHub". */
  site: string;
  mono?: boolean;
  /** Keep a long value on one line, cut with an ellipsis and whole in a
      title. Pass the value as a string. */
  truncate?: boolean;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      data-testid={testId}
      title={truncate && typeof children === "string" ? children : undefined}
      className={cn(
        "inline-flex max-w-full items-center gap-1 text-meta text-foreground hover:text-primary hover:underline",
        truncate ? "min-w-0" : "wrap-anywhere",
        mono && "font-mono"
      )}
    >
      {truncate ? <span className="min-w-0 truncate">{children}</span> : children}
      <ArrowUpRight aria-hidden="true" strokeWidth={2.25} className="size-3 shrink-0 text-subtle-foreground" />
      <span className="sr-only">, on {site} (opens in a new tab)</span>
    </a>
  );
}

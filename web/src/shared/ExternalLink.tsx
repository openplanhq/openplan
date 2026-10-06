import { ArrowUpRight } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// A link out of openplan, as openplan UI draws one: the value it names in the
// foreground, a small arrow, and words for a screen reader saying where it
// goes and that it opens a new tab. Machine values (a repository, a commit)
// are mono; words are not.
export default function ExternalLink({
  href,
  site,
  mono = true,
  testId,
  children
}: {
  href: string;
  /** Where the link goes, as a person names it: "GitHub". */
  site: string;
  mono?: boolean;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      data-testid={testId}
      className={cn(
        "inline-flex max-w-full items-center gap-1 text-meta text-foreground wrap-anywhere hover:text-primary hover:underline",
        mono && "font-mono"
      )}
    >
      {children}
      <ArrowUpRight aria-hidden="true" strokeWidth={2.25} className="size-3 shrink-0 text-subtle-foreground" />
      <span className="sr-only">, on {site} (opens in a new tab)</span>
    </a>
  );
}

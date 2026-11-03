import { cn } from "@/lib/utils";

// A revision's commit subject, as its author wrote it: one line, cut with an
// ellipsis, whole on hover. Nothing at all when the commit has none, so no
// blank line or empty tooltip stands in for it. dir="auto" isolates it, so a
// right-to-left subject can't reorder the commit printed beside it.
export default function RevisionSubject({ subject, className }: { subject: string; className?: string }) {
  if (!subject) {
    return null;
  }
  return (
    <span data-testid="revision-subject" dir="auto" title={subject} className={cn("block min-w-0 truncate text-meta font-medium text-foreground", className)}>
      {subject}
    </span>
  );
}

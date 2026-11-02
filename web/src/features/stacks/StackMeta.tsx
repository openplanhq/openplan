import type { Stack } from "../../api/types";

// The line under a stack's name: its slug, then its tags as written, never
// coloured by their value. A slug derived from the name only repeats it, so it
// is left out; with no tags either, the line goes.
export default function StackMeta({ stack }: { stack: Pick<Stack, "slug" | "slug_is_derived" | "tags"> }) {
  const tags = Object.entries(stack.tags);
  if (stack.slug_is_derived && tags.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-wrap items-center gap-2">
      {!stack.slug_is_derived && <span className="font-mono text-caption text-muted-foreground">{stack.slug}</span>}
      {tags.map(([key, value]) => (
        <span key={key} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-caption text-tag-foreground">
          {key}: {value}
        </span>
      ))}
    </div>
  );
}

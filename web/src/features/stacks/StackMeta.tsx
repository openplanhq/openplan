import type { Stack } from "../../api/types";

// The line under a stack's name: its slug, then its tags as written, never
// coloured by their value. hideSlug leaves the slug out, for a slug that only
// repeats the name.
export default function StackMeta({ stack, hideSlug = false }: { stack: Pick<Stack, "slug" | "tags">; hideSlug?: boolean }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {!hideSlug && <span className="font-mono text-caption text-muted-foreground">{stack.slug}</span>}
      {Object.entries(stack.tags).map(([key, value]) => (
        <span key={key} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-caption text-tag-foreground">
          {key}: {value}
        </span>
      ))}
    </div>
  );
}

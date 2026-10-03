import type { Stack } from "../../api/types";

// The line under a stack's name: its slug, then its tags as written, never
// coloured by their value.
export default function StackMeta({ stack }: { stack: Pick<Stack, "slug" | "tags"> }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="font-mono text-xs text-muted-foreground">{stack.slug}</span>
      {Object.entries(stack.tags).map(([key, value]) => (
        <span key={key} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-xs text-tag-foreground">
          {key}: {value}
        </span>
      ))}
    </div>
  );
}

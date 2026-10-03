import { cn } from "@/lib/utils";

/**
 * openplan UI's ListItem: a selectable row in a split view's list, at least
 * 56px tall. Selected, it takes the soft primary fill and border; otherwise
 * it tints on hover. The focus ring is drawn inside, where the list's frame
 * cannot clip it.
 */
export function listItemClass(selected: boolean): string {
  return cn(
    "flex min-h-14 items-center justify-between gap-3 rounded-lg border border-transparent px-3 py-2 text-foreground transition-colors focus-visible:-outline-offset-2",
    selected ? "border-primary/35 bg-primary-soft" : "hover:bg-primary-tint"
  );
}

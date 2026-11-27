import { cn } from "@/lib/utils";

// The quiet mark between pieces of meta text: "·" between facts, "/" in a
// trail. Hidden from screen readers, which read the pieces on their own.
// spaced pads it with spaces, for inline text whose parent has no flex gap.
export default function Separator({ glyph = "·", spaced = false, className }: { glyph?: "·" | "/"; spaced?: boolean; className?: string }) {
  return (
    <span aria-hidden="true" className={cn("text-separator", className)}>
      {spaced ? ` ${glyph} ` : glyph}
    </span>
  );
}

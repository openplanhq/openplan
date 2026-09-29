import type { ComponentProps } from "react";
import { Badge } from "@/components/ui/badge";
import { statusGlyph } from "./statusTone";
import type { StatusTone } from "./statusTone";

type BadgeVariant = NonNullable<ComponentProps<typeof Badge>["variant"]>;

// The tone table in docs/superpowers/specs/2026-09-29-shadcn-adoption-design.md.
const VARIANTS: Record<StatusTone, BadgeVariant> = {
  settled: "success",
  progress: "progress",
  waiting: "warning",
  failed: "destructive",
  canceled: "muted"
};

type StatusBadgeProps = Omit<ComponentProps<typeof Badge>, "variant"> & { tone: StatusTone };

// A status pill. The glyph and the words say what the colour says, so colour
// is never the only signal. Callers choose the tone, usually
// statusTone(value), and the words.
export default function StatusBadge({ tone, children, ...props }: StatusBadgeProps) {
  return (
    <Badge variant={VARIANTS[tone]} data-tone={tone} {...props}>
      <span aria-hidden="true" className={tone === "progress" ? "motion-safe:animate-pulse" : undefined}>
        {statusGlyph(tone)}
      </span>
      {children}
    </Badge>
  );
}

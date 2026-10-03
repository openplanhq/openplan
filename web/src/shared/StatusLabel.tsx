import type { LucideIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

// What a state asks of a person, which is what its colour says. A settled state
// stays grey so the ones that need someone are what draws the eye.
export type StatusLabelTone = "settled" | "idle" | "attention" | "failed";

/** A state as a StatusLabel draws it: its word, its icon, its tone, and
    whether it needs a person now. */
export interface StatusIndicator {
  label: string;
  icon: LucideIcon;
  tone: StatusLabelTone;
  strong: boolean;
}

const TONE_CLASSES: Record<StatusLabelTone, string> = {
  settled: "text-muted-foreground",
  idle: "text-subtle-foreground",
  attention: "text-warning",
  failed: "text-destructive"
};

type StatusLabelProps = ComponentProps<"span"> & {
  icon: LucideIcon;
  tone: StatusLabelTone;
  /** Medium weight, for a state that needs a person now. */
  strong?: boolean;
};

// A state as an icon and a word, with no pill and no fill: openplan UI's
// StatusLabel. The word carries the meaning, so the icon is decorative, and
// colour is never the only signal.
export default function StatusLabel({ icon: Icon, tone, strong = false, className, children, ...props }: StatusLabelProps) {
  return (
    <span
      data-tone={tone}
      className={cn("inline-flex items-center gap-1.75 text-meta whitespace-nowrap", TONE_CLASSES[tone], strong && "font-medium", className)}
      {...props}
    >
      <Icon aria-hidden="true" className="size-3.5 shrink-0" strokeWidth={2.25} />
      {children}
    </span>
  );
}

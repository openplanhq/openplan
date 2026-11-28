import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// openplan UI's EmptyState: what a place shows when it has nothing in it, a
// title saying what is missing, a sentence saying what to do or why it is
// fine, and the one action that fills it.
//
// "panel" is the dashed outline inside a panel or tab; "page" is the card
// that is a page's only content, with an icon above the title. Parts sit
// space-3 apart, or space-2 in a panel with no action to offer. short is for
// one under a form that fills it.
export default function EmptyState({
  title,
  titleAs: Title = "p",
  description,
  action,
  variant = "panel",
  icon: Icon,
  short = false,
  className,
}: {
  title: ReactNode;
  titleAs?: "h2" | "h3" | "h4" | "p";
  description?: ReactNode;
  action?: ReactNode;
  variant?: "panel" | "page";
  icon?: LucideIcon;
  short?: boolean;
  className?: string;
}) {
  const gap = variant === "page" || action ? "gap-3" : "gap-2";
  return (
    <Empty
      className={cn(
        gap,
        variant === "page" ? "rounded-panel border border-solid bg-card px-5 py-14" : cn("rounded-lg border border-dashed border-dashed-border px-5", short ? "py-7" : "py-10"),
        className
      )}
    >
      <EmptyHeader className={gap}>
        {Icon && (
          <EmptyMedia>
            <Icon aria-hidden="true" className="size-5 text-muted-foreground" />
          </EmptyMedia>
        )}
        <Title className="text-label">{title}</Title>
        {description && <EmptyDescription className="text-meta">{description}</EmptyDescription>}
      </EmptyHeader>
      {action}
    </Empty>
  );
}

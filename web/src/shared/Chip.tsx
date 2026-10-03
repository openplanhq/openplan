import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const FILLS = { warning: "bg-warning-soft", destructive: "bg-destructive-soft" } as const;

// openplan UI's Chip: a small filled marker on a parent row saying something
// inside it needs a person. Never for a settled state.
export default function Chip({ tone, icon: Icon, children }: { tone: keyof typeof FILLS; icon: LucideIcon; children: ReactNode }) {
  return (
    <Badge variant={tone} className={cn("gap-1.25 rounded-sm", FILLS[tone])}>
      <Icon aria-hidden="true" strokeWidth={2.25} />
      {children}
    </Badge>
  );
}

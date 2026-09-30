import type { ComponentProps } from "react";
import { Badge, type BadgeVariant } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

// Roles share the status tints; owner takes progress's primary blue. This is a
// Map, not an object, so a role named like an Object.prototype property
// ("constructor") can't find one.
const VARIANTS = new Map<string, BadgeVariant>([
  ["owner", "progress"],
  ["operator", "success"],
  ["approver", "warning"],
  ["viewer", "muted"]
]);

// stackRole, not role, which would shadow the ARIA attribute Badge passes on.
type RoleBadgeProps = Omit<ComponentProps<typeof Badge>, "variant" | "children"> & { stackRole: string };

// A stack role. The API types a role as a string, so one the UI doesn't know
// yet still renders, named and muted. min-w-20 fits the longest role, so a
// column of badges lines up.
export default function RoleBadge({ stackRole, className, ...props }: RoleBadgeProps) {
  return (
    <Badge
      variant={VARIANTS.get(stackRole) ?? "muted"}
      data-role={stackRole}
      className={cn("min-w-20", className)}
      {...props}
    >
      {stackRole}
    </Badge>
  );
}

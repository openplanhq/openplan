import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { roleDefinition } from "./roles";
import type { RoleDefinition } from "./roles";

type RoleLabelProps = ComponentProps<"span"> & {
  roles: readonly RoleDefinition[];
  role: string;
};

// A role as an icon and a lowercase word, drawn as a StatusLabel is: no pill
// and no tint. A role is settled, so it stays grey; colour is kept for what
// needs a person. The words sit on the baseline and the icon is centred on
// them, so a role lines up with the text beside it.
export default function RoleLabel({ roles, role, className, children, ...props }: RoleLabelProps) {
  const { icon: Icon } = roleDefinition(roles, role);
  return (
    <span data-role={role} className={cn("inline-flex items-baseline gap-1.75 text-meta whitespace-nowrap", className)} {...props}>
      <Icon aria-hidden="true" strokeWidth={2.25} className="size-3.5 shrink-0 self-center text-muted-foreground" />
      {children ?? role}
    </span>
  );
}

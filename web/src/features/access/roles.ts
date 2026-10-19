import { CircleHelp, Eye, Pencil, Shield, Stamp, Wrench } from "lucide-react";
import type { LucideIcon } from "lucide-react";

/** One role a grant can hold: its word, what it adds to the role below it,
    and the icon a RoleLabel draws it with. */
export interface RoleDefinition {
  value: string;
  description: string;
  icon: LucideIcon;
}

// Each list is a ladder, highest first, as the authorization model defines it
// (internal/authorization/authorization-model.fga): each role includes the one
// below. The descriptions say what a role adds, so a person who isn't an
// infrastructure specialist can read the ladder off the list.
export const STACK_ROLES = [
  { value: "owner", description: "Manages who has access, and does what an approver does.", icon: Shield },
  { value: "approver", description: "Approves plans, and does what an operator does.", icon: Stamp },
  { value: "operator", description: "Plans, applies and edits variables.", icon: Wrench },
  { value: "viewer", description: "Sees the stack, its runs and their logs.", icon: Eye }
] as const satisfies readonly RoleDefinition[];

// The platform's grantable roles (#424). Root sits above admin but is set by
// OPENPLAN_ROOT_SUBJECT on the server, never granted, so it isn't here.
export const PLATFORM_ROLES = [
  { value: "admin", description: "Manages platform access and every stack’s access, and does what an editor does.", icon: Shield },
  { value: "editor", description: "Creates stacks and publishes templates, and sees what a viewer sees.", icon: Pencil },
  { value: "viewer", description: "Sees every stack and template.", icon: Eye }
] as const satisfies readonly RoleDefinition[];

export type StackRole = (typeof STACK_ROLES)[number]["value"];

/** The definition of value in roles. The API types a role as a string, so one
    the UI doesn't know yet still has a definition: its raw word, and an icon
    that says it's unknown. */
export function roleDefinition(roles: readonly RoleDefinition[], value: string): RoleDefinition {
  return roles.find((role) => role.value === value) ?? { value, description: "", icon: CircleHelp };
}

/** Where value sits on the ladder: 0 is the top. An unknown role sorts last. */
export function roleRank(roles: readonly RoleDefinition[], value: string): number {
  const index = roles.findIndex((role) => role.value === value);
  return index === -1 ? roles.length : index;
}

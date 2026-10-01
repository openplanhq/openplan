// The stack roles, in the order pickers list them. The role picker, the style
// guide and RoleBadge all read this, so adding or renaming a role is one edit.
export const STACK_ROLES = [
  { value: "owner", label: "Owner" },
  { value: "operator", label: "Operator" },
  { value: "approver", label: "Approver" },
  { value: "viewer", label: "Viewer" }
] as const;

export type StackRole = (typeof STACK_ROLES)[number]["value"];

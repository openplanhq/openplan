import RoleLabel from "./RoleLabel";
import { ROOT_ROLE } from "./roles";

/**
 * Root at the top of the platform's ladder: locked, with no role menu and no
 * Remove, since the server sets it and the API never lists it as a grant.
 * Root sees its own name here, marked as GrantRow marks you; anyone else sees
 * only "Root" (the owner cut a line naming OPENPLAN_ROOT_SUBJECT, 2026-10-19).
 */
export default function RootRow({ me }: { me?: { display_name: string; email?: string } }) {
  return (
    <li className="border-t border-divider first:border-t-0" data-testid="grant-root">
      <div className="flex min-h-14 items-center gap-4 py-2 pr-3 pl-5">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex min-w-0 items-baseline">
            <span className="truncate text-label text-foreground">{me ? me.display_name : "Root"}</span>
            {me && <span className="shrink-0 text-label font-normal whitespace-pre text-muted-foreground">{" (you)"}</span>}
          </span>
          {me?.email && <span className="truncate text-caption text-muted-foreground">{me.email}</span>}
        </div>
        {/* A quiet RoleSelect pulls back by its own padding, so a plain label
            at the cell's edge lines up with the roles below. */}
        <div className="w-55 shrink-0">
          <RoleLabel roles={ROOT_ROLE} role="root" />
        </div>
        <div className="w-18 shrink-0" />
      </div>
    </li>
  );
}

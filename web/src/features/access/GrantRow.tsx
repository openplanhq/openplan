import { LoaderCircle, Trash2 } from "lucide-react";
import type { GrantView } from "../../api/types";
import ErrorLine from "../../shared/ErrorLine";
import { buttonClass } from "../../shared/buttonClass";
import { cn } from "@/lib/utils";
import RoleSelect from "./RoleSelect";
import type { RoleDefinition } from "./roles";

/**
 * One person's grant: who they are, their role as a quiet RoleSelect, and
 * Remove. Both act at once: nothing asks first and nothing offers Undo
 * (owner, 2026-10-19), so a removed person simply leaves the list.
 */
export default function GrantRow({
  roles,
  grant,
  isMe,
  locked = false,
  rolePending = false,
  busy = false,
  error,
  onRoleChange,
  onRemove
}: {
  roles: readonly RoleDefinition[];
  grant: GrantView;
  isMe: boolean;
  /** The role and Remove can't change, such as for a stack's only owner. */
  locked?: boolean;
  /** A role change is saving. */
  rolePending?: boolean;
  /** A remove is running. */
  busy?: boolean;
  error?: string;
  onRoleChange: (role: string) => void;
  onRemove: () => void;
}) {
  // The API names someone it has no record of by their subject. Say so, and
  // keep the subject, which is the only handle on them.
  const unknown = grant.display_name === grant.user_sub;
  const name = unknown ? "Unknown person" : grant.display_name;
  const possessive = isMe ? "your" : `${name}’s`;

  return (
    <li className="border-t border-divider first:border-t-0" data-testid={`grant-${grant.user_sub}`}>
      <div className="flex min-h-14 items-center gap-4 py-2 pr-3 pl-5">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex min-w-0 items-baseline">
            <span title={name} className={cn("truncate text-sm leading-label font-medium", unknown ? "text-muted-foreground" : "text-foreground")}>
              {name}
            </span>
            {/* "Name (you)", the way share lists mark you (owner's pick,
                2026-10-19): the name's size, normal weight, muted, joined
                by a plain space so it reads as part of the name. It never
                shrinks, so a long name is cut before it. */}
            {isMe && <span className="shrink-0 text-sm leading-label whitespace-pre text-muted-foreground">{" (you)"}</span>}
          </span>
          {unknown ? (
            <span title={grant.user_sub} className="truncate font-mono text-xs text-muted-foreground">
              {grant.user_sub}
            </span>
          ) : (
            grant.email && (
              <span title={grant.email} className="truncate text-xs text-muted-foreground">
                {grant.email}
              </span>
            )
          )}
        </div>

        <div className="w-55 shrink-0">
          <RoleSelect
            roles={roles}
            value={grant.role}
            onChange={onRoleChange}
            label={isMe ? "Your role" : `${name}’s role`}
            disabled={locked || busy}
            pending={rolePending}
            testId={`role-${grant.user_sub}`}
          />
        </div>
        <div className="flex w-18 shrink-0 justify-end">
          {/* Not disabled while the remove runs, so focus stays on it; a
              second press is ignored instead, and the icon shows the work. */}
          <button
            type="button"
            className={cn(buttonClass("icon"), "pointer-coarse:size-11")}
            aria-label={`Remove ${possessive} access`}
            disabled={locked || rolePending}
            aria-disabled={busy || undefined}
            onClick={() => !busy && onRemove()}
            data-remove
          >
            {busy ? (
              <LoaderCircle aria-hidden="true" strokeWidth={2.25} className="size-3.5 animate-spin" />
            ) : (
              <Trash2 aria-hidden="true" strokeWidth={2.25} className="size-3.5" />
            )}
          </button>
        </div>
      </div>
      {error && (
        <div className="pr-3 pb-3 pl-5">
          <ErrorLine>{error}</ErrorLine>
        </div>
      )}
    </li>
  );
}

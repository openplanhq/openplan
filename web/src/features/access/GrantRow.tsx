import { useEffect, useRef } from "react";
import { LoaderCircle, Minus, Trash2, TriangleAlert, Undo2 } from "lucide-react";
import type { GrantView } from "../../api/types";
import ErrorLine from "../../shared/ErrorLine";
import { buttonClass } from "../../shared/buttonClass";
import { cn } from "@/lib/utils";
import RoleSelect from "./RoleSelect";
import type { RoleDefinition } from "./roles";

/** Where a row is: as it stands, asking to confirm a change, or removed with Undo beside it. */
export type GrantRowMode = "idle" | "confirm" | "removed";

/**
 * One person's grant: who they are, their role as a quiet RoleSelect, and
 * Remove. The row confirms in place, keeping its height, and stays after a
 * remove with Undo beside it, so feedback sits where the change was made.
 */
export default function GrantRow({
  roles,
  grant,
  isMe,
  mode,
  lockReason,
  rolePending = false,
  busy = false,
  question,
  confirmLabel,
  warning,
  error,
  canUndo = true,
  onRoleChange,
  onRemove,
  onConfirm,
  onCancel,
  onUndo
}: {
  roles: readonly RoleDefinition[];
  grant: GrantView;
  isMe: boolean;
  mode: GrantRowMode;
  /** Why the role and Remove are locked, such as being the only owner. */
  lockReason?: string;
  /** A role change is saving. */
  rolePending?: boolean;
  /** A remove or an Undo is running. */
  busy?: boolean;
  question?: string;
  confirmLabel?: string;
  /** What the person loses, when the change takes their own access. */
  warning?: string;
  error?: string;
  canUndo?: boolean;
  onRoleChange: (role: string) => void;
  onRemove: () => void;
  onConfirm: () => void;
  onCancel: () => void;
  onUndo: () => void;
}) {
  // The API names someone it has no record of by their subject. Say so, and
  // keep the subject, which is the only handle on them.
  const unknown = grant.display_name === grant.user_sub;
  const name = unknown ? "Unknown person" : grant.display_name;
  const possessive = isMe ? "your" : `${name}’s`;
  const removed = mode === "removed";

  // Focus follows the row: into the question when it opens, back to Remove
  // when it's cancelled, onto Undo once removed, so a keyboard user is never
  // left on a button that has gone.
  const cancelRef = useRef<HTMLButtonElement>(null);
  const removeRef = useRef<HTMLButtonElement>(null);
  const undoRef = useRef<HTMLButtonElement>(null);
  const previous = useRef(mode);
  useEffect(() => {
    if (previous.current === mode) return;
    if (mode === "confirm") cancelRef.current?.focus();
    else if (mode === "removed") undoRef.current?.focus();
    else if (previous.current === "confirm") removeRef.current?.focus();
    previous.current = mode;
  }, [mode]);

  return (
    <li className={cn("border-t border-divider first:border-t-0", mode === "confirm" && "bg-canvas")} data-testid={`grant-${grant.user_sub}`}>
      <div className="flex min-h-14 items-center gap-4 py-2 pr-3 pl-5">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex min-w-0 items-baseline gap-2">
            <span title={name} className={cn("truncate text-sm leading-label font-medium", removed ? "text-subtle-foreground" : unknown ? "text-muted-foreground" : "text-foreground")}>
              {name}
            </span>
            {isMe && <span className="shrink-0 text-xs text-muted-foreground">you</span>}
          </span>
          {unknown ? (
            <span title={grant.user_sub} className="truncate text-xs text-muted-foreground">
              Not in openplan’s user list · <span className="font-mono">{grant.user_sub}</span>
            </span>
          ) : (
            grant.email && (
              <span title={grant.email} className={cn("truncate text-xs", removed ? "text-subtle-foreground" : "text-muted-foreground")}>
                {grant.email}
              </span>
            )
          )}
          {lockReason && !removed && <span className="text-xs text-muted-foreground">{lockReason}</span>}
        </div>

        {mode === "idle" && (
          <>
            <div className="w-55 shrink-0">
              <RoleSelect
                roles={roles}
                value={grant.role}
                onChange={onRoleChange}
                label={isMe ? "Your role" : `${name}’s role`}
                disabled={lockReason !== undefined || busy}
                pending={rolePending}
                testId={`role-${grant.user_sub}`}
              />
            </div>
            <div className="flex w-18 shrink-0 justify-end">
              <button
                ref={removeRef}
                type="button"
                className={cn(buttonClass("icon"), "pointer-coarse:size-11")}
                aria-label={`Remove ${possessive} access`}
                disabled={lockReason !== undefined || rolePending || busy}
                onClick={onRemove}
              >
                <Trash2 aria-hidden="true" strokeWidth={2.25} className="size-3.5" />
              </button>
            </div>
          </>
        )}

        {mode === "confirm" && (
          <div className="flex shrink-0 items-center gap-3">
            <span className="text-meta text-foreground">{question}</span>
            <div className="flex gap-2">
              {/* Not disabled while the request runs, so focus stays put; a
                  second press is ignored instead. */}
              <button ref={cancelRef} type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => !busy && onCancel()} aria-disabled={busy || undefined}>
                Cancel
              </button>
              <button
                type="button"
                className={cn(buttonClass("outline"), "text-destructive hover:text-destructive pointer-coarse:h-11")}
                onClick={() => !busy && onConfirm()}
                aria-disabled={busy || undefined}
              >
                {busy && <LoaderCircle data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
                {confirmLabel}
              </button>
            </div>
          </div>
        )}

        {removed && (
          <>
            <span className="inline-flex w-55 shrink-0 items-baseline gap-1.75 text-meta text-subtle-foreground">
              <Minus aria-hidden="true" strokeWidth={2.25} className="size-3.5 shrink-0 self-center" />
              access removed
            </span>
            <div className="flex w-18 shrink-0 justify-end">
              {canUndo && (
                <button
                  ref={undoRef}
                  type="button"
                  className={buttonClass("quiet")}
                  aria-label={`Undo removing ${possessive} access`}
                  onClick={() => !busy && onUndo()}
                  aria-disabled={busy || undefined}
                >
                  {busy ? <LoaderCircle data-icon="inline-start" aria-hidden="true" className="animate-spin" /> : <Undo2 data-icon="inline-start" aria-hidden="true" />}
                  Undo
                </button>
              )}
            </div>
          </>
        )}
      </div>

      {mode === "confirm" && warning && (
        <p className="flex justify-end gap-2 pr-3 pb-3 pl-5 text-meta text-foreground">
          <TriangleAlert aria-hidden="true" strokeWidth={2.25} className="mt-0.5 size-3.5 shrink-0 text-warning" />
          <span>{warning}</span>
        </p>
      )}
      {error && (
        <div className="pr-3 pb-3 pl-5">
          <ErrorLine>{error}</ErrorLine>
        </div>
      )}
    </li>
  );
}

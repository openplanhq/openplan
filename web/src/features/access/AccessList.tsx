import { useEffect, useId, useMemo, useRef, useState } from "react";
import { LoaderCircle, Plus, RefreshCw } from "lucide-react";
import type { GrantView, UserProfile } from "../../api/types";
import { apiErrorMessage } from "../../api/problems";
import ErrorLine from "../../shared/ErrorLine";
import RequestIdNote from "../../shared/RequestIdNote";
import SearchField from "../../shared/SearchField";
import { buttonClass } from "../../shared/buttonClass";
import { cn } from "@/lib/utils";
import GrantRow from "./GrantRow";
import type { GrantRowMode } from "./GrantRow";
import PersonPicker from "./PersonPicker";
import RoleSelect from "./RoleSelect";
import { roleRank } from "./roles";
import type { RoleDefinition } from "./roles";

// Past this many people, a filter earns its place above the list.
const FILTER_FROM = 10;

/** The words that differ between a stack's access and the platform's. */
export interface AccessCopy {
  /** What a grant gives: "access to prod", "platform access". */
  access: string;
  /** Where a role applies: "on prod", "on the platform". */
  where: string;
  /** Who can give access back: "another owner or a platform admin". */
  restorers: string;
  /** The list's accessible name: "People with access to prod". */
  listLabel: string;
  loadError: string;
  emptyTitle: string;
}

/** The role a removed or restored grant had, kept until the server agrees. */
type Kept = { grant: GrantView; state: "removed" | "restored" };
type Confirming = { sub: string; kind: "remove" } | { sub: string; kind: "role"; role: string };

/**
 * Who has a role on one object, and the ways to change it: openplan UI's
 * access card. A person picker, a role and Grant access on top, then a
 * GrantRow per person, highest role first. Nothing in it is stack-only: the
 * platform's Access screen (#424) passes its own roles, words and calls.
 *
 * The confirmation rule: what the server will refuse (removing or demoting
 * the only owner, with lockLastTop) is never offered; what takes away your
 * own access confirms with what you lose; removing anyone confirms in the
 * row; and a removed row stays with Undo until the page is left.
 */
export default function AccessList({
  roles,
  copy,
  grants,
  status,
  error,
  onRetry,
  meSub,
  lockLastTop,
  lastTopWarning,
  useSearch,
  assign,
  revoke,
  onOwnAccessChanged
}: {
  /** The ladder, highest first. */
  roles: readonly RoleDefinition[];
  copy: AccessCopy;
  grants: GrantView[] | undefined;
  status: "pending" | "error" | "success";
  error: unknown;
  onRetry: () => void;
  meSub?: string;
  /** The server refuses to remove or demote the last holder of the top role. */
  lockLastTop: boolean;
  /** Said when removing the last holder of the top role is allowed but worth a warning. */
  lastTopWarning?: (name: string) => string;
  useSearch: (query: string) => { data?: UserProfile[] };
  assign: (sub: string, role: string) => Promise<unknown>;
  revoke: (sub: string) => Promise<unknown>;
  /** Your own grant changed. Resolves false when you've left the page, so nothing more is drawn. */
  onOwnAccessChanged: (kind: "removed" | "demoted") => Promise<boolean>;
}) {
  const top = roles[0].value;
  const lowest = roles[roles.length - 1].value;
  const roleFieldId = useId();
  const searchId = useId();

  const [picked, setPicked] = useState<UserProfile | null>(null);
  const [newRole, setNewRole] = useState(lowest);
  const [granting, setGranting] = useState(false);
  const [addError, setAddError] = useState("");
  const [confirming, setConfirming] = useState<Confirming | null>(null);
  const [kept, setKept] = useState<Record<string, Kept>>({});
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState("");
  const [announce, setAnnounce] = useState("");
  // You removed your own grant and could still manage access (a platform
  // admin), so giving it back is still yours to do.
  const [keptOwnAccess, setKeptOwnAccess] = useState(false);
  const mounted = useRef(true);
  // Set on every mount too: StrictMode mounts twice, running the cleanup between.
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const places = useRef(new Map<string, { rank: number; name: string }>());
  const server = useMemo(() => new Map((grants ?? []).map((g) => [g.user_sub, g])), [grants]);

  // A restored grant, or a role change, is drawn from what was sent until the
  // list comes back with it, so a row never flickers back to its old state.
  useEffect(() => {
    setKept((current) => {
      const next = { ...current };
      let changed = false;
      for (const [sub, entry] of Object.entries(current)) {
        if (entry.state === "restored" && server.has(sub)) {
          delete next[sub];
          changed = true;
        }
      }
      return changed ? next : current;
    });
    setDrafts((current) => {
      const next = { ...current };
      let changed = false;
      for (const [sub, role] of Object.entries(current)) {
        if (server.get(sub)?.role === role) {
          delete next[sub];
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [server]);

  const rows = useMemo(() => {
    const subs = new Set([...server.keys(), ...Object.keys(kept)]);
    const list = [...subs].map((sub) => {
      const entry = kept[sub];
      if (entry?.state === "removed") return { grant: entry.grant, removed: true };
      const grant = server.get(sub) ?? entry!.grant;
      return { grant: drafts[sub] ? { ...grant, role: drafts[sub] } : grant, removed: false };
    });
    // By role, highest first, then by name; someone openplan has no record of
    // goes last in their role, since their "name" is only a subject. Each
    // person keeps the place they had when first seen on this visit, so a row
    // whose role just changed doesn't jump away from the pointer and the eye.
    const sortName = (g: GrantView) => (g.display_name === g.user_sub ? "\uffff" : g.display_name);
    const placeOf = (g: GrantView) => {
      let place = places.current.get(g.user_sub);
      if (!place) {
        place = { rank: roleRank(roles, g.role), name: sortName(g) };
        places.current.set(g.user_sub, place);
      }
      return place;
    };
    return list.sort((a, b) => {
      const pa = placeOf(a.grant);
      const pb = placeOf(b.grant);
      return pa.rank - pb.rank || pa.name.localeCompare(pb.name);
    });
  }, [server, kept, drafts, roles]);

  const topHolders = rows.filter((row) => !row.removed && row.grant.role === top).length;
  const ready = status === "success";
  const needle = filter.trim().toLowerCase();
  const shown = needle
    ? rows.filter(({ grant }) => grant.display_name.toLowerCase().includes(needle) || grant.email.toLowerCase().includes(needle))
    : rows;

  const fail = (sub: string, err: unknown, fallback: string) => {
    if (mounted.current) setRowErrors((current) => ({ ...current, [sub]: apiErrorMessage(err, fallback) }));
  };
  const clearError = (sub: string) =>
    setRowErrors((current) => {
      if (!(sub in current)) return current;
      const next = { ...current };
      delete next[sub];
      return next;
    });

  const changeRole = async (grant: GrantView, role: string) => {
    clearError(grant.user_sub);
    setConfirming(null);
    setDrafts((current) => ({ ...current, [grant.user_sub]: role }));
    try {
      await assign(grant.user_sub, role);
      setAnnounce(`${nameOf(grant)} is now ${role}.`);
      if (grant.user_sub === meSub && grant.role === top) await onOwnAccessChanged("demoted");
    } catch (err) {
      setDrafts((current) => {
        const next = { ...current };
        delete next[grant.user_sub];
        return next;
      });
      fail(grant.user_sub, err, `${nameOf(grant)}’s role could not be changed.`);
    }
  };

  const remove = async (grant: GrantView) => {
    clearError(grant.user_sub);
    setBusy(grant.user_sub);
    try {
      await revoke(grant.user_sub);
      if (grant.user_sub === meSub) {
        if (!(await onOwnAccessChanged("removed"))) return;
        setKeptOwnAccess(true);
      }
      if (!mounted.current) return;
      setConfirming(null);
      setKept((current) => ({ ...current, [grant.user_sub]: { grant, state: "removed" } }));
      setAnnounce(`Removed ${nameOf(grant)}’s ${copy.access}. Undo is beside the row.`);
    } catch (err) {
      setConfirming(null);
      fail(grant.user_sub, err, `${nameOf(grant)}’s access could not be removed.`);
    } finally {
      if (mounted.current) setBusy(null);
    }
  };

  const undo = async (grant: GrantView) => {
    clearError(grant.user_sub);
    setBusy(grant.user_sub);
    try {
      await assign(grant.user_sub, grant.role);
      setKept((current) => ({ ...current, [grant.user_sub]: { grant, state: "restored" } }));
      setAnnounce(`${nameOf(grant)} is ${grant.role} again.`);
    } catch (err) {
      fail(grant.user_sub, err, `${nameOf(grant)}’s access could not be given back.`);
    } finally {
      if (mounted.current) setBusy(null);
    }
  };

  const grantPicked = async () => {
    if (!picked) return;
    const person = picked;
    setAddError("");
    setGranting(true);
    try {
      await assign(person.sub, newRole);
      setKept((current) => {
        // Someone removed earlier on this visit and granted again is just granted.
        if (!(person.sub in current)) return current;
        return { ...current, [person.sub]: { grant: { user_sub: person.sub, role: newRole, display_name: person.display_name, email: person.email }, state: "restored" } };
      });
      setPicked(null);
      // Back to the search, ready for the next person.
      requestAnimationFrame(() => document.getElementById(searchId)?.focus());
      setAnnounce(`Granted ${person.display_name} the ${newRole} role ${copy.where}.`);
    } catch (err) {
      setAddError(apiErrorMessage(err, `${person.display_name} could not be granted a role.`));
    } finally {
      setGranting(false);
    }
  };

  const heldRole = (sub: string) => {
    const entry = kept[sub];
    if (entry?.state === "removed") return undefined;
    return server.get(sub)?.role ?? entry?.grant.role;
  };

  return (
    <section aria-label={copy.listLabel} className="rounded-panel border bg-card">
      {/* Add a person: who, their role, and Grant access. */}
      <div className="flex flex-col gap-3 border-b border-divider px-5 py-4">
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <PersonPicker
              useSearch={useSearch}
              roles={roles}
              heldRole={heldRole}
              meSub={meSub}
              picked={picked}
              onPick={(user) => {
                setPicked(user);
                setAddError("");
                // Next: their role.
                if (user) requestAnimationFrame(() => document.getElementById(roleFieldId)?.focus());
              }}
              disabled={!ready || granting}
              inputId={searchId}
            />
          </div>
          <div className="w-50 shrink-0">
            <RoleSelect
              id={roleFieldId}
              roles={roles}
              value={newRole}
              onChange={setNewRole}
              label="Role for the new person"
              variant="field"
              disabled={!ready || granting}
              testId="new-role"
            />
          </div>
          <button
            type="button"
            className={cn(buttonClass("primary", "lg"), "shrink-0 pointer-coarse:h-11")}
            disabled={!ready || !picked || granting}
            onClick={grantPicked}
          >
            {granting ? <LoaderCircle data-icon="inline-start" aria-hidden="true" className="animate-spin" /> : <Plus data-icon="inline-start" aria-hidden="true" />}
            Grant access
          </button>
        </div>
        {addError && <ErrorLine>{addError}</ErrorLine>}
      </div>

      {ready && rows.length > FILTER_FROM && (
        <div className="border-b border-divider px-5 py-3">
          <SearchField label="Filter people" value={filter} onChange={setFilter} testId="access-filter" />
        </div>
      )}

      {status === "pending" && (
        <p className="flex items-center gap-2 px-5 py-4 text-meta text-muted-foreground" data-testid="access-loading">
          <LoaderCircle aria-hidden="true" strokeWidth={2.25} className="size-3.5 animate-spin" />
          Loading who has access…
        </p>
      )}

      {status === "error" && (
        <div className="flex flex-col items-start gap-3 px-5 pt-4 pb-5" data-testid="access-error">
          <ErrorLine>{copy.loadError}</ErrorLine>
          <RequestIdNote error={error} />
          <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={onRetry}>
            <RefreshCw data-icon="inline-start" aria-hidden="true" />
            Retry
          </button>
        </div>
      )}

      {ready && rows.length === 0 && (
        <div className="p-5">
          <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-10 text-center" data-testid="access-empty">
            <p className="text-sm leading-label font-medium">{copy.emptyTitle}</p>
          </div>
        </div>
      )}

      {ready && rows.length > 0 && (
        <>
          <ul aria-label={copy.listLabel}>
            {shown.map(({ grant, removed }) => {
              const sub = grant.user_sub;
              const isMe = sub === meSub;
              const c = confirming?.sub === sub ? confirming : null;
              const mode: GrantRowMode = removed ? "removed" : c ? "confirm" : "idle";
              const lastTop = grant.role === top && topHolders === 1;
              const locked = !removed && lockLastTop && lastTop;
              let question = "";
              let confirmLabel = "";
              let warning: string | undefined;
              if (c?.kind === "remove") {
                question = isMe ? `Remove your own ${copy.access}?` : `Remove ${nameOf(grant)}’s ${copy.access}?`;
                confirmLabel = isMe ? "Remove my access" : "Remove access";
                if (isMe) warning = `If you are not a platform admin, you will lose ${copy.access} right away, and only ${copy.restorers} can give it back.`;
                else if (lastTop && lastTopWarning) warning = lastTopWarning(nameOf(grant));
              } else if (c?.kind === "role") {
                question = `Make yourself ${c.role} ${copy.where}?`;
                confirmLabel = "Change my role";
                warning = `If you are not a platform admin, you will stop managing ${copy.access} right away, and only ${copy.restorers} can make you ${top} again.`;
              }
              return (
                <GrantRow
                  key={sub}
                  roles={roles}
                  grant={grant}
                  isMe={isMe}
                  mode={mode}
                  // Locked without a line saying why: the owner's call for
                  // this screen (2026-10-19), over the design system's rule.
                  locked={locked}
                  rolePending={drafts[sub] !== undefined}
                  busy={busy === sub}
                  question={question}
                  confirmLabel={confirmLabel}
                  warning={warning}
                  error={rowErrors[sub]}
                  // Your own access, once gone, is yours to give back only if
                  // you can still manage access here.
                  canUndo={!isMe || keptOwnAccess}
                  onRoleChange={(role) => {
                    // Taking away your own top role is the one role change that asks first.
                    if (isMe && grant.role === top && roleRank(roles, role) > 0) setConfirming({ sub, kind: "role", role });
                    else void changeRole(grant, role);
                  }}
                  onRemove={() => {
                    clearError(sub);
                    setConfirming({ sub, kind: "remove" });
                  }}
                  onConfirm={() => {
                    if (c?.kind === "role") void changeRole(grant, c.role);
                    else void remove(grant);
                  }}
                  onCancel={() => setConfirming(null)}
                  onUndo={() => void undo(grant)}
                />
              );
            })}
          </ul>
          {needle && shown.length === 0 && <p className="px-5 py-3 text-meta text-muted-foreground">No one matches this filter.</p>}
        </>
      )}

      {/* Always present, so what it says is announced when it changes. */}
      <p role="status" className="sr-only">
        {announce}
      </p>
    </section>
  );
}

function nameOf(grant: GrantView): string {
  return grant.display_name === grant.user_sub ? "this person" : grant.display_name;
}

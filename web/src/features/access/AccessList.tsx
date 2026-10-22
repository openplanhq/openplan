import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { LoaderCircle, Plus, RefreshCw } from "lucide-react";
import type { GrantView, UserProfile } from "../../api/types";
import { apiErrorMessage } from "../../api/problems";
import ErrorLine from "../../shared/ErrorLine";
import RequestIdNote from "../../shared/RequestIdNote";
import SearchField from "../../shared/SearchField";
import { buttonClass } from "../../shared/buttonClass";
import { cn } from "@/lib/utils";
import GrantRow from "./GrantRow";
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
  /** The list's accessible name: "People with access to prod". */
  listLabel: string;
  loadError: string;
  emptyTitle: string;
}

/**
 * Who has a role on one object, and the ways to change it: openplan UI's
 * access card. A person picker, a role and Grant access on top, then a
 * GrantRow per person, highest role first. Nothing in it is stack-only: the
 * platform's Access screen passes its own roles, words and calls.
 *
 * Every change acts at once (owner, 2026-10-19): no confirm, no Undo, your
 * own access included. What the server will refuse (removing or demoting
 * the only owner, with lockLastTop) is never offered.
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
  useSearch,
  assign,
  revoke,
  onOwnAccessChanged,
  lead
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
  useSearch: (query: string) => { data?: UserProfile[] };
  assign: (sub: string, role: string) => Promise<unknown>;
  revoke: (sub: string) => Promise<unknown>;
  /** Your own grant changed: re-read what you may still do here. */
  onOwnAccessChanged?: () => void;
  /** A row above the grants that isn't one, such as the platform's root.
      With it the list is never empty, so the empty state never shows. */
  lead?: ReactNode;
}) {
  const lowest = roles[roles.length - 1].value;
  const roleFieldId = useId();
  const searchId = useId();
  const listRef = useRef<HTMLUListElement>(null);

  const [picked, setPicked] = useState<UserProfile | null>(null);
  const [newRole, setNewRole] = useState(lowest);
  const [granting, setGranting] = useState(false);
  const [addError, setAddError] = useState("");
  // People removed on this visit, hidden until the list comes back without them.
  const [gone, setGone] = useState<ReadonlySet<string>>(new Set());
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [filter, setFilter] = useState("");
  const [announce, setAnnounce] = useState("");
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

  // A role change is drawn from what was sent until the list comes back with
  // it, and a removed row stays hidden until the list drops it, so a row never
  // flickers back to its old state.
  useEffect(() => {
    setGone((current) => {
      const next = new Set([...current].filter((sub) => server.has(sub)));
      return next.size === current.size ? current : next;
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
    const list = [...server.values()]
      .filter((grant) => !gone.has(grant.user_sub))
      .map((grant) => (drafts[grant.user_sub] ? { ...grant, role: drafts[grant.user_sub] } : grant));
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
      const pa = placeOf(a);
      const pb = placeOf(b);
      return pa.rank - pb.rank || pa.name.localeCompare(pb.name);
    });
  }, [server, gone, drafts, roles]);

  const top = roles[0].value;
  const topHolders = rows.filter((grant) => grant.role === top).length;
  const ready = status === "success";
  const needle = filter.trim().toLowerCase();
  const shown = needle
    ? rows.filter((grant) => grant.display_name.toLowerCase().includes(needle) || grant.email.toLowerCase().includes(needle))
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
    setDrafts((current) => ({ ...current, [grant.user_sub]: role }));
    try {
      await assign(grant.user_sub, role);
      setAnnounce(`${nameOf(grant)} is now ${role}.`);
      if (grant.user_sub === meSub) onOwnAccessChanged?.();
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
    const sub = grant.user_sub;
    clearError(sub);
    setBusy(sub);
    // The row is about to go with the button that has focus: hand focus to
    // the next row's Remove, else the one before, else the search.
    const buttons = [...(listRef.current?.querySelectorAll<HTMLButtonElement>("[data-remove]") ?? [])];
    const at = buttons.findIndex((button) => button.closest("li")?.dataset.testid === `grant-${sub}`);
    const heir = buttons[at + 1] ?? buttons[at - 1];
    try {
      await revoke(sub);
      if (!mounted.current) return;
      setGone((current) => new Set(current).add(sub));
      setAnnounce(`Removed ${nameOf(grant)}’s ${copy.access}.`);
      requestAnimationFrame(() => (heir && !heir.disabled ? heir : document.getElementById(searchId))?.focus());
      if (sub === meSub) onOwnAccessChanged?.();
    } catch (err) {
      fail(sub, err, `${nameOf(grant)}’s access could not be removed.`);
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
      // Someone removed earlier on this visit and granted again is just granted.
      setGone((current) => {
        if (!current.has(person.sub)) return current;
        const next = new Set(current);
        next.delete(person.sub);
        return next;
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

  const heldRole = (sub: string) => (gone.has(sub) ? undefined : server.get(sub)?.role);

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

      {ready && rows.length === 0 && !lead && (
        <div className="p-5">
          <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-10 text-center" data-testid="access-empty">
            <p className="text-label">{copy.emptyTitle}</p>
          </div>
        </div>
      )}

      {ready && (rows.length > 0 || lead) && (
        <>
          <ul aria-label={copy.listLabel} ref={listRef}>
            {/* A filter narrows to the people it names; the lead row isn't one. */}
            {!needle && lead}
            {shown.map((grant) => {
              const sub = grant.user_sub;
              return (
                <GrantRow
                  key={sub}
                  roles={roles}
                  grant={grant}
                  isMe={sub === meSub}
                  // Locked without a line saying why: the owner's call for
                  // this screen (2026-10-19), over the design system's rule.
                  locked={lockLastTop && grant.role === top && topHolders === 1}
                  rolePending={drafts[sub] !== undefined}
                  busy={busy === sub}
                  error={rowErrors[sub]}
                  onRoleChange={(role) => void changeRole(grant, role)}
                  onRemove={() => void remove(grant)}
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

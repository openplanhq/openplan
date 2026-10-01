import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { CircleAlert, Loader2, RefreshCw, Search, Shield, Trash2 } from "lucide-react";
import {
  useStackGrantsQuery,
  useSearchUsersQuery,
  useAssignStackRoleMutation,
  useRevokeStackRoleMutation
} from "../../api/queries";
import type { GrantView, UserProfile } from "../../api/types";
import { tenantID } from "../../config";
import RoleBadge from "../../shared/RoleBadge";
import { STACK_ROLES, type StackRole } from "../../shared/roles";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import {
  Combobox,
  ComboboxClear,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList
} from "@/components/ui/combobox";
import { InputGroupAddon } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

interface UndoEntry {
  userSub: string;
  role: string;
  displayName: string;
}

// base.css gives every h2 the legacy 32px display type until PR 9, so each
// heading sets its own family, size, weight and tracking.
const headingClass = "flex items-center gap-2 font-heading text-base leading-snug font-medium tracking-normal";

export default function StackAccessScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const grants = useStackGrantsQuery(tenantID, stackId);

  // What the input shows, and what was typed into it to search. They part
  // when a pick fills the input with the user's name, which is not a query.
  const [inputText, setInputText] = useState("");
  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserProfile | null>(null);
  const [selectedRole, setSelectedRole] = useState<StackRole>("viewer");
  const [undoEntry, setUndoEntry] = useState<UndoEntry | null>(null);
  const [mutationError, setMutationError] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);
  const debouncedSearch = useDebounce(search, 300);
  const searchResults = useSearchUsersQuery(tenantID, debouncedSearch);
  const assignMutation = useAssignStackRoleMutation(tenantID, stackId);
  // Undo has its own, so restoring a role doesn't make Assign read
  // "Assigning...".
  const undoMutation = useAssignStackRoleMutation(tenantID, stackId);
  const revokeMutation = useRevokeStackRoleMutation(tenantID, stackId);
  const undoTimeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  const grantsBySub = new Map((grants.data?.grants ?? []).map((g) => [g.userSub, g]));
  const replacing = selectedUser !== null && grantsBySub.has(selectedUser.sub);
  // The list opens on the user's intent, a search of two or more characters,
  // and says "Searching..." until that search answers, so Base UI never opens
  // or closes it on its own while one loads. useSearchUsersQuery keeps the
  // last answer while a query that narrows it loads, so refining a search
  // doesn't blink back to "Searching...".
  const searching = debouncedSearch.length >= 2;
  const results = searching ? searchResults.data?.users : undefined;
  const listOpen = searchOpen && searching;
  // Who gets the role is who the input shows: typing over a pick holds Assign
  // until the user picks again or leaves, which puts the pick back.
  const pickShown = selectedUser !== null && inputText === selectedUser.displayName;

  const handleAssign = useCallback(async () => {
    if (!selectedUser) return;
    setMutationError("");
    const sentUser = selectedUser;
    const sentSearch = search;
    try {
      await assignMutation.mutateAsync({
        user_sub: sentUser.sub,
        role: selectedRole
      });
      // The search stays usable while the request is out. Clear only what
      // still holds what was sent, so a newer pick or query stays.
      setSelectedUser((current) => (current?.sub === sentUser.sub ? null : current));
      setInputText((current) => (current === sentUser.displayName ? "" : current));
      setSearch((current) => (current === sentSearch ? "" : current));
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : "Failed to assign role");
    }
  }, [selectedUser, selectedRole, search, assignMutation]);

  const handleRevoke = useCallback(
    async (grant: GrantView) => {
      setMutationError("");
      setConfirmRevoke(null);
      try {
        await revokeMutation.mutateAsync(grant.userSub);
        setUndoEntry({
          userSub: grant.userSub,
          role: grant.role,
          displayName: grant.displayName
        });
      } catch (err) {
        setMutationError(err instanceof Error ? err.message : "Failed to revoke role");
      }
    },
    [revokeMutation]
  );

  const handleUndo = useCallback(async () => {
    if (!undoEntry) return;
    setMutationError("");
    try {
      await undoMutation.mutateAsync({
        user_sub: undoEntry.userSub,
        role: undoEntry.role
      });
    } catch {
      setMutationError("Failed to restore role");
    }
    setUndoEntry(null);
  }, [undoEntry, undoMutation]);

  useEffect(() => {
    if (!undoEntry) return;
    undoTimeoutRef.current = setTimeout(() => setUndoEntry(null), 5000);
    return () => clearTimeout(undoTimeoutRef.current);
  }, [undoEntry]);

  useEffect(() => {
    if (revokeMutation.isSuccess || assignMutation.isSuccess || undoMutation.isSuccess) {
      setMutationError("");
    }
  }, [revokeMutation.isSuccess, assignMutation.isSuccess, undoMutation.isSuccess]);

  return (
    // Two columns on a wide screen, split 3:4 as the legacy grid's 0.85fr and
    // 1.15fr were; one column below lg. The grid sets its own text colour,
    // since body keeps the legacy one until PR 9.
    <section className="grid gap-6 text-foreground lg:grid-cols-7">
      <Card className="lg:col-span-3">
        <CardHeader>
          <h2 className={headingClass}>
            <Shield aria-hidden="true" className="size-4" />
            Current Grants
          </h2>
        </CardHeader>
        <CardContent className="grid gap-4">
          {grants.isLoading && (
            <p className="flex items-center gap-2 text-muted-foreground">
              <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading grants...
            </p>
          )}
          {grants.isError && (
            <div className="grid justify-items-start gap-3">
              <Alert variant="destructive">
                <CircleAlert aria-hidden="true" />
                <AlertTitle>Failed to load grants.</AlertTitle>
              </Alert>
              <Button variant="outline" className="pointer-coarse:h-11" onClick={() => grants.refetch()}>
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
                Retry
              </Button>
            </div>
          )}
          {grants.data && grants.data.grants.length === 0 && (
            <p className="text-muted-foreground">
              No users have been assigned access yet. Use the panel on the right to add the first grant.
            </p>
          )}
          {grants.data && grants.data.grants.length > 0 && (
            <ul className="divide-y rounded-lg border">
              {grants.data.grants.map((grant) => (
                <li key={grant.userSub} className="flex flex-wrap items-center gap-x-3 gap-y-2 px-3 py-2">
                  <div className="grid min-w-0 grow basis-32 gap-0.5">
                    <span className="truncate" title={grant.displayName}>
                      {grant.displayName}
                    </span>
                    {grant.email && (
                      <span className="truncate text-xs text-muted-foreground" title={grant.email}>
                        {grant.email}
                      </span>
                    )}
                  </div>
                  <RoleBadge stackRole={grant.role} />
                  <div className="ml-auto flex items-center gap-2">
                    {confirmRevoke === grant.userSub ? (
                      <>
                        <span>Remove access?</span>
                        <Button variant="destructive" className="pointer-coarse:h-11" onClick={() => handleRevoke(grant)}>
                          Confirm
                        </Button>
                        <Button variant="outline" className="pointer-coarse:h-11" onClick={() => setConfirmRevoke(null)}>
                          Cancel
                        </Button>
                      </>
                    ) : (
                      <Button
                        variant="ghost"
                        size="icon"
                        className="pointer-coarse:size-11"
                        onClick={() => setConfirmRevoke(grant.userSub)}
                        aria-label={`Revoke ${grant.displayName}'s ${grant.role} role`}
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Card className="lg:col-span-4">
        <CardHeader>
          <h2 className={headingClass}>
            <Search aria-hidden="true" className="size-4" />
            Assign Role
          </h2>
        </CardHeader>
        <CardContent className="grid gap-4">
          {mutationError && (
            <Alert variant="destructive">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{mutationError}</AlertTitle>
            </Alert>
          )}
          {/* The server searches, by name and email, so the list shows its
              answer as it is (filter={null}). The pick is the Combobox's
              value: the input shows the picked name, and keeps it while the
              user searches again, until another pick, the clear button or
              Escape. */}
          <Combobox<UserProfile>
            items={results ?? []}
            filter={null}
            open={listOpen}
            onOpenChange={(open, { reason }) => {
              setSearchOpen(open);
              // The user left or dismissed the list: put the pick back, or
              // empty the input, and end the search. Other closes, such as
              // Enter with nothing highlighted, keep what was typed.
              if (!open && (reason === "focus-out" || reason === "outside-press" || reason === "escape-key")) {
                setInputText(selectedUser?.displayName ?? "");
                setSearch("");
              }
            }}
            value={selectedUser}
            onValueChange={(user) => setSelectedUser(user)}
            inputValue={inputText}
            onInputValueChange={(value, { reason }) => {
              // Typing searches. A pick writes the user's name, which is not
              // a query. Escape on a closed list, and the clear button, clear
              // the pick. Base UI's other resets follow every close of the
              // list, so they are left out: onOpenChange handles the user's
              // closes, and ignores the rest.
              if (reason === "input-change") {
                setInputText(value);
                setSearch(value);
              } else if (reason === "item-press") {
                setInputText(value);
              } else if (reason === "escape-key" || reason === "clear-press") {
                setInputText(value);
                setSearch("");
              }
            }}
            itemToStringLabel={(user) => user.displayName}
            isItemEqualToValue={(a, b) => a.sub === b.sub}
          >
            <ComboboxInput
              aria-label="Search users"
              placeholder="Search users by name or email..."
              showTrigger={false}
              className="w-full pointer-coarse:h-11 pointer-coarse:*:data-[slot=input-group-control]:h-full"
              // Leaving a closed list fires no close, so put the pick back
              // here. An open list handles its own close, and acting here
              // could shut it under a click on one of its options.
              onBlur={() => {
                if (!listOpen) {
                  // Or a query still in its debounce would open the list
                  // once it lands, on a field the user has left.
                  setSearchOpen(false);
                  setInputText(selectedUser?.displayName ?? "");
                  setSearch("");
                }
              }}
            >
              {/* Base UI's clear button empties the pick and the input
                  (reason "clear-press") and returns focus to the input. It
                  leaves the tab order by default; tabIndex keeps it there,
                  as the selected-user card's X was. The addon goes with the
                  pick, so no empty one pads the input. */}
              {selectedUser && (
                <InputGroupAddon align="inline-end">
                  <ComboboxClear aria-label="Clear selected user" tabIndex={0} className="pointer-coarse:size-11" />
                </InputGroupAddon>
              )}
            </ComboboxInput>
            <ComboboxContent>
              <ComboboxEmpty>{results === undefined ? "Searching..." : "No users found"}</ComboboxEmpty>
              <ComboboxList>
                {(user: UserProfile) => {
                  const grant = grantsBySub.get(user.sub);
                  return (
                    <ComboboxItem key={user.sub} value={user} disabled={grant !== undefined} className="pointer-coarse:min-h-11">
                      <div className="grid min-w-0 flex-1">
                        <span className="truncate">{user.displayName}</span>
                        <span className="truncate font-mono text-xs text-muted-foreground">
                          {user.email || user.sub}
                          {grant && ` — ${grant.role}`}
                        </span>
                      </div>
                    </ComboboxItem>
                  );
                }}
              </ComboboxList>
            </ComboboxContent>
          </Combobox>

          <div className="grid gap-2">
            <Label htmlFor="role-select">Role</Label>
            <Select items={STACK_ROLES} value={selectedRole} onValueChange={(role) => {
                // Base UI types the value as nullable; a role is always chosen.
                if (role !== null) setSelectedRole(role);
              }}>
              <SelectTrigger id="role-select" className="w-full pointer-coarse:data-[size=default]:h-11">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {STACK_ROLES.map((role) => (
                  <SelectItem key={role.value} value={role.value} className="pointer-coarse:min-h-11">
                    {role.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <Button
            className="w-full pointer-coarse:h-11 md:w-auto md:justify-self-start"
            onClick={handleAssign}
            disabled={!pickShown || assignMutation.isPending}
          >
            {assignMutation.isPending ? (
              <>
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
                {replacing ? "Replacing..." : "Assigning..."}
              </>
            ) : replacing ? (
              "Replace Role"
            ) : (
              "Assign Role"
            )}
          </Button>
        </CardContent>
      </Card>

      {/* A toast: fixed above the page, centred, and inside the screen's
          edges on a phone. z-20 puts it over the sticky header (z-5) and
          under Base UI's popups (z-50); AppShell.test.tsx checks the order.
          The status region stays in the page, empty and sizeless, so screen
          readers announce the message when it arrives. */}
      <div role="status" className="fixed inset-x-4 bottom-6 z-20 mx-auto w-fit">
        {undoEntry && (
          <div className="flex items-center gap-4 rounded-lg border bg-popover py-2 pr-2 pl-4 text-sm text-popover-foreground shadow-lg">
            <span>
              Removed {undoEntry.displayName}&apos;s {undoEntry.role} access.
            </span>
            <Button className="pointer-coarse:h-11" disabled={undoMutation.isPending} onClick={handleUndo} aria-label="Undo">
              {undoMutation.isPending ? <Loader2 aria-hidden="true" className="animate-spin" /> : "Undo"}
            </Button>
          </div>
        )}
      </div>
    </section>
  );
}

function useDebounce<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

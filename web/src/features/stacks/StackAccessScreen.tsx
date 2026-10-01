import { useCallback, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { CircleAlert, Loader2, RefreshCw, Search, Shield, Trash2, X } from "lucide-react";
import {
  useStackGrantsQuery,
  useSearchUsersQuery,
  useAssignStackRoleMutation,
  useRevokeStackRoleMutation
} from "../../api/queries";
import type { GrantView, UserProfile } from "../../api/types";
import { tenantID } from "../../config";
import RoleBadge from "../../shared/RoleBadge";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Combobox, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxList } from "@/components/ui/combobox";
import { InputGroupAddon, InputGroupButton } from "@/components/ui/input-group";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const ROLES = [
  { value: "owner", label: "Owner" },
  { value: "operator", label: "Operator" },
  { value: "approver", label: "Approver" },
  { value: "viewer", label: "Viewer" }
] as const;
type Role = (typeof ROLES)[number]["value"];

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

  const [search, setSearch] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const [selectedUser, setSelectedUser] = useState<UserProfile | null>(null);
  const [selectedRole, setSelectedRole] = useState<Role>("viewer");
  const [undoEntry, setUndoEntry] = useState<UndoEntry | null>(null);
  const [mutationError, setMutationError] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState<string | null>(null);
  const debouncedSearch = useDebounce(search, 300);
  const searchResults = useSearchUsersQuery(tenantID, debouncedSearch);
  const assignMutation = useAssignStackRoleMutation(tenantID, stackId);
  const revokeMutation = useRevokeStackRoleMutation(tenantID, stackId);
  const undoTimeoutRef = useRef<ReturnType<typeof setTimeout>>(undefined);

  const grantsBySub = new Map((grants.data?.grants ?? []).map((g) => [g.userSub, g]));
  const replacing = selectedUser !== null && grantsBySub.has(selectedUser.sub);
  // The list opens once a search has answered, as the old dropdown did: before
  // that there is nothing to show, and "No users found" would be untrue.
  const results = debouncedSearch.length >= 2 ? searchResults.data?.users : undefined;

  const handleAssign = useCallback(async () => {
    if (!selectedUser) return;
    setMutationError("");
    try {
      await assignMutation.mutateAsync({
        user_sub: selectedUser.sub,
        role: selectedRole
      });
      setSelectedUser(null);
      setSearch("");
    } catch (err) {
      setMutationError(err instanceof Error ? err.message : "Failed to assign role");
    }
  }, [selectedUser, selectedRole, assignMutation]);

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
      await assignMutation.mutateAsync({
        user_sub: undoEntry.userSub,
        role: undoEntry.role
      });
    } catch {
      setMutationError("Failed to restore role");
    }
    setUndoEntry(null);
  }, [undoEntry, assignMutation]);

  useEffect(() => {
    if (!undoEntry) return;
    undoTimeoutRef.current = setTimeout(() => setUndoEntry(null), 5000);
    return () => clearTimeout(undoTimeoutRef.current);
  }, [undoEntry]);

  useEffect(() => {
    if (revokeMutation.isSuccess || assignMutation.isSuccess) {
      setMutationError("");
    }
  }, [revokeMutation.isSuccess, assignMutation.isSuccess]);

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
                  <div className="grid min-w-0 flex-1 gap-0.5">
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
            open={searchOpen && results !== undefined}
            onOpenChange={setSearchOpen}
            value={selectedUser}
            onValueChange={(user) => setSelectedUser(user)}
            onInputValueChange={(value, { reason }) => {
              // Typing searches. Picking a user writes their name into the
              // input, which is not a query, so only typing sets one.
              if (reason === "input-change") {
                setSearch(value);
              } else if (value === "") {
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
            >
              {selectedUser && (
                <InputGroupAddon align="inline-end">
                  <InputGroupButton
                    size="icon-xs"
                    aria-label="Clear selected user"
                    className="pointer-coarse:size-11"
                    onClick={() => setSelectedUser(null)}
                  >
                    <X aria-hidden="true" />
                  </InputGroupButton>
                </InputGroupAddon>
              )}
            </ComboboxInput>
            <ComboboxContent>
              <ComboboxEmpty>No users found</ComboboxEmpty>
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
            <Select items={ROLES} value={selectedRole} onValueChange={(role) => setSelectedRole(role as Role)}>
              <SelectTrigger id="role-select" className="w-full pointer-coarse:data-[size=default]:h-11">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ROLES.map((role) => (
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
            disabled={!selectedUser || assignMutation.isPending}
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
          under Base UI's popups (z-50); AppShell.test.tsx checks the order. */}
      {undoEntry && (
        <div
          role="status"
          className="fixed inset-x-4 bottom-6 z-20 mx-auto flex w-fit items-center gap-4 rounded-lg border bg-popover py-2 pr-2 pl-4 text-sm text-popover-foreground shadow-lg"
        >
          <span>
            Removed {undoEntry.displayName}&apos;s {undoEntry.role} access.
          </span>
          <Button className="pointer-coarse:h-11" onClick={handleUndo} aria-label="Undo">
            {assignMutation.isPending ? <Loader2 aria-hidden="true" className="animate-spin" /> : "Undo"}
          </Button>
        </div>
      )}
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

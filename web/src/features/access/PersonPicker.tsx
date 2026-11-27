import { useState } from "react";
import { X } from "lucide-react";
import type { UserProfile } from "../../api/types";
import { useDebounce } from "../../shared/useDebounce";
import { buttonClass } from "../../shared/buttonClass";
import { matchCountLabel } from "../../shared/matchCountLabel";
import SearchPicker, { ChoiceRow, Highlighted } from "../../shared/SearchPicker";
import { cn } from "@/lib/utils";
import RoleLabel from "./RoleLabel";
import type { RoleDefinition } from "./roles";

// The most people the list shows at once; past a handful, typing finds
// someone faster than scrolling does.
const MAX_CHOICES = 6;
// The server searches, so the list waits for typing to pause, and for two
// characters, before it asks.
const DEBOUNCE_MS = 300;
const MIN_QUERY = 2;

/**
 * A SearchPicker for people: the server's matches for a name or email, the
 * best few. Someone who already holds a role stays listed, disabled, with it.
 * Once someone is picked the field shows them in place, with a way to clear
 * the pick; the screen decides what a pick means.
 */
export default function PersonPicker({
  useSearch,
  roles,
  heldRole,
  meSub,
  picked,
  onPick,
  disabled = false,
  inputId
}: {
  /** The server search for a query, as a query hook: an empty stack id is the platform's. */
  useSearch: (query: string) => { data?: UserProfile[] };
  roles: readonly RoleDefinition[];
  /** The role someone already holds here, if any. */
  heldRole: (sub: string) => string | undefined;
  meSub?: string;
  picked: UserProfile | null;
  onPick: (user: UserProfile | null) => void;
  disabled?: boolean;
  /** The search input's id, so the screen can return focus to it. */
  inputId?: string;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  // After a pick is cleared the search comes back focused, so a keyboard user
  // carries on where they were.
  const [focusSearch, setFocusSearch] = useState(false);
  const debounced = useDebounce(query.trim(), DEBOUNCE_MS);
  const searching = debounced.length >= MIN_QUERY;
  const results = useSearch(searching ? debounced : "").data;
  const shown = searching && results ? results.slice(0, MAX_CHOICES) : [];

  if (picked) {
    return (
      <div
        className="flex h-9 min-w-0 items-center gap-2 rounded-lg border border-primary/35 bg-primary-soft pr-1 pl-3 pointer-coarse:h-11"
        data-testid="person-picked"
      >
        <span className="truncate text-label text-primary-strong">{picked.display_name}</span>
        {picked.email && <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{picked.email}</span>}
        <button
          type="button"
          aria-label={`Clear ${picked.display_name}`}
          className={cn(buttonClass("icon"), "ml-auto size-7")}
          onClick={() => {
            setQuery("");
            setFocusSearch(true);
            onPick(null);
          }}
        >
          <X aria-hidden="true" strokeWidth={2.25} className="size-3.5" />
        </button>
      </div>
    );
  }

  return (
    // The server ranks and filters; the list shows its answer as it is.
    <SearchPicker<UserProfile>
      items={shown}
      query={query}
      onQueryChange={setQuery}
      onPick={(user) => {
        setQuery("");
        setOpen(false);
        onPick(user);
      }}
      open={open && query.trim().length >= MIN_QUERY}
      onOpenChange={(next) => setOpen(next)}
      itemToStringLabel={(user) => user.display_name}
      isItemEqualToValue={(a, b) => a.sub === b.sub}
      disabled={disabled}
      input={{
        id: inputId,
        "aria-label": "Search people",
        placeholder: "Search people by name or email",
        autoFocus: focusSearch,
        onFocus: () => setOpen(true),
        "data-testid": "person-search"
      }}
      emptyClassName="gap-1 text-muted-foreground"
      empty={
        results === undefined || !searching ? (
          <span>Searching…</span>
        ) : (
          <>
            <span>No one matches “{debounced}”.</span>
            <span>People appear here once they have signed in to openplan. Ask them to sign in, then search again.</span>
          </>
        )
      }
      count={matchCountLabel(results?.length ?? 0, shown.length)}
    >
      {(user) => <PersonChoice key={user.sub} user={user} query={debounced} roles={roles} held={heldRole(user.sub)} isMe={user.sub === meSub} />}
    </SearchPicker>
  );
}

// One person as a ChoiceRow: the name, the letters typed drawn heavier when
// the name starts with them, the email under it, and the role they already
// hold here, which is why they can't be picked.
function PersonChoice({ user, query, roles, held, isMe }: { user: UserProfile; query: string; roles: readonly RoleDefinition[]; held?: string; isMe: boolean }) {
  const lead = user.display_name.toLowerCase().startsWith(query.toLowerCase()) ? query.length : 0;
  return (
    <ChoiceRow
      value={user}
      disabled={held !== undefined}
      name={
        <>
          <Highlighted text={user.display_name} hits={[...Array(lead).keys()]} markClassName="font-semibold" />
          {isMe && <span className="font-normal text-muted-foreground"> (you)</span>}
        </>
      }
      caption={user.email && <span className="truncate text-caption text-muted-foreground">{user.email}</span>}
      aside={
        held && (
          <RoleLabel roles={roles} role={held} className="shrink-0 text-muted-foreground">
            already {held}
          </RoleLabel>
        )
      }
    />
  );
}

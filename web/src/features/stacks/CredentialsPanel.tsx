import { useId, useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import { apiErrorMessage, fieldErrors } from "../../api/problems";
import type { CredentialMetadata, CredentialRequest } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import SecretInput from "../../shared/SecretInput";
import ValueTextarea from "../../shared/ValueTextarea";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/** Which of the Environment's two tabs a panel is: secrets are write-only,
    variables are read back and shown. */
export type CredentialKind = "secret" | "variable";

interface CredentialsPanelProps {
  kind: CredentialKind;
  /** What these entries are for, above the list. */
  note: string;
  /** The empty state's title and its line, shown in place of an empty list. */
  emptyTitle: string;
  emptyDescription: string;
  credentials: CredentialMetadata[];
  loading: boolean;
  busy: boolean;
  onCreate: (credential: CredentialRequest) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

/**
 * One tab of a stack's or a template's environment: a note on what the
 * entries are for, the list (the name, for a variable its value, when it was
 * added, and delete), and the form that adds one. A secret panel lists names
 * only, since a sensitive value never comes back from the server, and masks
 * its Value box; a variable panel shows each value and takes it in plain
 * text. The kind decides the `sensitive` flag every entry is sent with, so
 * which tab someone adds from is the only choice they make. The copy comes
 * from the caller: the template's tab and the stack's Environment page share
 * this panel.
 */
export default function CredentialsPanel({ kind, note, emptyTitle, emptyDescription, credentials, loading, busy, onCreate, onDelete }: CredentialsPanelProps) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  // Whether the Value box shows what is typed. It starts hidden, and goes
  // back to hidden after each add, so every new secret starts hidden.
  const [valueRevealed, setValueRevealed] = useState(false);
  const [error, setError] = useState("");
  // The fields the server refused. The row keeps its one line of error, so
  // a refused field is marked invalid and points at that line.
  const [invalid, setInvalid] = useState<Record<string, string>>({});
  const nameId = useId();
  const valueId = useId();
  const errorId = useId();
  const lineBreakNoteId = useId();

  /** Validates the form, sends the value once, then clears it from local state. */
  async function submit() {
    if (!name.trim() || !value) {
      setError("Name and value are required");
      return;
    }
    setError("");
    setInvalid({});
    const sentName = name;
    const sentValue = value;
    try {
      await onCreate({ name: name.trim(), value, sensitive: kind === "secret" });
      // The fields stay editable while the request is out. Clear only what
      // still holds what was sent, so the next credential, half typed, stays.
      setName((current) => (current === sentName ? "" : current));
      setValue((current) => (current === sentValue ? "" : current));
      setValueRevealed(false);
    } catch (nextError) {
      setInvalid(fieldErrors(nextError, "#/"));
      setError(apiErrorMessage(nextError, "Request failed"));
    }
  }

  /** Deletes one credential, and says why in the same line when that fails. */
  async function remove(id: string) {
    setError("");
    setInvalid({});
    try {
      await onDelete(id);
    } catch (nextError) {
      setError(apiErrorMessage(nextError, "Request failed"));
    }
  }

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted. A half-typed
  // credential secret is the worst instance of the loss this guard exists to
  // prevent: the value never reaches the server until it is added, so losing
  // it means retyping a password from scratch.
  const hasUnsavedCredential = name !== "" || value !== "";
  const secret = kind === "secret";
  // A value is sent exactly as typed, so a one-line token followed by a
  // habitual Enter, or copied from a file, keeps its line break and fails
  // wherever it is used. A key's final line break is part of the key, so
  // only a single line ending in one gets the note.
  const strayLineBreak = /^[^\n]+\n+$/.test(value);
  const valueDescribedBy = [invalid.value ? errorId : "", strayLineBreak ? lineBreakNoteId : ""].filter(Boolean).join(" ") || undefined;

  return (
    <div className="flex min-w-0 flex-col gap-5" data-unsaved={hasUnsavedCredential ? "true" : undefined}>
      <p className="text-meta text-muted-foreground">{note}</p>
      {/* One grid holds the list and the form, and each row is a subgrid of
          it, so a saved name and value sit right above the Name and Value
          boxes; grid-cols-environment's last track fits a row's date and
          delete, and the add button fills it. */}
      <div className="grid grid-cols-environment gap-x-2 gap-y-5">
        {loading ? (
          <p className="col-span-3 flex items-center gap-2 text-meta text-muted-foreground">
            <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading environment…
          </p>
        ) : credentials.length === 0 ? (
          <Empty className="col-span-3 gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-7">
            <EmptyHeader className="gap-2">
              <p className="text-label">{emptyTitle}</p>
              <EmptyDescription className="text-meta">{emptyDescription}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <ul className="col-span-3 grid grid-cols-subgrid divide-y divide-divider overflow-hidden rounded-lg border">
            {credentials.map((credential) => (
              // pl-3 inside the list's 1px border starts the name where a
              // box's text starts: 1px border, 12px inset. Baseline, not
              // centre: a one-line row looks the same either way, and a
              // multi-line value keeps the name, date and delete on its
              // first line.
              <li key={credential.id} className="col-span-3 grid min-h-12 grid-cols-subgrid items-baseline py-2 pr-2 pl-3">
                <span className={cn("min-w-0 truncate font-mono text-meta", secret && "col-span-2")} title={credential.name}>
                  {credential.name}
                </span>
                {!secret && (
                  // The value a variable was saved with, a line per line it
                  // holds, each cut to the row; the whole of it is in the
                  // title. It is the content here, so it takes the name's
                  // colour, inset like the Value box's text.
                  <ListedValue value={credential.value ?? ""} />
                )}
                <span className="flex min-w-0 items-center justify-end gap-2">
                  <span className="truncate text-meta text-muted-foreground">Added {formatTimestamp(credential.created_at)}</span>
                  <button
                    type="button"
                    className={cn(buttonClass("icon"), "shrink-0 pointer-coarse:size-11")}
                    disabled={busy}
                    onClick={() => void remove(credential.id)}
                    aria-label={`Delete ${credential.name}`}
                  >
                    <Trash2 aria-hidden="true" className="size-3.5" strokeWidth={2.25} />
                  </button>
                </span>
              </li>
            ))}
          </ul>
        )}
        {/* A form, so Enter in the Name box adds the entry. In the Value
            box Enter is a line break, so a key typed or pasted a line at a
            time is never sent half done; the button adds it. The Show
            value toggle is type=button and never submits it.
            Each field spans the form's two rows, label over box, and the
            button sits in the second row, so the labels share a line and
            the boxes and the button share a top edge: as the Value box
            grows, the Name box and Add stay where they are. */}
        <form
          className="col-span-3 grid grid-cols-subgrid gap-y-1.5"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="row-span-2 grid min-w-0 grid-rows-subgrid">
            <Label htmlFor={nameId} className={fieldLabelClass}>
              Name
            </Label>
            <Input
              id={nameId}
              placeholder={secret ? "AWS_SECRET_ACCESS_KEY" : "AWS_REGION"}
              autoComplete="off"
              value={name}
              onChange={(event) => setName(event.target.value)}
              aria-invalid={invalid.name ? true : undefined}
              aria-describedby={invalid.name ? errorId : undefined}
              className={inputClass}
            />
          </div>
          <div className="row-span-2 grid min-w-0 grid-rows-subgrid">
            <Label htmlFor={valueId} className={fieldLabelClass}>
              Value
            </Label>
            {/* The note sits under the box in the same cell, so the Name box
                and Add keep their place beside the box's top edge. */}
            <div className="flex min-w-0 flex-col gap-1.5">
              {secret ? (
                <SecretInput
                  id={valueId}
                  revealed={valueRevealed}
                  onRevealedChange={setValueRevealed}
                  // A textarea is never a password field, so no password
                  // manager fills a saved one in or offers to save the
                  // secret; off keeps other autofill out of it too.
                  autoComplete="off"
                  placeholder="Secret value"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                  aria-invalid={invalid.value ? true : undefined}
                  aria-describedby={valueDescribedBy}
                />
              ) : (
                // A variable is read back anyway, so its box is plain text; it
                // still keeps the value out of autofill and spellcheck.
                <ValueTextarea
                  id={valueId}
                  placeholder="us-east-1"
                  autoComplete="off"
                  value={value}
                  onChange={(event) => setValue(event.target.value)}
                  aria-invalid={invalid.value ? true : undefined}
                  aria-describedby={valueDescribedBy}
                />
              )}
              {strayLineBreak && (
                <p id={lineBreakNoteId} className="text-caption text-muted-foreground">
                  Ends with a line break, which is saved as part of the value.
                </p>
              )}
            </div>
          </div>
          {/* The button names what it adds, so the form says which tab it
              belongs to right where someone submits it. Disabled while a
              request is out, which also stops Enter from sending twice. */}
          <button type="submit" className={cn(buttonClass("outline", "lg"), "col-start-3 row-start-2 w-full pointer-coarse:h-11")} disabled={busy}>
            {secret ? "Add secret" : "Add variable"}
          </button>
        </form>
      </div>
      {error && <ErrorLine id={errorId}>{error}</ErrorLine>}
    </div>
  );
}

/** How many of a variable's lines its row shows before saying how many more. */
const LISTED_LINES = 3;

/**
 * A saved variable's value in its row: up to three lines, each cut to the
 * column, then how many more it holds. A CA certificate runs to twenty lines
 * and a chain to seventy, and a row that showed them all would bury the
 * other entries and push the form off the screen. The whole value stays in
 * the title.
 */
function ListedValue({ value }: { value: string }) {
  // A final line break ends the last line; it doesn't start another.
  const lines = value.replace(/\n$/, "").split("\n");
  const more = lines.length - LISTED_LINES;
  return (
    // Its first line is the row's baseline, so a flex column, whose
    // baseline is its first child's.
    <span className="flex min-w-0 flex-col pl-3.25">
      <span className="overflow-hidden font-mono text-meta text-ellipsis whitespace-pre" title={value}>
        {lines.slice(0, LISTED_LINES).join("\n")}
      </span>
      {more > 0 && (
        <span className="text-caption text-muted-foreground">
          {more} more {more === 1 ? "line" : "lines"}
        </span>
      )}
    </span>
  );
}

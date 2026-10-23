import { useId, useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import { apiErrorMessage, fieldErrors } from "../../api/problems";
import type { CredentialMetadata, CredentialRequest } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import SecretInput from "../../shared/SecretInput";
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
              // box's text starts: 1px border, 12px inset.
              <li key={credential.id} className="col-span-3 grid min-h-12 grid-cols-subgrid items-center py-2 pr-2 pl-3">
                <span className={cn("min-w-0 truncate font-mono text-meta", secret && "col-span-2")} title={credential.name}>
                  {credential.name}
                </span>
                {!secret && (
                  // The value a variable was saved with, cut to the row; the
                  // whole of it is in the title. It is the content here, so
                  // it takes the name's colour, inset like the Value box's text.
                  <span className="min-w-0 truncate pl-3.25 font-mono text-meta" title={credential.value}>
                    {credential.value}
                  </span>
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
        {/* A form, so Enter in either box adds the entry. The Show value
            toggle is type=button and never submits it. */}
        <form
          className="col-span-3 grid grid-cols-subgrid items-end"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="flex min-w-0 flex-col gap-1.5">
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
          <div className="flex min-w-0 flex-col gap-1.5">
            <Label htmlFor={valueId} className={fieldLabelClass}>
              Value
            </Label>
            {secret ? (
              <SecretInput
                id={valueId}
                revealed={valueRevealed}
                onRevealedChange={setValueRevealed}
                // Not a login: keeps password managers from filling a saved
                // password in, or offering to save the secret as one, in either
                // state of the toggle.
                autoComplete="new-password"
                placeholder="Secret value"
                value={value}
                onChange={(event) => setValue(event.target.value)}
                aria-invalid={invalid.value ? true : undefined}
                aria-describedby={invalid.value ? errorId : undefined}
              />
            ) : (
              // A variable is read back anyway, so its box is plain text; it
              // still keeps the value out of autofill and spellcheck.
              <Input
                id={valueId}
                placeholder="us-east-1"
                autoComplete="off"
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="off"
                value={value}
                onChange={(event) => setValue(event.target.value)}
                aria-invalid={invalid.value ? true : undefined}
                aria-describedby={invalid.value ? errorId : undefined}
                className={inputClass}
              />
            )}
          </div>
          {/* The button names what it adds, so the form says which tab it
              belongs to right where someone submits it. Disabled while a
              request is out, which also stops Enter from sending twice. */}
          <button type="submit" className={cn(buttonClass("outline", "lg"), "w-full pointer-coarse:h-11")} disabled={busy}>
            {secret ? "Add secret" : "Add variable"}
          </button>
        </form>
      </div>
      {error && <ErrorLine id={errorId}>{error}</ErrorLine>}
    </div>
  );
}

import { useId, useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import type { CredentialMetadata } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

interface CredentialsPanelProps {
  /** What these credentials are for, above the list. */
  note: string;
  /** The empty state's title and its line, shown in place of an empty list. */
  emptyTitle: string;
  emptyDescription: string;
  credentials: CredentialMetadata[];
  loading: boolean;
  busy: boolean;
  onCreate: (name: string, value: string) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

/**
 * Write-only credentials for a stack or for one of its templates: a note on
 * what they are for, the list (the name, when it was added, and delete), and
 * the form that adds one. A value never comes back from the server, so the
 * list has only names. The copy comes from the caller: the template's tab and
 * the stack's Environment page share this panel.
 */
export default function CredentialsPanel({ note, emptyTitle, emptyDescription, credentials, loading, busy, onCreate, onDelete }: CredentialsPanelProps) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const nameId = useId();
  const valueId = useId();

  /** Validates the form, sends the value once, then clears it from local state. */
  async function submit() {
    if (!name.trim() || !value) {
      setError("Name and value are required");
      return;
    }
    setError("");
    const sentName = name;
    const sentValue = value;
    try {
      await onCreate(name.trim(), value);
      // The fields stay editable while the request is out. Clear only what
      // still holds what was sent, so the next credential, half typed, stays.
      setName((current) => (current === sentName ? "" : current));
      setValue((current) => (current === sentValue ? "" : current));
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Request failed");
    }
  }

  /** Deletes one credential, and says why in the same line when that fails. */
  async function remove(id: string) {
    setError("");
    try {
      await onDelete(id);
    } catch (nextError) {
      setError(nextError instanceof Error ? nextError.message : "Request failed");
    }
  }

  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted. A half-typed
  // credential secret is the worst instance of the loss this guard exists to
  // prevent: the value never reaches the server until it is added, so losing
  // it means retyping a password from scratch.
  const hasUnsavedCredential = name !== "" || value !== "";

  return (
    <div className="flex min-w-0 flex-col gap-5" data-unsaved={hasUnsavedCredential ? "true" : undefined}>
      <p className="text-meta text-muted-foreground">{note}</p>
      {loading ? (
        <p className="flex items-center gap-2 text-meta text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading credentials…
        </p>
      ) : credentials.length === 0 ? (
        <Empty className="gap-2 rounded-lg border border-dashed border-dashed-border px-5 py-7">
          <EmptyHeader className="gap-2">
            <p className="text-sm font-medium">{emptyTitle}</p>
            <EmptyDescription className="text-meta">{emptyDescription}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <ul className="divide-y divide-divider overflow-hidden rounded-lg border">
          {credentials.map((credential) => (
            <li key={credential.id} className="flex min-h-12 items-center gap-4 py-2 pr-2 pl-4">
              <span className="min-w-0 flex-1 truncate font-mono text-meta" title={credential.name}>
                {credential.name}
              </span>
              <span className="shrink-0 text-meta text-muted-foreground sm:w-45">Added {formatTimestamp(credential.created_at)}</span>
              <button
                type="button"
                className={cn(buttonClass("icon"), "pointer-coarse:size-11")}
                disabled={busy}
                onClick={() => void remove(credential.id)}
                aria-label={`Delete ${credential.name}`}
              >
                <Trash2 aria-hidden="true" className="size-3.5" strokeWidth={2.25} />
              </button>
            </li>
          ))}
        </ul>
      )}
      {/* Name, Value and Add credential on one row, wrapping on a phone. */}
      <div className="flex flex-wrap items-end gap-2">
        <div className="flex min-w-0 flex-1 basis-50 flex-col gap-1.5">
          <Label htmlFor={nameId} className={fieldLabelClass}>
            Name
          </Label>
          <Input id={nameId} placeholder="AWS_ACCESS_KEY_ID" value={name} onChange={(event) => setName(event.target.value)} className={inputClass} />
        </div>
        <div className="flex min-w-0 flex-1 basis-50 flex-col gap-1.5">
          <Label htmlFor={valueId} className={fieldLabelClass}>
            Value
          </Label>
          <Input
            id={valueId}
            type="password"
            placeholder="Secret value"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            className={inputClass}
          />
        </div>
        <button type="button" className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")} disabled={busy} onClick={() => void submit()}>
          Add credential
        </button>
      </div>
      {error && <ErrorLine>{error}</ErrorLine>}
    </div>
  );
}

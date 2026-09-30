import { useState } from "react";
import { CircleAlert, Loader2, Plus, Trash2 } from "lucide-react";
import type { CredentialMetadata } from "../../api/types";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

interface CredentialsPanelProps {
  title: string;
  // Distinguishes this panel's scope from a near-identically-named one
  // elsewhere (e.g. the stack-scoped Environment tab). Optional so existing
  // call sites that have no such ambiguity to resolve need no change.
  subtitle?: string;
  credentials: CredentialMetadata[];
  loading: boolean;
  busy: boolean;
  onCreate: (name: string, value: string) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

/**
 * Renders write-only credential management for either a Stack or StackTemplate scope.
 *
 * Until PR 9, body keeps the legacy text colour and base.css gives every h2
 * the legacy 32px display type, so the card sets its own colour and the
 * heading sets its own family, size, weight and tracking.
 */
export default function CredentialsPanel({ title, subtitle, credentials, loading, busy, onCreate, onDelete }: CredentialsPanelProps) {
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [error, setError] = useState("");

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

  /** Deletes one credential, and says why in the same Alert when that fails. */
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
  // prevent — the value never reaches the server until Add is clicked, so
  // losing it means retyping a password from scratch.
  const hasUnsavedCredential = name !== "" || value !== "";
  const fieldLabel = title.replace(/ credentials$/, " credential");

  return (
    <Card className="text-foreground" data-unsaved={hasUnsavedCredential ? "true" : undefined}>
      <CardHeader>
        <h2 className="font-heading text-base leading-snug font-medium tracking-normal">{title}</h2>
        {subtitle && <CardDescription>{subtitle}</CardDescription>}
        <CardDescription>
          Values are write-only and injected only when Terraform runs. Use TF_VAR_NAME for Terraform variables; provider credentials keep their provider-specific names.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        {loading ? (
          <p className="flex items-center gap-2 text-muted-foreground">
            <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading credentials…
          </p>
        ) : credentials.length === 0 ? (
          <p className="text-muted-foreground">No credentials configured</p>
        ) : (
          <div className="overflow-hidden rounded-lg border">
            <Table>
              <colgroup>
                <col />
                <col className="w-28" />
                <col className="w-16" />
              </colgroup>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Value</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {credentials.map((credential) => (
                  <TableRow key={credential.id}>
                    <TableCell className="truncate font-mono" title={credential.name}>
                      {credential.name}
                    </TableCell>
                    <TableCell className="text-muted-foreground">configured</TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="pointer-coarse:size-11"
                        disabled={busy}
                        onClick={() => void remove(credential.id)}
                        aria-label={`Delete ${credential.name}`}
                      >
                        <Trash2 aria-hidden="true" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {/* Name, value and Add on one row; stacked on a phone. */}
        <div className="flex flex-col gap-4 md:flex-row">
          <Input
            aria-label={`${fieldLabel} name`}
            placeholder="AWS_ACCESS_KEY_ID"
            value={name}
            onChange={(event) => setName(event.target.value)}
            className="pointer-coarse:h-11 md:flex-1"
          />
          <Input
            aria-label={`${fieldLabel} value`}
            placeholder="Secret value"
            type="password"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            className="pointer-coarse:h-11 md:flex-1"
          />
          <Button variant="outline" className="pointer-coarse:h-11" disabled={busy} onClick={() => void submit()}>
            <Plus data-icon="inline-start" aria-hidden="true" />
            Add
          </Button>
        </div>
        {error && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{error}</AlertTitle>
          </Alert>
        )}
      </CardContent>
    </Card>
  );
}

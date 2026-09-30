import { CircleAlert, Loader2 } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useCreateStackMutation } from "../../api/queries";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export default function CreateStackScreen() {
  const navigate = useNavigate();
  const mutation = useCreateStackMutation(tenantID);
  const [name, setName] = useState("");
  const [errorMessage, setErrorMessage] = useState("");

  const trimmed = name.trim();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (trimmed === "") return;
    setErrorMessage("");
    try {
      const result = await mutation.mutateAsync({ name: trimmed, slug: "", tags: {}, default_credential_ids: [] });
      navigate(`/stacks/${result.id}`);
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : "Failed to create stack");
    }
  };

  if (mutation.isSuccess && mutation.data) {
    return (
      <section className="text-foreground" data-testid="create-stack-success">
        <p className="text-muted-foreground">Redirecting to your new stack…</p>
      </section>
    );
  }

  return (
    // Read by SessionProvider's proactive re-auth timer: it defers navigating
    // away while a `[data-unsaved='true']` element is mounted, so a
    // half-typed stack name is never wiped out by a background sign-in
    // redirect.
    <section className="text-foreground" data-unsaved={trimmed !== "" ? "true" : undefined}>
      <Breadcrumb items={[{ label: "Stacks", to: "/stacks" }, { label: "Create stack" }]} />

      <Card>
        <CardContent className="grid gap-4">
          {errorMessage && (
            <Alert variant="destructive" data-testid="create-stack-error">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{errorMessage}</AlertTitle>
            </Alert>
          )}

          {/* Two columns on a wide screen, as the legacy form grid had, so
              the name field takes half the card. The button takes a row of
              its own and its own width, or the full width on a phone. */}
          <form className="grid gap-5 md:grid-cols-2" onSubmit={handleSubmit}>
            <div className="grid gap-2">
              <Label htmlFor="create-stack-name">Name</Label>
              <Input
                id="create-stack-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="e.g. Production"
                autoFocus
                className="pointer-coarse:h-11"
              />
            </div>
            <Button
              type="submit"
              className="w-full pointer-coarse:h-11 md:col-span-2 md:w-auto md:justify-self-start"
              disabled={trimmed === "" || mutation.isPending}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
                  Creating…
                </>
              ) : (
                "Create stack"
              )}
            </Button>
          </form>
        </CardContent>
      </Card>
    </section>
  );
}

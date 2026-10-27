import { LoaderCircle } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { apiErrorMessage, fieldsShown } from "../../api/problems";
import { useCreateStackMutation } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import PageHeader from "../../shared/PageHeader";
import { useFieldErrors } from "../../shared/useFieldErrors";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

// Create stack, on openplan UI: its header and trail on the canvas, and the
// form in one card at the width of a form column (the owner's pick on issue
// 524).
export default function CreateStackScreen() {
  const tenantID = useTenantID();
  const navigate = useNavigate();
  const mutation = useCreateStackMutation(tenantID);
  const [name, setName] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const nameErrors = useFieldErrors("#/");

  const trimmed = name.trim();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (trimmed === "") return;
    setErrorMessage("");
    nameErrors.clear();
    try {
      const result = await mutation.mutateAsync({ name: trimmed, slug: "", tags: {}, default_credential_ids: [] });
      navigate(`/stacks/${result.id}`);
    } catch (err) {
      // The name's own problems show under it, including a clash on the slug
      // the server made from it; the line under the button says anything else.
      nameErrors.show(err);
      setErrorMessage(apiErrorMessage(err, "Failed to create stack", fieldsShown("#/", ["name"])));
    }
  };

  if (mutation.isSuccess && mutation.data) {
    return (
      <section data-testid="create-stack-success">
        <p className="text-meta text-muted-foreground">Redirecting to your new stack…</p>
      </section>
    );
  }

  return (
    // Read by SessionProvider's proactive re-auth timer: it defers navigating
    // away while a `[data-unsaved='true']` element is mounted, so a
    // half-typed stack name is never wiped out by a background sign-in
    // redirect.
    <section className="mx-auto max-w-140" data-unsaved={trimmed !== "" ? "true" : undefined}>
      <PageHeader title="Create stack" trail={[{ label: "Stacks", to: "/stacks" }]} />

      <form
        className="flex flex-col items-start gap-5 rounded-panel border bg-card px-7 pt-6 pb-7"
        onSubmit={handleSubmit}
      >
        <div className="flex flex-col gap-1.5 self-stretch">
          <Label htmlFor="create-stack-name" className={fieldLabelClass}>
            Name
          </Label>
          <Input
            id="create-stack-name"
            value={name}
            onChange={(event) => {
              setName(event.target.value);
              nameErrors.clear("name");
            }}
            aria-invalid={nameErrors.errors.name ? true : undefined}
            aria-describedby={nameErrors.errors.name ? "create-stack-name-error" : undefined}
            placeholder="prod"
            autoComplete="off"
            autoFocus
            className={inputClass}
          />
          {nameErrors.errors.name && (
            <ErrorLine id="create-stack-name-error" live={false}>
              {nameErrors.errors.name}
            </ErrorLine>
          )}
        </div>
        <button
          type="submit"
          className={cn(buttonClass("primary"), "pointer-coarse:h-11")}
          disabled={trimmed === "" || mutation.isPending}
        >
          {mutation.isPending ? (
            <>
              <LoaderCircle data-icon="inline-start" aria-hidden="true" className="animate-spin" />
              Creating…
            </>
          ) : (
            "Create stack"
          )}
        </button>
        {errorMessage && <ErrorLine testId="create-stack-error">{errorMessage}</ErrorLine>}
      </form>
    </section>
  );
}

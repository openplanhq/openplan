import { useEffect, useRef, useState } from "react";
import { GitBranch, Loader2 } from "lucide-react";
import { ApiRequestError } from "../../api/client";
import { apiErrorMessage } from "../../api/problems";
import { useChangeTemplateRefMutation } from "../../api/queries";
import { problemTypes } from "../../api/types";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, inputClass } from "../../shared/fieldClass";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

// Changing the ref a template follows, from Change ref on the name's line:
// a step under the source line, drawn as InlineConfirm is (one border, canvas
// fill), holding the ref to follow from now on. It opens on the current ref,
// selected, and changes nothing until the ref differs. Under the field, the
// one thing the screen can't show: stacks stay on their revision (owner,
// 2026-11-04). A refusal takes that line's place.
//
// Enter changes, Escape cancels. While the request runs Change ref stays
// focusable, marked aria-disabled, its icon turning.
export function TemplateRefStep({
  currentRef,
  busy,
  refusal,
  onChange,
  onCancel
}: {
  currentRef: string;
  busy: boolean;
  /** Why the server refused the last change, in words for a person. */
  refusal: string | null;
  onChange: (sourceRef: string) => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState(currentRef);
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();
  }, []);
  const next = draft.trim();
  const ready = next !== "" && next !== currentRef && !busy;

  return (
    <form
      aria-label="Change ref"
      className="flex min-w-0 items-end justify-between gap-6 rounded-lg border bg-canvas px-5 py-4"
      data-testid="template-ref-step"
      onSubmit={(event) => {
        event.preventDefault();
        if (ready) {
          onChange(next);
        }
      }}
      onKeyDown={(event) => {
        if (event.key === "Escape" && !busy) {
          event.stopPropagation();
          onCancel();
        }
      }}
    >
      <div className="flex min-w-0 grow flex-col gap-1.5">
        <Label htmlFor="template-ref" className={fieldLabelClass}>
          Ref
        </Label>
        <Input
          ref={inputRef}
          id="template-ref"
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          aria-invalid={refusal !== null ? true : undefined}
          aria-describedby="template-ref-said"
          spellCheck={false}
          autoComplete="off"
          className={cn(inputClass, "max-w-90")}
        />
        {refusal !== null ? (
          <ErrorLine id="template-ref-said">{refusal}</ErrorLine>
        ) : (
          <p id="template-ref-said" className="text-caption text-muted-foreground">
            Stacks stay on their revision until someone changes it.
          </p>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => !busy && onCancel()}>
          Cancel
        </button>
        <button
          type="submit"
          className={cn(buttonClass("primary"), "pointer-coarse:h-11", !ready && !busy && "opacity-50")}
          aria-disabled={ready ? undefined : "true"}
        >
          {busy ? (
            <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
          ) : (
            <GitBranch data-icon="inline-start" aria-hidden="true" />
          )}
          Change ref
        </button>
      </div>
    </form>
  );
}

// The state of a template's ref change. The panel stays mounted from one
// template to the next, so the step closes when the template changes, and an
// answer acts only on a step still open for its template: one that lands
// after the person moved on, or opened Delete, leaves focus where they put it.
// After a change focus goes back to Change ref.
export function useTemplateRefChange(id: string) {
  const tenantID = useTenantID();
  const changeTemplateRefMutation = useChangeTemplateRefMutation(tenantID);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [openFor, setOpenFor] = useState<string | null>(null);
  // openFor as of now, for an answer that arrives after renders moved on.
  const openNow = useRef<string | null>(null);
  const [refusal, setRefusal] = useState<{ id: string; message: string } | null>(null);
  const busy = changeTemplateRefMutation.isPending;

  function dismiss() {
    openNow.current = null;
    setOpenFor(null);
    setRefusal(null);
  }
  useEffect(dismiss, [id]);

  return {
    triggerRef,
    open: openFor === id,
    busy,
    refusal: refusal?.id === id ? refusal.message : null,
    ask() {
      openNow.current = id;
      setRefusal(null);
      setOpenFor(id);
    },
    // Another step opening in its place: close, and leave focus where it is.
    dismiss,
    cancel() {
      dismiss();
      triggerRef.current?.focus();
    },
    async change(sourceRef: string) {
      if (busy) {
        return;
      }
      setRefusal(null);
      try {
        await changeTemplateRefMutation.mutateAsync({ templateID: id, sourceRef });
        if (openNow.current !== id) {
          return;
        }
        dismiss();
        triggerRef.current?.focus();
      } catch (error) {
        if (openNow.current !== id) {
          return;
        }
        const taken = error instanceof ApiRequestError && error.type === problemTypes.duplicateTemplateSource;
        setRefusal({ id, message: taken ? `Another template already follows ${sourceRef}.` : apiErrorMessage(error, "Request failed") });
      }
    }
  };
}

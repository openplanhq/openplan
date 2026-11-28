import { useEffect, useRef, useState } from "react";
import { GitBranch } from "lucide-react";
import { apiErrorMessage, isProblem } from "../../api/problems";
import { useChangeTemplateRefMutation } from "../../api/queries";
import { problemTypes } from "../../api/types";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import Field from "../../shared/Field";
import { inputClass } from "../../shared/fieldClass";
import BusyIcon from "../../shared/BusyIcon";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useInlineStep } from "./useInlineStep";

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
      <Field
        id="template-ref"
        label="Ref"
        error={refusal ?? undefined}
        liveError
        hint={refusal === null && "Stacks stay on their revision until someone changes it."}
        className="grow"
      >
        {(control) => (
          <Input
            {...control}
            ref={inputRef}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            spellCheck={false}
            autoComplete="off"
            className={cn(inputClass, "max-w-90")}
          />
        )}
      </Field>
      <div className="flex shrink-0 items-center gap-2">
        <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => !busy && onCancel()}>
          Cancel
        </button>
        <button
          type="submit"
          className={cn(buttonClass("primary"), "pointer-coarse:h-11", !ready && !busy && "opacity-50")}
          aria-disabled={ready ? undefined : "true"}
        >
          <BusyIcon busy={busy} icon={GitBranch} />
          Change ref
        </button>
      </div>
    </form>
  );
}

// A template's ref change, in the step useInlineStep keeps. After a change
// focus goes back to Change ref, as after Cancel.
export function useTemplateRefChange(id: string) {
  const tenantID = useTenantID();
  const changeTemplateRefMutation = useChangeTemplateRefMutation(tenantID);
  const step = useInlineStep<string>(id);
  const busy = changeTemplateRefMutation.isPending;

  return {
    ...step,
    busy,
    async change(sourceRef: string) {
      if (busy) {
        return;
      }
      step.refuse(null);
      try {
        await changeTemplateRefMutation.mutateAsync({ templateID: id, sourceRef });
        if (step.stillOpen()) {
          step.cancel();
        }
      } catch (error) {
        if (step.stillOpen()) {
          const taken = isProblem(error, problemTypes.duplicateTemplateSource);
          step.refuse(taken ? `Another template already follows ${sourceRef}.` : apiErrorMessage(error, "Request failed"));
        }
      }
    }
  };
}

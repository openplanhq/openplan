import type { ReactNode } from "react";
import ErrorLine from "./ErrorLine";
import { fieldLabelClass } from "./fieldClass";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

type FieldProps = {
  id: string;
  label: ReactNode;
  /** A Terraform variable's name: mono, breaking anywhere. */
  mono?: boolean;
  /** Sits under the label, closer to it than to the control; its id goes in describedBy. */
  detail?: ReactNode;
  describedBy?: string;
  error?: string;
  liveError?: boolean;
  hint?: ReactNode;
  className?: string;
  children: (control: { id: string; "aria-invalid"?: true; "aria-describedby"?: string }) => ReactNode;
};

// openplan UI's Field: a label over its control, then what was refused about
// it, then what it is for. The control gets the ids that tie it to them.
export default function Field({ id, label, mono = false, detail, describedBy, error, liveError = false, hint, className, children }: FieldProps) {
  const labelElement = (
    <Label htmlFor={id} className={cn(fieldLabelClass, mono && "font-mono wrap-anywhere")}>
      {label}
    </Label>
  );
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      {detail ? (
        <div className="flex flex-col gap-0.5">
          {labelElement}
          {detail}
        </div>
      ) : (
        labelElement
      )}
      {children({
        id,
        "aria-invalid": error ? true : undefined,
        "aria-describedby": [error && `${id}-error`, describedBy, hint && `${id}-hint`].filter(Boolean).join(" ") || undefined
      })}
      {error ? <ErrorLine id={`${id}-error`} live={liveError}>{error}</ErrorLine> : null}
      {hint ? (
        <p id={`${id}-hint`} className="text-caption text-muted-foreground">
          {hint}
        </p>
      ) : null}
    </div>
  );
}

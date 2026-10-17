import { useId } from "react";
import type { TemplateVariable } from "../../api/types";
import ErrorLine from "../../shared/ErrorLine";
import { inputClass } from "../../shared/fieldClass";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface VariableFieldsProps {
  variables: TemplateVariable[];
  variableValues: Record<string, string>;
  onVariableValueChange: (name: string, value: string) => void;
  disabled?: boolean;
  /** What to say when the revision declares no variables. */
  emptyMessage: string;
  /** What the server refused about each variable, by name. */
  errors?: Record<string, string>;
}

/**
 * The variable fields, and nothing else: no mutations, no revision choice, no
 * actions. Shared by Variables, Add template and Change revision, so all
 * three draw variables the same way.
 *
 * One column, at most 560px: each variable's name as a mono label (" *" when
 * it is required), its input, and its description under it. Each input's id
 * comes from useId, so a label always names its own input. A variable the
 * server refused shows why under its input, which is marked invalid.
 */
export default function VariableFields({
  variables,
  variableValues,
  onVariableValueChange,
  disabled = false,
  emptyMessage,
  errors = {}
}: VariableFieldsProps) {
  const idPrefix = useId();
  if (variables.length === 0) {
    return <p className="text-meta text-muted-foreground">{emptyMessage}</p>;
  }
  return (
    <div className="flex max-w-140 flex-col gap-4">
      {variables.map((variable, index) => {
        const id = `${idPrefix}-${index}`;
        const descriptionId = `${id}-description`;
        const errorId = `${id}-error`;
        const description = variable.description.trim();
        const error = errors[variable.name];
        const describedBy = [error ? errorId : "", description ? descriptionId : ""].filter(Boolean).join(" ");
        return (
          <div key={variable.name} className="flex flex-col gap-1.5">
            {/* Variable names are identifiers with no spaces, so a long one
                breaks anywhere rather than widening the column. */}
            <Label htmlFor={id} className="font-mono text-meta leading-label wrap-anywhere">
              {variable.name}
              {variable.required ? " *" : ""}
            </Label>
            <Input
              id={id}
              value={variableValues[variable.name] ?? ""}
              onChange={(event) => onVariableValueChange(variable.name, event.target.value)}
              placeholder={variable.type_expression || "value"}
              disabled={disabled}
              aria-invalid={error ? true : undefined}
              aria-describedby={describedBy || undefined}
              className={inputClass}
            />
            {error && (
              <ErrorLine id={errorId} live={false}>
                {error}
              </ErrorLine>
            )}
            {description && (
              <p id={descriptionId} className="text-xs text-muted-foreground">
                {description}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}

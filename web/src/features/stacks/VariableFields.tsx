import { useId } from "react";
import type { TemplateVariable } from "../../api/types";
import ErrorLine from "../../shared/ErrorLine";
import { inputClass, textareaClass } from "../../shared/fieldClass";
import { isStructuredVariable } from "./stackWorkflow";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";

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
 *
 * A string, number or bool keeps a one-line input with its type as the
 * placeholder. Anything else (list, map, set, object, tuple, any, or no type
 * at all) is typed as JSON in a mono textarea, with the type expression shown
 * verbatim between the label and the field: a placeholder vanishes as soon as
 * someone types, and the shape of an object is what they need while typing.
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
        const typeId = `${id}-type`;
        const description = variable.description.trim();
        const error = errors[variable.name];
        const typeExpression = variable.type_expression.trim();
        const structured = isStructuredVariable(variable);
        const describedBy = [error ? errorId : "", structured ? typeId : "", description ? descriptionId : ""].filter(Boolean).join(" ");
        return (
          <div key={variable.name} className="flex flex-col gap-1.5">
            {/* The label and the type read as one heading for the field, so
                they sit closer to each other than to the control. */}
            <div className="flex flex-col gap-0.5">
              {/* Variable names are identifiers with no spaces, so a long one
                  breaks anywhere rather than widening the column. */}
              <Label htmlFor={id} className="font-mono text-meta leading-label wrap-anywhere">
                {variable.name}
                {variable.required ? " *" : ""}
              </Label>
              {structured && (
                // Kept as the template wrote it: a multi-line object() type
                // keeps its line breaks and indentation. No type means any.
                <pre id={typeId} className="m-0 font-mono text-xs leading-relaxed whitespace-pre-wrap text-muted-foreground wrap-anywhere">
                  {typeExpression || "any"}
                </pre>
              )}
            </div>
            {structured ? (
              <Textarea
                id={id}
                value={variableValues[variable.name] ?? ""}
                onChange={(event) => onVariableValueChange(variable.name, event.target.value)}
                disabled={disabled}
                // JSON isn't prose: no spelling marks, no capitalised first key.
                spellCheck={false}
                autoCapitalize="off"
                autoComplete="off"
                aria-invalid={error ? true : undefined}
                aria-describedby={describedBy || undefined}
                className={textareaClass}
              />
            ) : (
              <Input
                id={id}
                value={variableValues[variable.name] ?? ""}
                onChange={(event) => onVariableValueChange(variable.name, event.target.value)}
                placeholder={typeExpression}
                disabled={disabled}
                aria-invalid={error ? true : undefined}
                aria-describedby={describedBy || undefined}
                className={inputClass}
              />
            )}
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

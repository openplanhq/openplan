import { useId } from "react";
import { LockKeyhole } from "lucide-react";
import { Link } from "react-router-dom";
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
  /** Where a sensitive variable's secret is added: the installed template's
      Environment, or the stack's while the template isn't installed yet. */
  environmentPath: string;
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
  errors = {},
  environmentPath
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
        if (variable.sensitive) {
          return <SensitiveVariable key={variable.name} variable={variable} description={description} environmentPath={environmentPath} />;
        }
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
                <pre id={typeId} className="m-0 font-mono text-log whitespace-pre-wrap text-muted-foreground wrap-anywhere">
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
              <p id={descriptionId} className="text-caption text-muted-foreground">
                {description}
              </p>
            )}
          </div>
        );
      })}
    </div>
  );
}

/**
 * A variable marked sensitive, with no box: config is stored in the clear and
 * shown to everyone who can view the stack, so the server refuses a value for
 * it. Its value comes from an Environment secret named TF_VAR_<name>, and the
 * row says which and links to where it is added. The name keeps the label's
 * look and its required mark; "Sensitive" is the icon and the word.
 */
function SensitiveVariable({ variable, description, environmentPath }: { variable: TemplateVariable; description: string; environmentPath: string }) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <p className="m-0 font-mono text-meta leading-label font-medium wrap-anywhere">
          {variable.name}
          {variable.required ? " *" : ""}
        </p>
        <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
          <LockKeyhole aria-hidden="true" className="size-3" strokeWidth={2.25} />
          Sensitive
        </span>
      </div>
      <p className="text-meta text-muted-foreground">
        Set it as the secret <code className="font-mono text-code-foreground">{`TF_VAR_${variable.name}`}</code> in{" "}
        <Link to={environmentPath} className="font-medium text-primary hover:underline">
          Environment
        </Link>
        .
      </p>
      {description && <p className="text-caption text-muted-foreground">{description}</p>}
    </div>
  );
}

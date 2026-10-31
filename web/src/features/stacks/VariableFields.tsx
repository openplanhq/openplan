import { useId } from "react";
import { Lock } from "lucide-react";
import { Link } from "react-router-dom";
import type { TemplateVariable } from "../../api/types";
import ErrorLine from "../../shared/ErrorLine";
import StatusLabel from "../../shared/StatusLabel";
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
  /** Who can set a sensitive variable's secret, and where. */
  secrets: SecretSetting;
}

/** Where a sensitive variable's TF_VAR_ secret is set, and whether this
    person can set it. */
export interface SecretSetting {
  /** The stack's can_manage_access: only an owner can add secrets, and the
      Environment screens are closed to everyone else. */
  canSet: boolean;
  /** The installed template's Environment. Absent while the template isn't
      installed yet, which also means no link: following one mid-form would
      throw away what was picked and typed. */
  environmentPath?: string;
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
  secrets
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
          return <SensitiveVariable key={variable.name} variable={variable} description={description} secrets={secrets} />;
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
 * row says which, who sets it and where. The name keeps the label's look and
 * its required mark; "sensitive" is a StatusLabel, icon and word.
 */
function SensitiveVariable({ variable, description, secrets }: { variable: TemplateVariable; description: string; secrets: SecretSetting }) {
  const { canSet, environmentPath } = secrets;
  // The installed template's own Environment, never the stack's: a stack
  // secret reaches every template the stack installs, and template code is
  // untrusted.
  const where = environmentPath ? (
    canSet ? (
      <Link to={environmentPath} className="font-medium text-primary hover:underline">
        Environment
      </Link>
    ) : (
      "Environment"
    )
  ) : (
    "its Environment"
  );
  const who = canSet ? "set it" : "an owner sets it";
  const lead = environmentPath ? who.charAt(0).toUpperCase() + who.slice(1) : `Once added, ${who}`;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <p className="m-0 font-mono text-meta leading-label font-medium wrap-anywhere">
          {variable.name}
          {variable.required ? " *" : ""}
        </p>
        <StatusLabel icon={Lock} tone="settled">
          sensitive
        </StatusLabel>
      </div>
      {/* A TF_VAR_ name has no break points of its own, so a long one breaks
          anywhere rather than widening the column. */}
      <p className="text-meta text-muted-foreground wrap-anywhere">
        {lead} as the secret <code className="font-mono text-code-foreground">{`TF_VAR_${variable.name}`}</code> in {where}.
      </p>
      {description && <p className="text-caption text-muted-foreground">{description}</p>}
    </div>
  );
}

import { useId } from "react";
import type { TemplateVariable } from "../../api/types";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface VariableFieldsProps {
  variables: TemplateVariable[];
  variableValues: Record<string, string>;
  onVariableValueChange: (name: string, value: string) => void;
  disabled?: boolean;
  emptyMessage?: string;
}

/**
 * The variable input grid, and nothing else — no mutations, no revision
 * selection, no actions. Shared by the template, add, and upgrade screens so
 * all three render variables identically.
 *
 * Two columns from md, one on a phone. Each input's id comes from useId, so a
 * label always names its own input, whatever the variable is called.
 */
export default function VariableFields({
  variables,
  variableValues,
  onVariableValueChange,
  disabled = false,
  emptyMessage = "No variables loaded"
}: VariableFieldsProps) {
  const idPrefix = useId();
  if (variables.length === 0) {
    return <p className="text-muted-foreground">{emptyMessage}</p>;
  }
  return (
    <div className="grid gap-5 md:grid-cols-2">
      {variables.map((variable, index) => {
        const id = `${idPrefix}-${index}`;
        return (
          <div key={variable.name} className="grid content-start gap-2">
            {/* Variable names are identifiers with no spaces, so a long one
                breaks anywhere rather than widening the grid. */}
            <Label htmlFor={id} className="wrap-anywhere">
              {variable.name}
              {variable.required ? " *" : ""}
            </Label>
            <Input
              id={id}
              value={variableValues[variable.name] ?? ""}
              onChange={(event) => onVariableValueChange(variable.name, event.target.value)}
              placeholder={variable.type_expression || "value"}
              disabled={disabled}
              className="pointer-coarse:h-11"
            />
          </div>
        );
      })}
    </div>
  );
}

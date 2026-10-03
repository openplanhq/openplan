import { Loader2 } from "lucide-react";
import type { TemplateVariable } from "../../api/types";
import { buttonClass } from "../../shared/buttonClass";
import VariableFields from "./VariableFields";
import { cn } from "@/lib/utils";

interface StackTemplateConfigPanelProps {
  variables: TemplateVariable[];
  variableValues: Record<string, string>;
  onVariableValueChange: (name: string, value: string) => void;
  canSave: boolean;
  onSave: () => void;
  saveBusy: boolean;
  // When set, it leads the panel, and the inputs and the action are
  // disabled: the capability gate's denied-with-reason state (AUTH-020).
  disabledReason?: string;
}

/**
 * The installed template's variables, for its desired revision, with one
 * action. Adding and changing the revision live on their own views, so this
 * panel never switches modes.
 */
export default function StackTemplateConfigPanel({
  variables,
  variableValues,
  onVariableValueChange,
  canSave,
  onSave,
  saveBusy,
  disabledReason
}: StackTemplateConfigPanelProps) {
  const locked = Boolean(disabledReason);
  return (
    <div className="flex min-w-0 flex-col gap-5" data-testid="stack-template-config">
      {disabledReason && (
        <p className="text-meta text-muted-foreground" data-testid="variables-disabled-reason">
          {disabledReason}
        </p>
      )}
      <VariableFields
        variables={variables}
        variableValues={variableValues}
        onVariableValueChange={onVariableValueChange}
        disabled={locked}
        emptyMessage="This template declares no variables."
      />
      <button
        type="button"
        className={cn(buttonClass("primary"), "self-start pointer-coarse:h-11")}
        disabled={locked || !canSave || saveBusy}
        onClick={onSave}
      >
        {saveBusy && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
        Save variables
      </button>
    </div>
  );
}

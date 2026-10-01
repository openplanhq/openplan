import { Loader2, Save } from "lucide-react";
import type { TemplateVariable } from "../../api/types";
import VariableFields from "./VariableFields";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";

interface StackTemplateConfigPanelProps {
  variables: TemplateVariable[];
  variableValues: Record<string, string>;
  onVariableValueChange: (name: string, value: string) => void;
  canSave: boolean;
  onSave: () => void;
  saveBusy: boolean;
  // When set, the inputs and the action render disabled and the reason is
  // shown — the capability gate's denied-with-reason state (AUTH-020).
  disabledReason?: string;
}

/**
 * Variables for the installed template's desired revision, with a single
 * action. Installing and upgrading live on their own screens, so this panel
 * never has to switch modes.
 *
 * Preflight leaves a heading with the body's type, so the heading sets
 * CardTitle's look: family, size, weight and tracking.
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
    <Card data-testid="stack-template-config">
      <CardHeader>
        <h2 className="font-heading text-base leading-snug font-medium tracking-normal">Variables</h2>
      </CardHeader>
      <CardContent className="grid gap-6">
        <VariableFields
          variables={variables}
          variableValues={variableValues}
          onVariableValueChange={onVariableValueChange}
          disabled={locked}
        />
        {/* Its own width, or the full width on a phone. */}
        <Button
          className="w-full pointer-coarse:h-11 md:w-auto md:justify-self-start"
          disabled={locked || !canSave || saveBusy}
          onClick={onSave}
        >
          {saveBusy ? (
            <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
          ) : (
            <Save data-icon="inline-start" aria-hidden="true" />
          )}
          Save config
        </Button>
        {disabledReason && (
          <p className="text-sm text-muted-foreground" data-testid="variables-disabled-reason">
            {disabledReason}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

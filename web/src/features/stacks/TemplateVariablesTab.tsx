import { useState } from "react";
import { CircleAlert, Loader2, RefreshCw } from "lucide-react";
import { useTemplateRevisionVariablesQuery, useUpdateStackTemplateConfigMutation } from "../../api/queries";
import { tenantID } from "../../config";
import RequireCapability from "../../auth/RequireCapability";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { runInFlightReason, useRunInFlight } from "../runs/useRunInFlight";
import StackTemplateConfigPanel from "./StackTemplateConfigPanel";
import { useStackTemplateOutlet } from "./StackTemplateDetailShell";
import {
  canSaveInstalledTemplateConfig,
  configFromVariableValues,
  isDestroyingStackTemplate,
  variableValuesFromConfig
} from "./stackWorkflow";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

// /stacks/:stackId/templates/:stackTemplateId/variables — the installed
// template's configuration. Choosing another revision is a separate page
// (upgrade), reached from Settings, so this form has exactly one action.
export default function TemplateVariablesTab() {
  const { stackId, stackTemplate } = useStackTemplateOutlet();
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");

  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, stackTemplate.desired_template_revision_id);
  const variables = variablesQuery.data ?? [];
  const boundary = useQueryErrorBoundary(variablesQuery.error);
  const updateStackTemplateConfigMutation = useUpdateStackTemplateConfigMutation(tenantID, stackId);
  const runInFlight = useRunInFlight(stackTemplate.id);

  // Displayed values are the installed config overlaid with unsaved edits.
  const baseValues = variableValuesFromConfig(stackTemplate.config, variables);
  const variableValues: Record<string, string> = {};
  for (const variable of variables) {
    variableValues[variable.name] = editedValues[variable.name] ?? baseValues[variable.name] ?? "";
  }

  const canSaveConfig = canSaveInstalledTemplateConfig(stackTemplate, variables, variableValues);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so an in-flight
  // edit here is never wiped out by a background sign-in redirect.
  const hasUnsavedConfig = Object.keys(editedValues).length > 0;

  function handleVariableValueChange(name: string, value: string) {
    setEditedValues((current) => ({ ...current, [name]: value }));
  }

  async function handleSave() {
    if (!canSaveConfig) {
      return;
    }
    setErrorMessage("");
    try {
      await updateStackTemplateConfigMutation.mutateAsync({
        stackTemplateID: stackTemplate.id,
        body: { config: configFromVariableValues(variables, variableValues) }
      });
      setEditedValues({});
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  if (variablesQuery.status === "pending") {
    return (
      <p className="flex items-center gap-2 text-muted-foreground" data-testid="template-variables-loading">
        <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading variables…
      </p>
    );
  }

  if (variablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <div className="grid justify-items-start gap-4" data-testid="template-variables-error">
        <p className="text-muted-foreground">Something went wrong while loading the template's variables.</p>
        <Button className="pointer-coarse:h-11" data-testid="template-variables-retry" onClick={() => variablesQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </div>
    );
  }

  const configPanelProps = {
    variables,
    variableValues,
    onVariableValueChange: handleVariableValueChange,
    canSave: canSaveConfig,
    onSave: handleSave,
    saveBusy: updateStackTemplateConfigMutation.isPending,
    disabledReason: isDestroyingStackTemplate(stackTemplate)
      ? "Destroy in progress"
      : runInFlight
        ? runInFlightReason(runInFlight, "changing the config")
        : undefined
  };

  return (
    <div className="grid min-w-0 grid-cols-1 content-start gap-6" data-testid="template-variables-tab" data-unsaved={hasUnsavedConfig ? "true" : undefined}>
      {errorMessage && (
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{errorMessage}</AlertTitle>
        </Alert>
      )}
      <RequireCapability
        capability="canOperate"
        fallback={<StackTemplateConfigPanel {...configPanelProps} canSave={false} saveBusy={false} disabledReason="Editing requires operator access" />}
      >
        <StackTemplateConfigPanel {...configPanelProps} />
      </RequireCapability>
    </div>
  );
}

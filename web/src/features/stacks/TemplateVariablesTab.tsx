import { useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { useTemplateRevisionVariablesQuery, useUpdateStackTemplateConfigMutation } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { useLockState, variablesLockReason } from "../runs/lockReasons";
import StackTemplateConfigPanel from "./StackTemplateConfigPanel";
import { useStackTemplate } from "./stackTemplateContext";
import { canSaveInstalledTemplateConfig, configFromVariableValues, variableValuesFromConfig } from "./stackWorkflow";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/variables — the installed
// template's configuration. Choosing another revision is a separate view
// (upgrade), reached from Settings, so this form has exactly one action.
export default function TemplateVariablesTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");

  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, stackTemplate.desired_template_revision_id);
  const variables = variablesQuery.data ?? [];
  const boundary = useQueryErrorBoundary(variablesQuery.error);
  const updateStackTemplateConfigMutation = useUpdateStackTemplateConfigMutation(tenantID, stackId);
  const lockReason = variablesLockReason(useLockState(stackId, stackTemplate));

  // Displayed values are the installed config overlaid with unsaved edits.
  const baseValues = variableValuesFromConfig(stackTemplate.config, variables);
  const variableValues: Record<string, string> = {};
  for (const variable of variables) {
    variableValues[variable.name] = editedValues[variable.name] ?? baseValues[variable.name] ?? "";
  }

  const canSaveConfig = canSaveInstalledTemplateConfig(stackTemplate, variables, variableValues);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so an in-flight
  // edit here is never wiped out by a background sign-in redirect. A lock
  // keeps the edits, so it keeps the marker too.
  const hasUnsavedConfig = Object.keys(editedValues).length > 0;

  async function handleSave() {
    if (!canSaveConfig || lockReason !== "") {
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
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="template-variables-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables…
      </p>
    );
  }

  if (variablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <div className="flex flex-col items-start gap-3" data-testid="template-variables-error">
        <ErrorLine live={false}>Something went wrong while loading the template's variables.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="template-variables-retry"
          onClick={() => void variablesQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3" data-testid="template-variables-tab" data-unsaved={hasUnsavedConfig ? "true" : undefined}>
      <StackTemplateConfigPanel
        variables={variables}
        variableValues={variableValues}
        onVariableValueChange={(name, value) => setEditedValues((current) => ({ ...current, [name]: value }))}
        canSave={canSaveConfig}
        onSave={() => void handleSave()}
        saveBusy={updateStackTemplateConfigMutation.isPending}
        disabledReason={lockReason || undefined}
      />
      {errorMessage && <ErrorLine>{errorMessage}</ErrorLine>}
    </div>
  );
}

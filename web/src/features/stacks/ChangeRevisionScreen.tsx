import { useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { useTemplateRevisionVariablesQuery, useTemplateRevisionsQuery, useChangeStackTemplateRevisionMutation } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, selectTriggerClass } from "../../shared/fieldClass";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { lifecycleLockReason } from "../runs/lockReasons";
import { templateRevisionLabel } from "../templates/templateWorkflow";
import PanelTrail from "./PanelTrail";
import { useStackTemplate } from "./stackTemplateContext";
import {
  configFromVariableValues,
  partitionRevisionChangeVariables,
  revisionChangeCandidates,
  variableValuesFromConfig
} from "./stackWorkflow";
import { stackTemplatePath } from "./templateSelection";
import VariableFields from "./VariableFields";
import { apiErrorMessage, fieldsShown } from "../../api/problems";
import { useFieldErrors } from "../../shared/useFieldErrors";
import RequestIdNote from "../../shared/RequestIdNote";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/change-revision — moving the template
// to another revision of the same template, under Settings. The
// candidates are filtered to the valid ones and what the change does to the
// config is stated outright. The panel around it names the template and
// handles one the stack does not have.
export default function ChangeRevisionScreen() {
  const tenantID = useTenantID();
  const { stackId, stackTemplate } = useStackTemplate();
  const navigate = useNavigate();
  const [chosenTargetID, setChosenTargetID] = useState("");
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");
  const variableErrors = useFieldErrors("#/config/");

  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID, stackTemplate.template_id);
  const candidates = revisionChangeCandidates(templateRevisionsQuery.data ?? [], stackTemplate);
  const targetRevision = candidates.find((revision) => revision.id === chosenTargetID) ?? candidates[0] ?? null;

  const currentVariablesQuery = useTemplateRevisionVariablesQuery(tenantID, stackTemplate.desired_template_revision_id);
  const targetVariablesQuery = useTemplateRevisionVariablesQuery(tenantID, targetRevision?.id ?? "");
  const partition = partitionRevisionChangeVariables(currentVariablesQuery.data ?? [], targetVariablesQuery.data ?? []);

  // Every query feeding the diff belongs here. A failed variables fetch leaves
  // data undefined, which the partition above reads as "that revision declares
  // no variables": a confident and wrong diff. Only treating it as an error
  // stops that.
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error ?? currentVariablesQuery.error ?? targetVariablesQuery.error);

  // useTemplateRevisionVariablesQuery keeps the previous data while it fetches
  // a new key, so after the target changes, status reads "success" while data
  // still holds the previous target's variables. Only isFetching sees that.
  // Left ungated, the notes and the posted config would come from the stale
  // target while target_template_revision_id already names the new one.
  const targetVariablesRefreshing = targetRevision !== null && targetVariablesQuery.isFetching;

  // The config sent covers the target revision's variables only, so anything
  // the new revision dropped is excluded structurally.
  const targetVariables = [...partition.added, ...partition.carried];
  const baseValues = variableValuesFromConfig(stackTemplate.config, targetVariables);
  const variableValues: Record<string, string> = {};
  for (const variable of targetVariables) {
    variableValues[variable.name] = editedValues[variable.name] ?? baseValues[variable.name] ?? "";
  }

  const changeStackTemplateRevisionMutation = useChangeStackTemplateRevisionMutation(tenantID, stackId);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so values typed
  // here are never wiped out by a background sign-in redirect.
  const hasUnsavedValues = Object.keys(editedValues).length > 0;
  const settingsPath = stackTemplatePath(stackId, stackTemplate.id, "settings");
  const trail = <PanelTrail name="Change revision" parent={{ label: "Settings", to: settingsPath }} current="Change revision" />;

  async function handleChangeRevision() {
    if (!targetRevision || targetVariablesRefreshing) {
      return;
    }
    setErrorMessage("");
    variableErrors.clear();
    // A value that isn't JSON is refused here, under its field, before any request.
    const encoded = configFromVariableValues(targetVariables, variableValues);
    if (Object.keys(encoded.errors).length > 0) {
      variableErrors.set(encoded.errors);
      return;
    }
    try {
      await changeStackTemplateRevisionMutation.mutateAsync({
        stackTemplateID: stackTemplate.id,
        body: {
          target_template_revision_id: targetRevision.id,
          config: encoded.config
        }
      });
      navigate(stackTemplatePath(stackId, stackTemplate.id, "runs"));
    } catch (error) {
      variableErrors.show(error);
      setErrorMessage(apiErrorMessage(error, "Request failed", fieldsShown("#/config/", targetVariables.map((variable) => variable.name))));
    }
  }

  // The target's variables query is disabled, and so pending forever, when
  // there is no candidate. Wait on it only once something is being fetched.
  // Both sides matter: gating only the target would compare the real current
  // list against an empty target and call everything carried "new"; gating
  // only the current side would call everything "dropped".
  const waitingOnCurrentVariables = currentVariablesQuery.status === "pending";
  const waitingOnTargetVariables = targetRevision !== null && targetVariablesQuery.status === "pending";

  if (templateRevisionsQuery.status === "pending" || waitingOnCurrentVariables || waitingOnTargetVariables) {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="change-revision-loading">
        {trail}
        <p className="flex items-center gap-2 text-meta text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading revisions…
        </p>
      </section>
    );
  }

  if (templateRevisionsQuery.status === "error" || currentVariablesQuery.status === "error" || targetVariablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="flex min-w-0 flex-col items-start gap-5" data-testid="change-revision-load-error">
        {trail}
        <ErrorLine live={false}>Something went wrong while loading revisions.</ErrorLine>
        <RequestIdNote error={templateRevisionsQuery.error ?? currentVariablesQuery.error ?? targetVariablesQuery.error} />
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="change-revision-retry"
          onClick={() => {
            void templateRevisionsQuery.refetch();
            void currentVariablesQuery.refetch();
            void targetVariablesQuery.refetch();
          }}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </section>
    );
  }

  // Settings disables the link for a template that is not active, but the
  // URL still reaches here. The server refuses the change regardless; this
  // says why instead of drawing a form for an action that cannot succeed.
  const lifecycleReason = lifecycleLockReason(stackTemplate.lifecycle, "revision");
  if (lifecycleReason) {
    return (
      <section className="flex min-w-0 flex-col gap-5" data-testid="change-revision-locked">
        {trail}
        <p className="text-meta text-muted-foreground">{lifecycleReason}</p>
      </section>
    );
  }

  // Select shows the chosen option's label from these.
  const targetItems = candidates.map((candidate) => ({ value: candidate.id, label: templateRevisionLabel(candidate) }));

  return (
    <section className="flex min-w-0 flex-col gap-5" data-testid="change-revision-stack-template-screen" data-unsaved={hasUnsavedValues ? "true" : undefined}>
      {trail}
      {candidates.length === 0 ? (
        <p className="text-meta text-muted-foreground" data-testid="change-revision-no-alternatives">
          No other active revisions available.
        </p>
      ) : (
        <>
          <div className="flex max-w-140 flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="change-revision-target" className={fieldLabelClass}>
                Revision
              </Label>
              <Select
                items={targetItems}
                value={targetRevision?.id ?? null}
                onValueChange={(revisionID) => {
                  // Base UI types the value as nullable; a target is always chosen.
                  if (revisionID === null) return;
                  setChosenTargetID(revisionID);
                  setEditedValues({});
                }}
              >
                <SelectTrigger id="change-revision-target" data-testid="change-revision-target-select" className={selectTriggerClass}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {targetItems.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="font-mono text-meta pointer-coarse:min-h-11">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {targetVariablesRefreshing ? (
              // Inline, not the loading view: a new target is a small change to
              // a drawn view. It replaces only what would be wrong while the
              // previous target's data is still served: the notes and fields.
              <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="change-revision-target-variables-loading">
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables for the selected revision…
              </p>
            ) : (
              <>
                {/* What the change does to the config, stated outright: new
                    variables in muted text, dropped ones in warning, since
                    their values will be lost. */}
                {(partition.added.length > 0 || partition.removed.length > 0) && (
                  <ul className="flex flex-col gap-1 text-meta">
                    {partition.added.map((variable) => (
                      <li key={`added-${variable.name}`} className="text-muted-foreground wrap-anywhere" data-testid={`change-revision-added-${variable.name}`}>
                        <span className="font-mono text-code-foreground">{variable.name}</span> is new in this revision.
                      </li>
                    ))}
                    {partition.removed.map((variable) => (
                      <li key={`removed-${variable.name}`} className="text-warning wrap-anywhere" data-testid={`change-revision-removed-${variable.name}`}>
                        <span className="font-mono">{variable.name}</span> is no longer used and will be dropped.
                      </li>
                    ))}
                  </ul>
                )}
                <VariableFields
                  variables={targetVariables}
                  variableValues={variableValues}
                  onVariableValueChange={(name, value) => {
                    setEditedValues((current) => ({ ...current, [name]: value }));
                    variableErrors.clear(name);
                  }}
                  emptyMessage="This revision declares no variables."
                  errors={variableErrors.errors}
                />
              </>
            )}
          </div>
          {errorMessage && <ErrorLine testId="change-revision-stack-template-error">{errorMessage}</ErrorLine>}
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              className={cn(buttonClass("primary"), "pointer-coarse:h-11")}
              disabled={targetVariablesRefreshing || changeStackTemplateRevisionMutation.isPending}
              onClick={() => void handleChangeRevision()}
            >
              {changeStackTemplateRevisionMutation.isPending ? (
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
              ) : (
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
              )}
              Change revision
            </button>
            <Link to={settingsPath} className={cn(buttonClass("outline"), "pointer-coarse:h-11")}>
              Cancel
            </Link>
          </div>
        </>
      )}
    </section>
  );
}

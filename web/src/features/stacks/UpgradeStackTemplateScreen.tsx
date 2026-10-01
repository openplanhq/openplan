import { useState } from "react";
import { CircleAlert, Loader2, RefreshCw } from "lucide-react";
import { useNavigate, useParams } from "react-router-dom";
import {
  useStackQuery,
  useTemplateRevisionVariablesQuery,
  useTemplateRevisionsQuery,
  useUpgradeStackTemplateMutation
} from "../../api/queries";
import { tenantID } from "../../config";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { toneTextClass } from "../../shared/statusTone";
import { templateRevisionLabel } from "../templates/templateWorkflow";
import {
  configFromVariableValues,
  findSelectedStackTemplate,
  isDestroyingStackTemplate,
  partitionUpgradeVariables,
  stackTemplateLabel,
  upgradeCandidateRevisions,
  variableValuesFromConfig
} from "./stackWorkflow";
import VariableFields from "./VariableFields";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// base.css gives every h2 the legacy 32px display type until PR 9, so each
// heading sets its own family, size, weight and tracking.
const headingClass = "font-heading text-base leading-snug font-medium tracking-normal";

// /stacks/:stackId/templates/:stackTemplateId/upgrade — moving an installed
// template to a different revision of the same source template. The old
// screen hid this behind a tenant-wide revision dropdown whose effect
// depended on an invisible source-template match; here the candidates are
// filtered to the valid ones and the variable changes are stated outright.
//
// Until PR 9, body keeps the legacy text colour, so each state sets its own.
export default function UpgradeStackTemplateScreen() {
  const { stackId = "", stackTemplateId = "" } = useParams<{ stackId: string; stackTemplateId: string }>();
  const navigate = useNavigate();
  const [chosenTargetID, setChosenTargetID] = useState("");
  const [editedValues, setEditedValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");

  const stackQuery = useStackQuery(tenantID, stackId);
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);

  const stackTemplate = findSelectedStackTemplate(stackQuery.data?.templates ?? [], stackTemplateId);
  const candidates = upgradeCandidateRevisions(templateRevisionsQuery.data ?? [], stackTemplate);
  const targetRevision = candidates.find((revision) => revision.id === chosenTargetID) ?? candidates[0] ?? null;

  const currentVariablesQuery = useTemplateRevisionVariablesQuery(
    tenantID,
    stackTemplate?.desired_template_revision_id ?? ""
  );
  const targetVariablesQuery = useTemplateRevisionVariablesQuery(tenantID, targetRevision?.id ?? "");
  const partition = partitionUpgradeVariables(currentVariablesQuery.data ?? [], targetVariablesQuery.data ?? []);

  // Every query feeding the diff belongs here. A failed variables fetch leaves
  // data undefined, which the partition above reads as "that revision declares
  // no variables" — indistinguishable from a genuinely empty revision, and so
  // rendered as a confident (and wrong) diff. The loading guards below cannot
  // catch it either: a failed query is "error", not "pending", and isFetching
  // is false once retries are spent. Only treating it as an error does.
  const boundary = useQueryErrorBoundary(
    stackQuery.error ?? templateRevisionsQuery.error ?? currentVariablesQuery.error ?? targetVariablesQuery.error
  );

  // useTemplateRevisionVariablesQuery sets placeholderData: keepPreviousData,
  // so switching the target dropdown flips targetVariablesQuery.status to
  // "success" immediately while data still serves the *previous* target's
  // variables during the refetch — status/isPending can't see this, only
  // isFetching can (same distinction AddStackTemplateScreen's revision
  // switch relies on). Left ungated, the diff notes and the posted config
  // would both be built from the stale target while target_template_revision_id
  // (computed synchronously above) already points at the new one.
  //
  // currentVariablesQuery does not need the same treatment: its query key is
  // stackTemplate.desired_template_revision_id, which only changes if the
  // installed template itself changes — and nothing on this screen lets a
  // user do that without a fresh navigation to a different stackTemplateId
  // (no in-page control mutates it, unlike the target dropdown). A URL edit
  // or history navigation to a different stackTemplateId while this
  // component stays mounted could in principle hit the same window, but
  // that's not a control this screen exposes, so it's out of scope here.
  const targetVariablesRefreshing = targetRevision !== null && targetVariablesQuery.isFetching;

  // The config sent with the upgrade covers the target revision's variables
  // only, so anything the new revision dropped is excluded structurally.
  const targetVariables = [...partition.added, ...partition.carried];
  const baseValues = stackTemplate ? variableValuesFromConfig(stackTemplate.config, targetVariables) : {};
  const variableValues: Record<string, string> = {};
  for (const variable of targetVariables) {
    variableValues[variable.name] = editedValues[variable.name] ?? baseValues[variable.name] ?? "";
  }

  const upgradeStackTemplateMutation = useUpgradeStackTemplateMutation(tenantID, stackId);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so values typed
  // into the upgrade form are never wiped out by a background sign-in redirect.
  const hasUnsavedValues = Object.keys(editedValues).length > 0;

  async function handleUpgrade() {
    if (!stackTemplate || !targetRevision || targetVariablesRefreshing) {
      return;
    }
    setErrorMessage("");
    try {
      await upgradeStackTemplateMutation.mutateAsync({
        stackTemplateID: stackTemplate.id,
        body: {
          target_template_revision_id: targetRevision.id,
          config: configFromVariableValues(targetVariables, variableValues)
        }
      });
      navigate(`/stacks/${stackId}/templates/${stackTemplate.id}`);
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  // Both variable queries are disabled (and so sit in "pending" forever)
  // whenever there is no revision id to fetch yet — no installed template, or
  // no upgrade candidate. Only wait on a query once something is actually
  // being fetched, mirroring StackTemplateScreen's waitingOnVariables guard.
  // Both sides matter: if only the target were gated, the partition below
  // would compare a real "current" list against an empty "target" list and
  // mislabel everything carried as newly added; if only the current side were
  // gated, first paint would compare an empty "current" against the real
  // target and mislabel everything as dropped.
  const waitingOnCurrentVariables = stackTemplate !== null && currentVariablesQuery.status === "pending";
  const waitingOnTargetVariables = targetRevision !== null && targetVariablesQuery.status === "pending";

  if (
    stackQuery.status === "pending" ||
    templateRevisionsQuery.status === "pending" ||
    waitingOnCurrentVariables ||
    waitingOnTargetVariables
  ) {
    return (
      <section className="text-foreground" data-testid="upgrade-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading revisions…
        </p>
      </section>
    );
  }

  if (
    stackQuery.status === "error" ||
    templateRevisionsQuery.status === "error" ||
    currentVariablesQuery.status === "error" ||
    targetVariablesQuery.status === "error"
  ) {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4 text-foreground" data-testid="upgrade-load-error">
        <p className="text-muted-foreground">Something went wrong while loading revisions.</p>
        <Button
          className="pointer-coarse:h-11"
          data-testid="upgrade-retry"
          onClick={() => {
            stackQuery.refetch();
            templateRevisionsQuery.refetch();
            currentVariablesQuery.refetch();
            targetVariablesQuery.refetch();
          }}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  if (!stackTemplate) {
    return (
      <section className="text-foreground" data-testid="upgrade-template-missing">
        <p className="text-muted-foreground">That template is not installed on this stack.</p>
      </section>
    );
  }

  // Guards the direct-URL path: the template screen already hides/disables
  // the link that would bring a user here while a destroy is in progress
  // (see StackTemplateScreen.tsx), but this route is still reachable
  // directly. The backend rejects the upgrade with a 409 regardless — this
  // just states that outright instead of rendering a form for an action that
  // cannot succeed.
  if (isDestroyingStackTemplate(stackTemplate)) {
    return (
      <section className="text-foreground" data-testid="upgrade-destroying">
        <p className="text-muted-foreground">Destroy in progress — this template cannot change revision right now.</p>
      </section>
    );
  }

  // Select shows the chosen option's label from these.
  const targetItems = candidates.map((candidate) => ({ value: candidate.id, label: templateRevisionLabel(candidate) }));

  return (
    <section
      className="grid gap-6 text-foreground"
      data-testid="upgrade-stack-template-screen"
      data-unsaved={hasUnsavedValues ? "true" : undefined}
    >
      <h2 className={cn(headingClass, "wrap-anywhere")}>{stackTemplateLabel(stackTemplate)}</h2>

      {errorMessage && (
        <Alert variant="destructive" data-testid="upgrade-stack-template-error">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{errorMessage}</AlertTitle>
        </Alert>
      )}

      {candidates.length === 0 ? (
        <Card data-testid="upgrade-no-alternatives">
          <CardContent>
            <p className="text-muted-foreground">No other active revisions available.</p>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="grid gap-6">
            <div className="grid gap-2">
              <Label htmlFor="upgrade-target">Revision to apply</Label>
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
                <SelectTrigger
                  id="upgrade-target"
                  data-testid="upgrade-target-select"
                  className="w-full pointer-coarse:data-[size=default]:h-11"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {targetItems.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="pointer-coarse:min-h-11">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-4">
              <h2 className={headingClass}>Variables</h2>
              {targetVariablesRefreshing ? (
                // Inline, not the full-screen upgrade-loading state: a dropdown
                // change is a small update to an already-rendered screen, not a
                // fresh page load, and a full-screen spinner on every selection
                // would be a worse experience than the bug this suppresses. This
                // only replaces the parts that are wrong while the previous
                // target's data is still being served — the fields themselves
                // and the diff notes below — not the revision picker.
                <p className="flex items-center gap-2 text-muted-foreground" data-testid="upgrade-target-variables-loading">
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading variables for the selected revision…
                </p>
              ) : (
                <>
                  <VariableFields
                    variables={targetVariables}
                    variableValues={variableValues}
                    onVariableValueChange={(name, value) => setEditedValues((current) => ({ ...current, [name]: value }))}
                    emptyMessage="This revision declares no variables"
                  />

                  {/* What the change does to the config, stated outright: new
                      variables in the muted text, dropped ones in the warning
                      tone, since their values will be lost. */}
                  {partition.added.length > 0 && (
                    <ul className="grid gap-1 text-sm text-muted-foreground">
                      {partition.added.map((variable) => (
                        <li key={variable.name} className="wrap-anywhere" data-testid={`upgrade-added-${variable.name}`}>
                          {variable.name} is new in this revision
                        </li>
                      ))}
                    </ul>
                  )}

                  {partition.removed.length > 0 && (
                    <ul className={cn("grid gap-1 text-sm", toneTextClass("waiting"))}>
                      {partition.removed.map((variable) => (
                        <li key={variable.name} className="wrap-anywhere" data-testid={`upgrade-removed-${variable.name}`}>
                          {variable.name} is no longer used and will be dropped
                        </li>
                      ))}
                    </ul>
                  )}
                </>
              )}
            </div>

            {/* Its own width, or the full width on a phone. */}
            <Button
              className="w-full pointer-coarse:h-11 md:w-auto md:justify-self-start"
              disabled={targetVariablesRefreshing || upgradeStackTemplateMutation.isPending}
              onClick={handleUpgrade}
            >
              {upgradeStackTemplateMutation.isPending ? (
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
              ) : (
                <RefreshCw data-icon="inline-start" aria-hidden="true" />
              )}
              Change revision
            </Button>
          </CardContent>
        </Card>
      )}
    </section>
  );
}

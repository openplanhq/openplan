import { useId, useState } from "react";
import type { ReactNode } from "react";
import { Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  useAddTemplateToStackMutation,
  useStackQuery,
  useTemplateRevisionVariablesQuery,
  useTemplateRevisionsQuery
} from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, selectTriggerClass } from "../../shared/fieldClass";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import StatusLabel from "../../shared/StatusLabel";
import { revisionIndicator } from "../templates/revisionIndicator";
import {
  activeRevisions,
  groupTemplatesByRepository,
  latestActiveRevision,
  revisionCountLabel,
  shortCommitSHA,
  templateRootPathLabel
} from "../templates/templateWorkflow";
import type { SourceTemplateGroup } from "../templates/templateWorkflow";
import { configFromVariableValues } from "./stackWorkflow";
import { stackTemplatePath } from "./templateSelection";
import VariableFields from "./VariableFields";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/new — adding a template to the stack, in the
// stack page's panel, under a header of its own in place of a template's.
// The picker lists the registry's templates by repository, one row each; a
// pick resolves to the template's newest *active* revision, and the Revision
// select offers the rest. Installing almost always wants the newest validated
// commit; moving between commits afterwards is Change revision's job.
export default function AddStackTemplateScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const navigate = useNavigate();
  const [chosenRevisionID, setChosenRevisionID] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");
  const chooseHeadingId = useId();
  const configureHeadingId = useId();

  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? "";
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);
  const templateRevisions = templateRevisionsQuery.data ?? [];
  const chosenRevision = templateRevisions.find((revision) => revision.id === chosenRevisionID) ?? null;

  // Grouped once and shared: the picker draws these groups, and the configure
  // section finds the chosen template among them.
  const repositoryGroups = groupTemplatesByRepository(templateRevisions);
  const chosenTemplate =
    repositoryGroups
      .flatMap((group) => group.sourceTemplates)
      .find((sourceTemplate) => sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID)) ?? null;
  // Newest registered first, in the API's order (see activeRevisions), so the
  // select's first option is the one a row pick already chose.
  const chosenTemplateRevisions = chosenTemplate ? activeRevisions(chosenTemplate.revisions) : [];

  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, chosenRevision?.id ?? "");
  const variables = variablesQuery.data ?? [];
  // The variables query keeps the previous revision's variables while it
  // fetches a new one, so only isFetching covers both a first fetch and a
  // switch.
  const variablesLoading = chosenRevision !== null && variablesQuery.isFetching;
  const variablesFailed = chosenRevision !== null && variablesQuery.status === "error";

  const addTemplateToStackMutation = useAddTemplateToStackMutation(tenantID, stackId);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so values typed
  // here are never wiped out by a background sign-in redirect.
  const hasUnsavedValues = Object.keys(values).length > 0;

  function handleChoose(revisionID: string) {
    setChosenRevisionID(revisionID);
    setValues({});
    setErrorMessage("");
  }

  async function handleInstall() {
    // variablesFailed guards the request itself, not just the button: without
    // the variables, configFromVariableValues would post an empty config as
    // if the template needed nothing.
    if (!chosenRevision || variablesLoading || variablesFailed) {
      return;
    }
    setErrorMessage("");
    try {
      const installed = await addTemplateToStackMutation.mutateAsync({
        template_revision_id: chosenRevision.id,
        config: configFromVariableValues(variables, values)
      });
      navigate(stackTemplatePath(stackId, installed.id));
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  if (templateRevisionsQuery.status === "error" && boundary !== null) {
    return <>{boundary}</>;
  }

  // The select's options: the commit and when it was registered. The name and
  // ref are the template's, already on its row.
  const revisionItems = chosenTemplateRevisions.map((candidate, index) => ({
    value: candidate.id,
    label: `${shortCommitSHA(candidate.resolved_commit_sha)} · ${formatTimestamp(candidate.created_at)}${index === 0 ? " · latest" : ""}`
  }));

  let content: ReactNode;
  if (templateRevisionsQuery.status === "pending") {
    content = (
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="add-stack-template-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading templates…
      </p>
    );
  } else if (templateRevisionsQuery.status === "error") {
    content = (
      <div className="flex flex-col items-start gap-3" data-testid="add-stack-template-load-error">
        <ErrorLine live={false}>Something went wrong while loading templates.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="add-stack-template-retry"
          onClick={() => void templateRevisionsQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  } else if (templateRevisions.length === 0) {
    content = (
      <Empty className="gap-3 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="add-stack-template-none">
        <EmptyHeader className="gap-2">
          <p className="text-sm font-medium">No templates registered yet</p>
          <EmptyDescription className="text-meta">Register a template, then add it to this stack.</EmptyDescription>
        </EmptyHeader>
        <Link to="/templates/new" className={cn(buttonClass("primary"), "pointer-coarse:h-11")} data-testid="register-template-link">
          <Plus data-icon="inline-start" aria-hidden="true" />
          Register template
        </Link>
      </Empty>
    );
  } else {
    content = (
      <>
        <section aria-labelledby={chooseHeadingId} className="flex flex-col gap-3">
          <h3 id={chooseHeadingId} className="text-sm font-semibold">
            Choose a template
          </h3>
          {repositoryGroups.map((group) => (
            <div key={group.key} className="overflow-hidden rounded-lg border" data-testid={`template-group-${group.key}`}>
              <h4 className="border-b border-divider bg-canvas px-4 py-2 font-mono text-xs font-normal text-muted-foreground wrap-anywhere">
                {group.repoOwner}/{group.repoName}
              </h4>
              <ul className="divide-y divide-divider">
                {group.sourceTemplates.map((sourceTemplate) => (
                  <li key={sourceTemplate.sourceTemplateID}>
                    <TemplateChoice
                      sourceTemplate={sourceTemplate}
                      picked={sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID)}
                      onPick={handleChoose}
                    />
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </section>
        {chosenRevision && chosenTemplate && (
          <section aria-labelledby={configureHeadingId} className="flex max-w-140 flex-col gap-4" data-testid="add-stack-template-variables">
            <h3 id={configureHeadingId} className="text-sm font-semibold wrap-anywhere">
              Configure {chosenTemplate.name}
            </h3>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="add-template-revision" className={fieldLabelClass}>
                Revision
              </Label>
              <Select
                items={revisionItems}
                value={chosenRevisionID}
                onValueChange={(revisionID) => {
                  // Base UI types the value as nullable; a revision is always chosen.
                  if (revisionID !== null) handleChoose(revisionID);
                }}
              >
                <SelectTrigger id="add-template-revision" data-testid="add-template-revision-select" className={selectTriggerClass}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {revisionItems.map((item) => (
                    <SelectItem key={item.value} value={item.value} className="font-mono text-meta pointer-coarse:min-h-11">
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {variablesLoading ? (
              <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="add-stack-template-variables-loading">
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables…
              </p>
            ) : variablesFailed ? (
              // Not the empty message: a fetch that failed must never read as
              // "this template declares no variables". Inline, so the picker
              // stays usable.
              <div className="flex flex-col items-start gap-3" data-testid="add-stack-template-variables-error">
                <ErrorLine>Could not load this template's variables.</ErrorLine>
                <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => void variablesQuery.refetch()}>
                  <RefreshCw data-icon="inline-start" aria-hidden="true" />
                  Retry
                </button>
              </div>
            ) : (
              <VariableFields
                variables={variables}
                variableValues={values}
                onVariableValueChange={(name, value) => setValues((current) => ({ ...current, [name]: value }))}
                emptyMessage="This template declares no variables."
              />
            )}
            {errorMessage && <ErrorLine testId="add-stack-template-error">{errorMessage}</ErrorLine>}
            <button
              type="button"
              className={cn(buttonClass("primary"), "self-start pointer-coarse:h-11")}
              disabled={variablesLoading || variablesFailed || addTemplateToStackMutation.isPending}
              onClick={() => void handleInstall()}
            >
              {addTemplateToStackMutation.isPending ? (
                <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
              ) : (
                <Plus data-icon="inline-start" aria-hidden="true" />
              )}
              Add template
            </button>
          </section>
        )}
      </>
    );
  }

  return (
    <section className="flex min-w-0 flex-col" data-testid="add-stack-template-screen" data-unsaved={hasUnsavedValues ? "true" : undefined}>
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3 border-b border-divider px-7 py-6">
        <div className="flex min-w-0 flex-col gap-2">
          <h2 className="font-heading text-panel-title font-semibold tracking-title">Add template</h2>
          {stackName && <p className="text-meta text-muted-foreground wrap-anywhere">to {stackName}</p>}
        </div>
        <Link to={`/stacks/${stackId}`} className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")}>
          Cancel
        </Link>
      </div>
      <div className="flex min-w-0 flex-col gap-7 px-7 pt-5 pb-7">{content}</div>
    </section>
  );
}

// One template in the picker: its name, where it lives and its ref, and how
// many revisions it has that can be installed. When its newest revision is
// not active, the row says so; with no active revision at all, that is why
// the row cannot be picked.
function TemplateChoice({
  sourceTemplate,
  picked,
  onPick
}: {
  sourceTemplate: SourceTemplateGroup;
  picked: boolean;
  onPick: (revisionID: string) => void;
}) {
  const installable = latestActiveRevision(sourceTemplate.revisions);
  const installableCount = activeRevisions(sourceTemplate.revisions).length;
  const latestState = revisionIndicator(sourceTemplate.latestRevision.status);
  // The ref tells apart two templates at one path on different refs.
  const where = [templateRootPathLabel(sourceTemplate.rootPath, sourceTemplate.name), sourceTemplate.sourceRef].filter(Boolean).join(" · ");

  return (
    <button
      type="button"
      className={cn(
        "flex min-h-13 w-full items-center justify-between gap-4 px-4 py-2 text-left transition-colors focus-visible:-outline-offset-2 disabled:cursor-not-allowed pointer-coarse:min-h-14",
        picked ? "bg-primary-soft" : "enabled:hover:bg-primary-tint"
      )}
      aria-pressed={picked}
      disabled={installable === null}
      onClick={() => installable && onPick(installable.id)}
      data-testid={`add-template-choice-${sourceTemplate.sourceTemplateID}`}
    >
      <span className="flex min-w-0 flex-col gap-0.5">
        <span
          className={cn(
            "text-sm leading-label font-medium wrap-anywhere",
            picked ? "text-primary-strong" : installable ? "text-foreground" : "text-muted-foreground"
          )}
        >
          {sourceTemplate.name}
        </span>
        {where && <span className="font-mono text-xs text-muted-foreground wrap-anywhere">{where}</span>}
      </span>
      <span className="flex shrink-0 flex-wrap items-center justify-end gap-x-3 gap-y-1">
        {latestState && (
          <StatusLabel icon={latestState.icon} tone={latestState.tone} strong={latestState.strong}>
            {latestState.label}
          </StatusLabel>
        )}
        {installableCount > 0 && <span className="text-meta text-muted-foreground">{revisionCountLabel(installableCount)}</span>}
      </span>
    </button>
  );
}

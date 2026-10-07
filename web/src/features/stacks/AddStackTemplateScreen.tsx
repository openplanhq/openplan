import { useEffect, useId, useRef, useState } from "react";
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
import { activeRevisions, groupTemplatesByRepository, latestActiveRevision, shortCommitSHA } from "../templates/templateWorkflow";
import type { SourceTemplateGroup } from "../templates/templateWorkflow";
import { configFromVariableValues } from "./stackWorkflow";
import { stackTemplatePath } from "./templateSelection";
import TemplateSearch, { TemplateChoiceState, TemplateSource } from "./TemplateSearch";
import VariableFields from "./VariableFields";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/new — adding a template to the stack, in the
// stack page's panel, under a header of its own in place of a template's.
// The registry can hold hundreds of templates, so they are searched rather
// than listed (TemplateSearch). A pick closes the search, shows the template
// in its place with Change, and resolves to its newest *active* revision; the
// Revision select offers the rest. Installing almost always wants the newest
// validated commit; moving between commits afterwards is Change revision's job.
export default function AddStackTemplateScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const navigate = useNavigate();
  const [chosenRevisionID, setChosenRevisionID] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");
  // Change reopens the search over the chosen template, which stays until
  // another is picked.
  const [changingTemplate, setChangingTemplate] = useState(false);
  const chooseHeadingId = useId();
  const configureHeadingId = useId();

  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? "";
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);
  const templateRevisions = templateRevisionsQuery.data ?? [];
  const chosenRevision = templateRevisions.find((revision) => revision.id === chosenRevisionID) ?? null;

  const repositoryGroups = groupTemplatesByRepository(templateRevisions);
  const sourceTemplates = repositoryGroups.flatMap((group) => group.sourceTemplates);
  const chosenTemplate = sourceTemplates.find((sourceTemplate) => sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID)) ?? null;
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

  // Where focus goes once the search has closed and unmounted, so it is never
  // left on the page's body: after a pick, the first thing to fill in; after
  // Escape on Change, Change.
  const focusAfterPickRef = useRef(false);
  const focusChangeRef = useRef(false);
  const variableFieldsRef = useRef<HTMLDivElement>(null);
  const installButtonRef = useRef<HTMLButtonElement>(null);
  const changeButtonRef = useRef<HTMLButtonElement>(null);

  function handlePick(sourceTemplate: SourceTemplateGroup) {
    setChangingTemplate(false);
    // The template already chosen, picked again after Change, is a second
    // look, not a new choice: its revision and values stay.
    if (sourceTemplate === chosenTemplate) {
      focusChangeRef.current = true;
      return;
    }
    const installable = latestActiveRevision(sourceTemplate.revisions);
    if (installable) {
      focusAfterPickRef.current = true;
      handleChoose(installable.id);
    }
  }

  useEffect(() => {
    if (focusChangeRef.current && !changingTemplate) {
      focusChangeRef.current = false;
      changeButtonRef.current?.focus();
    }
  }, [changingTemplate]);

  // Waits for the variables, since the first one is the target.
  useEffect(() => {
    if (!focusAfterPickRef.current || variablesLoading) {
      return;
    }
    focusAfterPickRef.current = false;
    const firstVariable = variableFieldsRef.current?.querySelector("input");
    const installButton = installButtonRef.current?.disabled ? null : installButtonRef.current;
    (firstVariable ?? installButton ?? changeButtonRef.current)?.focus();
  }, [variablesLoading, chosenRevisionID]);

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
        <section aria-labelledby={chooseHeadingId} className="flex max-w-140 flex-col gap-3">
          <h3 id={chooseHeadingId} className="text-sm font-semibold">
            Choose a template
          </h3>
          {chosenTemplate && !changingTemplate ? (
            // The picked ChoiceRow, with Change to search again.
            <div className="overflow-hidden rounded-lg border" data-testid="add-template-picked">
              <div className="flex min-h-13 flex-wrap items-center justify-between gap-x-4 gap-y-2 bg-primary-soft py-2 pr-2.5 pl-4">
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="text-sm leading-label font-medium text-primary-strong wrap-anywhere">{chosenTemplate.name}</span>
                  <TemplateSource sourceTemplate={chosenTemplate} className="wrap-anywhere" />
                </span>
                <span className="flex shrink-0 items-center gap-3">
                  <TemplateChoiceState sourceTemplate={chosenTemplate} />
                  <button
                    ref={changeButtonRef}
                    type="button"
                    className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
                    onClick={() => setChangingTemplate(true)}
                  >
                    Change
                  </button>
                </span>
              </div>
            </div>
          ) : (
            <TemplateSearch
              sourceTemplates={sourceTemplates}
              repositoryCount={repositoryGroups.length}
              autoFocus={changingTemplate}
              onPick={handlePick}
              onDismiss={(reason) => {
                if (!changingTemplate) return;
                setChangingTemplate(false);
                focusChangeRef.current = reason === "escape";
              }}
            />
          )}
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
              <div ref={variableFieldsRef}>
                <VariableFields
                  variables={variables}
                  variableValues={values}
                  onVariableValueChange={(name, value) => setValues((current) => ({ ...current, [name]: value }))}
                  emptyMessage="This template declares no variables."
                />
              </div>
            )}
            {errorMessage && <ErrorLine testId="add-stack-template-error">{errorMessage}</ErrorLine>}
            <button
              ref={installButtonRef}
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

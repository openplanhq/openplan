import { useEffect, useId, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import {
  useAddTemplateToStackMutation,
  useStackQuery,
  useTemplateRevisionVariablesQuery,
  useTemplateRevisionsQuery,
  useTemplatesQuery
} from "../../api/queries";
import type { Template } from "../../api/types";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { fieldLabelClass, selectTriggerClass } from "../../shared/fieldClass";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { activeRevisions, latestActiveRevision, shortCommitSHA, templateDisplayName, templatesWithRevision } from "../templates/templateWorkflow";
import { configFromVariableValues } from "./stackWorkflow";
import { stackTemplatePath } from "./templateSelection";
import TemplateSearch, { TemplateChoiceState, TemplateSource } from "./TemplateSearch";
import VariableFields from "./VariableFields";
import { apiErrorMessage, fieldsShown } from "../../api/problems";
import { useFieldErrors } from "../../shared/useFieldErrors";
import RequestIdNote from "../../shared/RequestIdNote";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/new — adding a template to the stack, in the
// stack page's panel, under a header of its own in place of a template's.
// The registry can hold hundreds of templates, so they are searched rather
// than listed (TemplateSearch). A pick closes the search, shows the template
// in its place with Change, reads its revisions and resolves to the newest
// *active* one; the Revision select offers the rest. Installing almost always
// wants the newest validated commit; moving between commits afterwards is
// Change revision's job.
export default function AddStackTemplateScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const navigate = useNavigate();
  const [chosenTemplateID, setChosenTemplateID] = useState("");
  // "" until someone picks a revision in the select: the newest active one.
  const [chosenRevisionID, setChosenRevisionID] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");
  const variableErrors = useFieldErrors("#/config/");
  // Change reopens the search over the chosen template, which stays until
  // another is picked.
  const [changingTemplate, setChangingTemplate] = useState(false);
  const chooseHeadingId = useId();
  const configureHeadingId = useId();

  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? "";
  const templatesQuery = useTemplatesQuery(tenantID);
  const boundary = useQueryErrorBoundary(templatesQuery.error);
  const sourceTemplates = templatesWithRevision(templatesQuery.data ?? []);
  const repositoryCount = new Set(sourceTemplates.map((template) => `${template.repo_owner}/${template.repo_name}`)).size;
  const chosenTemplate = sourceTemplates.find((template) => template.id === chosenTemplateID) ?? null;

  const chosenRevisionsQuery = useTemplateRevisionsQuery(tenantID, chosenTemplate?.id ?? "");
  const chosenRevisions = chosenRevisionsQuery.data ?? [];
  const revisionsLoading = chosenTemplate !== null && chosenRevisionsQuery.isPending;
  const revisionsFailed = chosenTemplate !== null && chosenRevisionsQuery.status === "error";
  // Newest registered first, in the API's order (see activeRevisions), so the
  // select's first option is the one a pick resolves to.
  const chosenTemplateRevisions = activeRevisions(chosenRevisions);
  const chosenRevision =
    chosenTemplateRevisions.find((revision) => revision.id === chosenRevisionID) ?? (chosenRevisionID === "" ? latestActiveRevision(chosenRevisions) : null);

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

  function handleChoose(templateID: string, revisionID: string) {
    setChosenTemplateID(templateID);
    setChosenRevisionID(revisionID);
    setValues({});
    setErrorMessage("");
    variableErrors.clear();
  }

  // Where focus goes once the search has closed and unmounted, so it is never
  // left on the page's body: after a pick, the first thing to fill in; after
  // Escape on Change, Change.
  const focusAfterPickRef = useRef(false);
  const focusChangeRef = useRef(false);
  const variableFieldsRef = useRef<HTMLDivElement>(null);
  const installButtonRef = useRef<HTMLButtonElement>(null);
  const changeButtonRef = useRef<HTMLButtonElement>(null);

  function handlePick(sourceTemplate: Template) {
    setChangingTemplate(false);
    // The template already chosen, picked again after Change, is a second
    // look, not a new choice: its revision and values stay.
    if (sourceTemplate.id === chosenTemplate?.id) {
      focusChangeRef.current = true;
      return;
    }
    focusAfterPickRef.current = true;
    handleChoose(sourceTemplate.id, "");
  }

  useEffect(() => {
    if (focusChangeRef.current && !changingTemplate) {
      focusChangeRef.current = false;
      changeButtonRef.current?.focus();
    }
  }, [changingTemplate]);

  // Waits for the revisions and the variables, since the first variable is
  // the target.
  useEffect(() => {
    if (!focusAfterPickRef.current || revisionsLoading || variablesLoading) {
      return;
    }
    focusAfterPickRef.current = false;
    const firstVariable = variableFieldsRef.current?.querySelector<HTMLElement>("input, textarea");
    const installButton = installButtonRef.current?.disabled ? null : installButtonRef.current;
    (firstVariable ?? installButton ?? changeButtonRef.current)?.focus();
  }, [revisionsLoading, variablesLoading, chosenTemplateID, chosenRevisionID]);

  async function handleInstall() {
    // variablesFailed guards the request itself, not just the button: without
    // the variables, configFromVariableValues would post an empty config as
    // if the template needed nothing.
    if (!chosenRevision || variablesLoading || variablesFailed) {
      return;
    }
    setErrorMessage("");
    variableErrors.clear();
    // A value that isn't JSON is refused here, under its field, before any request.
    const encoded = configFromVariableValues(variables, values);
    if (Object.keys(encoded.errors).length > 0) {
      variableErrors.set(encoded.errors);
      return;
    }
    try {
      const installed = await addTemplateToStackMutation.mutateAsync({
        template_revision_id: chosenRevision.id,
        config: encoded.config
      });
      navigate(stackTemplatePath(stackId, installed.id));
    } catch (error) {
      // Each refused variable shows under its own input; the line says the rest.
      variableErrors.show(error);
      setErrorMessage(apiErrorMessage(error, "Request failed", fieldsShown("#/config/", variables.map((variable) => variable.name))));
    }
  }

  if (templatesQuery.status === "error" && boundary !== null) {
    return <>{boundary}</>;
  }

  // The select's options: the commit and when it was registered. The name and
  // ref are the template's, already on its row.
  const revisionItems = chosenTemplateRevisions.map((candidate, index) => ({
    value: candidate.id,
    label: `${shortCommitSHA(candidate.resolved_commit_sha)} · ${formatTimestamp(candidate.created_at)}${index === 0 ? " · latest" : ""}`
  }));

  let content: ReactNode;
  if (templatesQuery.status === "pending") {
    content = (
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="add-stack-template-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading templates…
      </p>
    );
  } else if (templatesQuery.status === "error") {
    content = (
      <div className="flex flex-col items-start gap-3" data-testid="add-stack-template-load-error">
        <ErrorLine live={false}>Something went wrong while loading templates.</ErrorLine>
        <RequestIdNote error={templatesQuery.error} />
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="add-stack-template-retry"
          onClick={() => void templatesQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  } else if (sourceTemplates.length === 0) {
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
                  <span className="text-sm leading-label font-medium text-primary-strong wrap-anywhere">{templateDisplayName(chosenTemplate)}</span>
                  <TemplateSource sourceTemplate={chosenTemplate} className="wrap-anywhere" />
                </span>
                <span className="flex shrink-0 items-center gap-3">
                  <TemplateChoiceState revisions={chosenRevisions} />
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
              repositoryCount={repositoryCount}
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
        {revisionsLoading && (
          <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="add-stack-template-revisions-loading">
            <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading revisions…
          </p>
        )}
        {revisionsFailed && (
          <div className="flex flex-col items-start gap-3" data-testid="add-stack-template-revisions-error">
            <ErrorLine>Could not load this template's revisions.</ErrorLine>
            <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => void chosenRevisionsQuery.refetch()}>
              <RefreshCw data-icon="inline-start" aria-hidden="true" />
              Retry
            </button>
          </div>
        )}
        {chosenRevision && chosenTemplate && (
          <section aria-labelledby={configureHeadingId} className="flex max-w-140 flex-col gap-4" data-testid="add-stack-template-variables">
            <h3 id={configureHeadingId} className="text-sm font-semibold wrap-anywhere">
              Configure {templateDisplayName(chosenTemplate)}
            </h3>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="add-template-revision" className={fieldLabelClass}>
                Revision
              </Label>
              <Select
                items={revisionItems}
                value={chosenRevision.id}
                onValueChange={(revisionID) => {
                  // Base UI types the value as nullable; a revision is always chosen.
                  if (revisionID !== null) handleChoose(chosenTemplate.id, revisionID);
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
                  onVariableValueChange={(name, value) => {
                    setValues((current) => ({ ...current, [name]: value }));
                    variableErrors.clear(name);
                  }}
                  emptyMessage="This template declares no variables."
                  errors={variableErrors.errors}
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

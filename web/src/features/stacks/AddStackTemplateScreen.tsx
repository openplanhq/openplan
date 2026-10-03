import { useState } from "react";
import { CircleAlert, Layers, Loader2, RefreshCw, ShieldCheck } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useAddTemplateToStackMutation, useTemplateRevisionVariablesQuery, useTemplateRevisionsQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { formatTimestamp } from "../../shared/formatTimestamp";
import StatusBadge from "../../shared/StatusBadge";
import {
  activeRevisions,
  groupTemplatesByRepository,
  latestActiveRevision,
  revisionCountLabel,
  shortCommitSHA,
  templateRootPathLabel,
  unsettledStatusTone
} from "../templates/templateWorkflow";
import { configFromVariableValues } from "./stackWorkflow";
import VariableFields from "./VariableFields";
import { Alert, AlertAction, AlertTitle } from "@/components/ui/alert";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

// Preflight leaves a heading with the body's type, so each heading sets
// CardTitle's look: family, size, weight and tracking.
const headingClass = "font-heading text-base leading-snug font-medium tracking-normal";

// /stacks/:stackId/templates/new — installing a template, extracted from the
// template screen where an always-present Install button sat next to an
// unrelated config form. The picker mirrors /templates so choosing here looks
// like browsing the registry: one row per template, named, with the ref and
// commit demoted to the row's trailing meta.
//
// The install still posts a template_revision_id, so a row click resolves to
// the template's latest *active* revision and a template with more than one
// offers the rest in a dropdown. Picking a revision outright was the wrong
// default: installing almost always wants the newest validated commit, and
// moving between commits afterwards is UpgradeStackTemplateScreen's job.
//
export default function AddStackTemplateScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const navigate = useNavigate();
  const [chosenRevisionID, setChosenRevisionID] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [errorMessage, setErrorMessage] = useState("");

  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);
  const templateRevisions = templateRevisionsQuery.data ?? [];
  const chosenRevision = templateRevisions.find((revision) => revision.id === chosenRevisionID) ?? null;

  // Grouped once and shared: the picker renders these buckets, and the panel
  // below finds the chosen template among them rather than re-deriving it from
  // the revision's source_template_id — which would need the same empty-id
  // fallback sourceTemplateKey already applies here.
  const repositoryGroups = groupTemplatesByRepository(templateRevisions);
  const chosenTemplate =
    repositoryGroups
      .flatMap((group) => group.sourceTemplates)
      .find((sourceTemplate) => sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID)) ?? null;
  // Newest-registered first, straight from the API's order — see
  // activeRevisions. The dropdown offers these in exactly this order, so its
  // first option is the one a row click already selected.
  const chosenTemplateRevisions = chosenTemplate ? activeRevisions(chosenTemplate.revisions) : [];

  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, chosenRevision?.id ?? "");
  const variables = variablesQuery.data ?? [];
  // useTemplateRevisionVariablesQuery keeps the previous revision's variables
  // visible (placeholderData: keepPreviousData) while it fetches the newly
  // chosen one, so isPending alone misses the revision-switch case — only
  // isFetching covers both "never fetched" and "refetching for a new key".
  const variablesLoading = chosenRevision !== null && variablesQuery.isFetching;
  const variablesFailed = chosenRevision !== null && variablesQuery.status === "error";

  const addTemplateToStackMutation = useAddTemplateToStackMutation(tenantID, stackId);
  // Read by SessionProvider's proactive re-auth timer: it defers navigating
  // away while a `[data-unsaved='true']` element is mounted, so variable
  // values entered here are never wiped out by a background sign-in redirect.
  const hasUnsavedValues = Object.keys(values).length > 0;

  function handleChoose(revisionID: string) {
    setChosenRevisionID(revisionID);
    setValues({});
    setErrorMessage("");
  }

  async function handleInstall() {
    // variablesFailed guards the request itself, not just the button: without
    // a loaded variable list, configFromVariableValues would build an empty
    // config and post it as if the template genuinely needed nothing.
    if (!chosenRevision || variablesLoading || variablesFailed) {
      return;
    }
    setErrorMessage("");
    try {
      const installed = await addTemplateToStackMutation.mutateAsync({
        template_revision_id: chosenRevision.id,
        config: configFromVariableValues(variables, values)
      });
      navigate(`/stacks/${stackId}/templates/${installed.id}`);
    } catch (error) {
      setErrorMessage(error instanceof Error ? error.message : "Request failed");
    }
  }

  if (templateRevisionsQuery.status === "pending") {
    return (
      <section data-testid="add-stack-template-loading">
        <p className="flex items-center gap-2 text-muted-foreground">
          <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading templates…
        </p>
      </section>
    );
  }

  if (templateRevisionsQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4" data-testid="add-stack-template-load-error">
        <p className="text-muted-foreground">Something went wrong while loading templates.</p>
        <Button className="pointer-coarse:h-11" data-testid="add-stack-template-retry" onClick={() => templateRevisionsQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  // The revision picker's options: the commit and its registration date. The
  // name and ref are fixed for the whole template and already named in the
  // row. Select shows the chosen option's label from these.
  const revisionItems = chosenTemplateRevisions.map((candidate, index) => ({
    value: candidate.id,
    label: `${shortCommitSHA(candidate.resolved_commit_sha)} · ${formatTimestamp(candidate.created_at)}${index === 0 ? " · latest" : ""}`
  }));

  return (
    <section
      className="grid gap-6"
      data-testid="add-stack-template-screen"
      data-unsaved={hasUnsavedValues ? "true" : undefined}
    >
      {errorMessage && (
        <Alert variant="destructive" data-testid="add-stack-template-error">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{errorMessage}</AlertTitle>
        </Alert>
      )}

      {templateRevisions.length === 0 ? (
        <Empty className="border" data-testid="add-stack-template-none">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Layers aria-hidden="true" />
            </EmptyMedia>
            <EmptyDescription>No templates are registered for this tenant yet.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Link
              className={cn(buttonVariants(), "pointer-coarse:h-11")}
              to="/templates/new"
              data-testid="register-template-link"
            >
              Register template
            </Link>
          </EmptyContent>
        </Empty>
      ) : (
        // Browse on the left, configure on the right, split 3:4 as the legacy
        // grid's 0.85fr and 1.15fr were. Stacked, the config panel landed
        // under the last repository group and read as belonging to it rather
        // than to the row actually chosen further up the page.
        <div className="grid gap-6 lg:grid-cols-7">
          <div className="grid content-start gap-6 lg:col-span-3">
            {repositoryGroups.map((group) => (
              <section className="grid gap-3" key={group.key} data-testid={`template-group-${group.key}`}>
                {/* No count pill here, unlike the registry. It counts
                    templates while the rows count installable revisions, and
                    two small numbers a few pixels apart reading "1" then
                    "2 revisions" invite the guess that they disagree. */}
                <h2 className="font-mono text-sm font-normal tracking-normal wrap-anywhere text-muted-foreground">
                  {group.repoOwner}/{group.repoName}
                </h2>
                <ul className="grid gap-3">
                  {group.sourceTemplates.map((sourceTemplate) => {
                    const installable = latestActiveRevision(sourceTemplate.revisions);
                    const chosenHere = sourceTemplate.revisions.some((revision) => revision.id === chosenRevisionID);
                    const tone = unsettledStatusTone(sourceTemplate.latestRevision.status);
                    const rootPath = templateRootPathLabel(sourceTemplate.rootPath, sourceTemplate.name);
                    const installableCount = activeRevisions(sourceTemplate.revisions).length;
                    // The commit shown is the one a click would install, which is
                    // not necessarily the newest revision — see
                    // latestActiveRevision.
                    const shownRevision = installable ?? sourceTemplate.latestRevision;
                    return (
                      <li key={sourceTemplate.sourceTemplateID}>
                        {/* The button is the card: the name and root path on
                            the left, the ref and commit on the right, wrapping
                            under the name on a phone. The chosen row takes the
                            primary border and ring. */}
                        <button
                          type="button"
                          className="flex w-full flex-wrap items-start justify-between gap-x-4 gap-y-1 rounded-lg border bg-card px-4 py-2 text-left text-card-foreground shadow-xs transition-colors outline-none hover:bg-muted focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:text-muted-foreground disabled:hover:bg-card data-[selected=true]:border-primary data-[selected=true]:ring-3 data-[selected=true]:ring-primary/20 pointer-coarse:min-h-11"
                          data-testid={`add-template-choice-${sourceTemplate.sourceTemplateID}`}
                          data-selected={chosenHere ? "true" : undefined}
                          // No active revision means nothing here can be
                          // installed. The row stays visible, and its status pill
                          // below says why, rather than leaving a dead button.
                          disabled={installable === null}
                          onClick={() => installable && handleChoose(installable.id)}
                        >
                          <span className="grid min-w-0 gap-1">
                            <span className="text-sm wrap-anywhere">{sourceTemplate.name}</span>
                            {rootPath !== "" && (
                              <span className="font-mono text-xs wrap-anywhere text-muted-foreground">{rootPath}</span>
                            )}
                          </span>
                          <span className="flex flex-wrap items-center gap-3">
                            {tone !== null && <StatusBadge tone={tone}>{sourceTemplate.latestRevision.status}</StatusBadge>}
                            <span className="font-mono text-xs text-muted-foreground">
                              {sourceTemplate.sourceRef} · {shortCommitSHA(shownRevision.resolved_commit_sha)}
                              {/* Only worth saying where it means the dropdown
                                  below has something to offer. */}
                              {installableCount > 1 && <> · {revisionCountLabel(installableCount)}</>}
                            </span>
                          </span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </section>
            ))}
          </div>

          {/* Sticky so the panel stays beside the row that opened it however
              far the list runs on. Below lg the columns stack, where a sticky
              panel would pin the form over the list instead. */}
          <div className="lg:sticky lg:top-6 lg:col-span-4 lg:self-start">
            {chosenRevision && chosenTemplate ? (
              <Card data-testid="add-stack-template-variables">
                <CardHeader>
                  {/* Names what is being configured. Proximity alone was not
                      enough: stacked below the list, this panel read as
                      belonging to whichever template happened to be rendered
                      last. */}
                  <h2 className={cn(headingClass, "wrap-anywhere")}>{chosenTemplate.name}</h2>
                </CardHeader>
                <CardContent className="grid gap-6">
                  {/* Only where there is a choice to make. One active revision
                      needs no control, and the row click has already selected
                      it. */}
                  {revisionItems.length > 1 && (
                    <div className="grid gap-2">
                      <Label htmlFor="add-template-revision">Revision</Label>
                      <Select
                        items={revisionItems}
                        value={chosenRevisionID}
                        onValueChange={(revisionID) => {
                          // Base UI types the value as nullable; a revision is always chosen.
                          if (revisionID !== null) handleChoose(revisionID);
                        }}
                      >
                        <SelectTrigger
                          id="add-template-revision"
                          data-testid="add-template-revision-select"
                          className="w-full pointer-coarse:data-[size=default]:h-11"
                        >
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          {revisionItems.map((item) => (
                            <SelectItem key={item.value} value={item.value} className="pointer-coarse:min-h-11">
                              {item.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  <div className="grid gap-4">
                    <h2 className={headingClass}>Variables</h2>
                    {variablesLoading ? (
                      <p className="flex items-center gap-2 text-muted-foreground" data-testid="add-stack-template-variables-loading">
                        <Loader2 aria-hidden="true" className="size-4 animate-spin" /> Loading variables…
                      </p>
                    ) : variablesFailed ? (
                      // Distinct from the empty message on purpose: an unresolved
                      // fetch must never read as "this template declares no
                      // variables", which is a claim we cannot make when we could
                      // not load them. Kept inline rather than replacing the
                      // screen, so the picker stays usable and another revision
                      // can be chosen.
                      <Alert variant="destructive" data-testid="add-stack-template-variables-error">
                        <CircleAlert aria-hidden="true" />
                        <AlertTitle>Could not load this template&rsquo;s variables.</AlertTitle>
                        <AlertAction>
                          <Button variant="outline" size="sm" className="pointer-coarse:h-11" onClick={() => variablesQuery.refetch()}>
                            <RefreshCw data-icon="inline-start" aria-hidden="true" />
                            Retry
                          </Button>
                        </AlertAction>
                      </Alert>
                    ) : (
                      <VariableFields
                        variables={variables}
                        variableValues={values}
                        onVariableValueChange={(name, value) => setValues((current) => ({ ...current, [name]: value }))}
                        emptyMessage="This template declares no variables."
                      />
                    )}
                  </div>
                  {/* Its own width, or the full width on a phone. */}
                  <Button
                    className="w-full pointer-coarse:h-11 md:w-auto md:justify-self-start"
                    disabled={variablesLoading || variablesFailed || addTemplateToStackMutation.isPending}
                    onClick={handleInstall}
                  >
                    {addTemplateToStackMutation.isPending ? (
                      <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
                    ) : (
                      <ShieldCheck data-icon="inline-start" aria-hidden="true" />
                    )}
                    Install
                  </Button>
                </CardContent>
              </Card>
            ) : (
              // Not a card: a full card of chrome around one sentence
              // outweighed the sentence.
              <p className="text-muted-foreground" data-testid="add-stack-template-unchosen">
                Choose a template to configure and install it.
              </p>
            )}
          </div>
        </div>
      )}
    </section>
  );
}

import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { BookMarked, Folder, GitBranch, GitCommitHorizontal, Loader2, RefreshCw, Trash2 } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Outlet, useLocation, useNavigate, useParams } from "react-router-dom";
import { ApiRequestError } from "../../api/client";
import { apiErrorMessage } from "../../api/problems";
import { queryKeys } from "../../api/queryKeys";
import { useDeleteTemplateMutation, useTemplateRevisionsQuery, useTemplateRevisionVariablesQuery, useTemplatesQuery } from "../../api/queries";
import { problemTypes } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import ErrorPanel from "../../shared/ErrorPanel";
import ExternalLink from "../../shared/ExternalLink";
import InlineConfirm from "../../shared/InlineConfirm";
import RequestIdNote from "../../shared/RequestIdNote";
import UnderlineTabs from "../../shared/UnderlineTabs";
import { TemplateContext } from "./templateContext";
import { githubLinks, rootPathOf } from "./templateLinks";
import { templatePath, templateTabOf } from "./templateRoutes";
import type { TemplatesLocationState } from "./templatesPageOutlet";
import TemplateSync from "./TemplateSync";
import { shortCommitSHA, templateDisplayName } from "./templateWorkflow";
import { cn } from "@/lib/utils";

// The right side of /templates: one template, and its latest revision for
// the commit and the variables. People who can publish get Sync and Delete
// on the name's line. Its name, where it lives on GitHub (the
// source line), what it is for, then
// its Variables and Revisions tabs. The route below draws the tab into <Outlet />, except on
// /templates itself, which passes the first template's Variables tab as
// children.
//
// TemplatesPage has loaded the templates before any panel renders, so the
// template is read from cache; its revisions are read here. The content is
// keyed on the template, so a tab's local state never carries over to
// another.
//
// The list is refetched each time the panel moves to another template, and
// its revisions are read again. A Sync or a registration only
// refreshes them while it is on screen, so one the person walked away from can
// finish unseen; coming back must show its commit.
export default function TemplatePanel({ sourceTemplateId, children }: { sourceTemplateId?: string; children?: ReactNode }) {
  const tenantID = useTenantID();
  const params = useParams<{ sourceTemplateId: string }>();
  const id = sourceTemplateId ?? params.sourceTemplateId ?? "";
  const queryClient = useQueryClient();
  // The template the panel last showed; the first one needs no refetch, as
  // the page has just loaded the list.
  const shownId = useRef(id);
  useEffect(() => {
    if (shownId.current === id) {
      return;
    }
    shownId.current = id;
    void queryClient.invalidateQueries({ queryKey: queryKeys.templates(tenantID), exact: true });
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID, id) });
  }, [id, queryClient]);
  const template = useTemplatesQuery(tenantID).data?.find((candidate) => candidate.id === id) ?? null;
  const revisionsQuery = useTemplateRevisionsQuery(tenantID, template?.id ?? "");
  const revisions = revisionsQuery.data ?? [];
  const latest = revisions.find((revision) => revision.id === template?.latest_template_revision_id) ?? revisions[0];
  // No count while the previous revision's variables stand in for the newest.
  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, latest?.id ?? "");
  const variableCount = variablesQuery.isPlaceholderData ? undefined : variablesQuery.data?.length;
  const tab = templateTabOf(useLocation().pathname);
  const remove = useTemplateDelete(id);

  if (template !== null && revisionsQuery.isPending) {
    return (
      <p className="flex items-center gap-2 px-7 py-6 text-meta text-muted-foreground" role="status" data-testid="template-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading template…
      </p>
    );
  }

  if (revisionsQuery.isError) {
    return (
      <div className="flex flex-col items-start gap-3 px-7 py-6" data-testid="template-error">
        <ErrorLine live={false}>Something went wrong while loading this template.</ErrorLine>
        <RequestIdNote error={revisionsQuery.error} />
        <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => void revisionsQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  }

  if (template === null) {
    return (
      <p className="px-7 py-6 text-meta text-muted-foreground" data-testid="template-missing">
        That template is not registered.
      </p>
    );
  }

  // Registered, but no sync has recorded a revision yet: its first sync is
  // still running, or it failed. There is no commit, description, variable or
  // revision to show, so the panel is its header alone, and TemplateSync says
  // why, from the template's latest_sync.
  const description = template.description.trim();
  const links = githubLinks(latest ?? { ...template, resolved_commit_sha: "" });
  const rootPath = rootPathOf(template.root_path);

  const name = templateDisplayName(template);
  const heading = <h2 className="min-w-0 font-heading text-title wrap-anywhere">{name}</h2>;
  // Delete sits on the name's line just before Sync, a Small Subtle button
  // like it, as one pair. Both are for people who can publish, so they come
  // and go together. A template a stack has installed, even one since
  // destroyed, can't be deleted, so it is left out rather than offered and
  // refused: nobody could use it here.
  const actions = (
    <RequireCapability capability="can_publish_template">
      {!template.installed && (
        <button
          ref={remove.triggerRef}
          type="button"
          className={cn(buttonClass("subtle", "sm"), "pointer-coarse:h-11")}
          aria-expanded={remove.confirming}
          onClick={remove.ask}
          data-testid="template-delete"
        >
          <Trash2 data-icon="inline-start" aria-hidden="true" strokeWidth={2.42} className="size-3.25" />
          Delete
        </button>
      )}
    </RequireCapability>
  );
  // The question, or why the server refused, under the source line: close
  // to Delete, and above the description it would otherwise push away.
  const deleteStep = remove.confirming ? (
    <InlineConfirm
      testId="template-delete-confirm"
      question={`Delete ${name}?`}
      confirmLabel="Delete template"
      confirmIcon={Trash2}
      busy={remove.busy}
      onConfirm={() => void remove.confirm(name)}
      onCancel={remove.cancel}
    >
      {latest ? "Stacks can no longer add it, and its revisions go with it." : undefined}
    </InlineConfirm>
  ) : remove.refusal === "in-use" ? (
    <ErrorPanel testId="template-delete-error" title="Can't delete this template">
      A stack has installed it, so it stays registered, even after that stack destroys it.
    </ErrorPanel>
  ) : remove.refusal !== null ? (
    <ErrorPanel testId="template-delete-error" title="Could not delete this template" details={remove.refusal.message} />
  ) : null;
  const source = (
    <ul aria-label="Source on GitHub" className="grid min-w-0 grid-cols-4 items-start gap-x-6 text-caption" data-testid="template-source">
      <SourceFact icon={BookMarked} term="Repository" href={links.repository}>
        {`${template.repo_owner}/${template.repo_name}`}
      </SourceFact>
      <SourceFact icon={Folder} term="Root path" href={links.tree} mono={rootPath !== null}>
        {rootPath ?? "repository root"}
      </SourceFact>
      <SourceFact icon={GitBranch} term="Ref" href={links.ref}>
        {template.source_ref}
      </SourceFact>
      {/* Its column stays empty with no commit, so the other three sit
          where they sit for every template. */}
      {latest && (
        <SourceFact icon={GitCommitHorizontal} term="Latest commit" href={links.commit}>
          {shortCommitSHA(latest.resolved_commit_sha)}
        </SourceFact>
      )}
    </ul>
  );
  // Before a revision there is no template.yaml read yet, so no
  // description to miss.
  const about = !latest ? (
    deleteStep
  ) : (
    <>
      {deleteStep}
      {description !== "" ? (
        <p className="max-w-170 text-meta text-foreground" data-testid="template-description">
          {description}
        </p>
      ) : (
        <RequireCapability capability="can_publish_template">
          <p className="max-w-170 text-caption text-muted-foreground" data-testid="template-no-description">
            No description. Add a <span className="font-mono text-code-foreground">template.yaml</span> beside the module with a name, a description
            and tags.
          </p>
        </RequireCapability>
      )}
      {template.tags.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {template.tags.map((tag) => (
            <span key={tag} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-caption text-tag-foreground">
              {tag}
            </span>
          ))}
        </div>
      )}
    </>
  );

  // One tree whether or not there is a revision yet, so TemplateSync stays
  // mounted when a first sync lands one, and its outcome stays in view.
  return (
    <TemplateContext.Provider value={latest ? { template, revisions, latest } : null}>
      <div className="flex min-w-0 flex-col" data-testid="template-panel">
        <div className={cn("flex flex-col gap-5 px-7 pt-6", latest ? "border-b border-divider" : "pb-7")}>
          {/* The name with Sync on its line, the source line under it, then
              what the template is for. Keyed on the template, so a sync
              stays with it. */}
          <TemplateSync key={id} template={template} heading={heading} actions={actions} source={source} about={about} />
          {latest && (
            <UnderlineTabs
              label="Template sections"
              tabs={[
                { to: templatePath(id, "variables"), label: "Variables", current: tab === "variables", count: variableCount },
                { to: templatePath(id, "revisions"), label: "Revisions", current: tab === "revisions", count: revisions.length }
              ]}
            />
          )}
        </div>
        {latest && (
          <div key={id} className="flex min-w-0 flex-col gap-3 px-7 pt-5 pb-7">
            {children ?? <Outlet />}
          </div>
        )}
      </div>
    </TemplateContext.Provider>
  );
}

type DeleteRefusal = "in-use" | { message: string };

// Deleting a template: Delete asks first, in place, then calls the server.
// Gone, it sends the person to /templates, which says what was deleted and
// shows the first template left. Refused because a stack has installed it
// (409 template-in-use), it says so: Delete is only offered while the
// template read says no stack has, so this is a stack that installed it
// after the page loaded. A template someone else deleted first (404) ends
// where a delete does.
//
// The state belongs to the template it was asked about, so moving to another
// template drops a question or a refusal rather than carrying it over.
function useTemplateDelete(id: string) {
  const tenantID = useTenantID();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const deleteTemplateMutation = useDeleteTemplateMutation(tenantID);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const [askedFor, setAskedFor] = useState<string | null>(null);
  const [refusal, setRefusal] = useState<{ id: string; refusal: DeleteRefusal } | null>(null);
  const busy = deleteTemplateMutation.isPending;

  function done(name: string) {
    // Replace: Back must not return to the address of a template that is gone.
    const state: TemplatesLocationState = { deletedTemplate: name };
    navigate("/templates", { replace: true, state });
  }

  return {
    triggerRef,
    confirming: askedFor === id,
    busy,
    refusal: refusal?.id === id ? refusal.refusal : null,
    ask() {
      setRefusal(null);
      setAskedFor(id);
    },
    cancel() {
      setAskedFor(null);
      triggerRef.current?.focus();
    },
    async confirm(name: string) {
      if (busy) {
        return;
      }
      try {
        await deleteTemplateMutation.mutateAsync(id);
        done(name);
      } catch (error) {
        if (error instanceof ApiRequestError && error.status === 404) {
          // The mutation only clears its reads on success; the list must
          // drop the template here too.
          await queryClient.invalidateQueries({ queryKey: queryKeys.templates(tenantID), exact: true });
          done(name);
          return;
        }
        const inUse = error instanceof ApiRequestError && error.type === problemTypes.templateInUse;
        setAskedFor(null);
        setRefusal({ id, refusal: inUse ? "in-use" : { message: apiErrorMessage(error, "Request failed") } });
        // The confirm that held focus is gone; Delete is where to try again.
        triggerRef.current?.focus();
      }
    }
  };
}

// One fact of the source line: an icon saying what it is (its word is in a
// tooltip and, for a screen reader, in the link), then its value as a link to
// GitHub. The four sit in equal columns, as one row of a table, so each starts
// at the same place whatever the others hold. A long value wraps inside its
// column after a "/" or "-", never cut, never mid-name.
function SourceFact({ icon: Icon, term, href, mono = true, children }: { icon: LucideIcon; term: string; href: string; mono?: boolean; children: string }) {
  return (
    <li className="flex min-w-0 items-start gap-1.5">
      <span title={term} className="mt-px flex shrink-0 text-subtle-foreground">
        <Icon aria-hidden="true" className="size-3.5" />
      </span>
      <ExternalLink href={href} site="GitHub" term={term} mono={mono} className="text-caption">
        {children}
      </ExternalLink>
    </li>
  );
}

import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Outlet, useLocation, useParams } from "react-router-dom";
import { queryKeys } from "../../api/queryKeys";
import { useTemplateRevisionsQuery, useTemplateRevisionVariablesQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import ExternalLink from "../../shared/ExternalLink";
import { formatTimestamp } from "../../shared/formatTimestamp";
import UnderlineTabs from "../../shared/UnderlineTabs";
import { TemplateContext } from "./templateContext";
import { githubLinks, rootPathOf } from "./templateLinks";
import { templatePath, templateTabOf } from "./templateRoutes";
import TemplateSync from "./TemplateSync";
import { revisionsForSourceTemplate, shortCommitSHA, templateDisplayName } from "./templateWorkflow";

// The right side of /templates: one template, read from its newest revision.
// Its name, what it is for, where it lives on GitHub, then its Variables and
// Revisions tabs. The route below draws the tab into <Outlet />, except on
// /templates itself, which passes the first template's Variables tab as
// children.
//
// TemplatesPage has loaded the revisions before any panel renders, so the
// template is read from cache. The content is keyed on the template, so a
// tab's local state never carries over to another.
//
// The list is refetched each time the panel moves to another template. A Sync
// or a registration only refreshes it while it is on screen, so one the
// person walked away from can finish unseen; coming back must show its commit.
export default function TemplatePanel({ sourceTemplateId, children }: { sourceTemplateId?: string; children?: ReactNode }) {
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
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
  }, [id, queryClient]);
  const revisions = revisionsForSourceTemplate(useTemplateRevisionsQuery(tenantID).data ?? [], id);
  const latest = revisions[0];
  const variableCount = useTemplateRevisionVariablesQuery(tenantID, latest?.id ?? "").data?.length;
  const tab = templateTabOf(useLocation().pathname);

  if (!latest) {
    return (
      <p className="px-7 py-6 text-meta text-muted-foreground" data-testid="template-missing">
        That template is not registered.
      </p>
    );
  }

  const description = latest.description.trim();
  const links = githubLinks(latest);
  const rootPath = rootPathOf(latest.root_path);

  return (
    <TemplateContext.Provider value={{ sourceTemplateId: id, revisions, latest }}>
      <div className="flex min-w-0 flex-col" data-testid="template-panel">
        <div className="flex flex-col gap-5 border-b border-divider px-7 pt-6">
          <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
            <div className="flex min-w-0 flex-1 basis-80 flex-col gap-3">
              <h2 className="font-heading text-panel-title font-semibold tracking-title wrap-anywhere">{templateDisplayName(latest)}</h2>
              {description !== "" ? (
                <p className="max-w-170 text-sm text-foreground" data-testid="template-description">
                  {description}
                </p>
              ) : (
                <RequireCapability capability="canPublishTemplate">
                  <p className="max-w-170 text-meta text-muted-foreground" data-testid="template-no-description">
                    No description. Add a <span className="font-mono text-code-foreground">template.yaml</span> beside the module with a name, a
                    description and tags.
                  </p>
                </RequireCapability>
              )}
              {latest.tags.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {latest.tags.map((tag) => (
                    <span key={tag} className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-xs text-tag-foreground">
                      {tag}
                    </span>
                  ))}
                </div>
              )}
            </div>
            {/* Hidden rather than disabled without the capability: the POST
                would be refused, so there is nothing anyone could do to make
                it work. Keyed on the template, so a sync stays with it. */}
            <RequireCapability capability="canPublishTemplate">
              <TemplateSync key={id} />
            </RequireCapability>
          </div>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4 rounded-lg border px-5 py-4 lg:grid-cols-4" data-testid="template-details">
            <Detail term="Repository">
              <ExternalLink href={links.repository} site="GitHub">
                {latest.repo_owner}/{latest.repo_name}
              </ExternalLink>
            </Detail>
            <Detail term="Root path">
              <ExternalLink href={links.tree} site="GitHub" mono={rootPath !== null}>
                {rootPath ?? "the repository root"}
              </ExternalLink>
            </Detail>
            <Detail term="Ref">
              <ExternalLink href={links.ref} site="GitHub">
                {latest.source_ref}
              </ExternalLink>
            </Detail>
            <Detail term="Latest commit">
              <ExternalLink href={links.commit} site="GitHub">
                {shortCommitSHA(latest.resolved_commit_sha)}
              </ExternalLink>
              <span aria-hidden="true" className="text-separator">
                ·
              </span>
              <time dateTime={latest.created_at} title={latest.created_at}>
                {formatTimestamp(latest.created_at)}
              </time>
            </Detail>
          </dl>
          <UnderlineTabs
            label="Template sections"
            tabs={[
              { to: templatePath(id, "variables"), label: "Variables", current: tab === "variables", count: variableCount },
              { to: templatePath(id, "revisions"), label: "Revisions", current: tab === "revisions", count: revisions.length }
            ]}
          />
        </div>
        <div key={id} className="flex min-w-0 flex-col gap-3 px-7 pt-5 pb-7">
          {children ?? <Outlet />}
        </div>
      </div>
    </TemplateContext.Provider>
  );
}

// One fact about the template: its term over its value, as openplan UI's
// DetailList draws them.
function Detail({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className="flex min-w-0 flex-wrap items-center gap-1.5 text-meta text-muted-foreground">{children}</dd>
    </div>
  );
}

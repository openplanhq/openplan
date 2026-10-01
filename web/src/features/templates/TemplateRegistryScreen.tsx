import { Loader2, Plus, RefreshCw } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { useTemplateRevisionsQuery } from "../../api/queries";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import StatusBadge from "../../shared/StatusBadge";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { buttonVariants, Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import {
  groupTemplatesByRepository,
  revisionCountLabel,
  shortCommitSHA,
  templateRootPathLabel,
  unsettledStatusTone
} from "./templateWorkflow";

// /templates lists one row per template, not per revision: a template
// accumulates a revision for every distinct commit its ref resolves to, so
// listing revisions here grows a wall of rows that differ only by SHA. The
// commits live behind the row, at /templates/:sourceTemplateId.
//
// Selection is URL state (?selected=<revisionID>) rather than component state
// so that the registration screen can hand back the revision it just minted,
// and so a highlighted row survives a reload or a shared link. The param names
// a revision, so the row highlighted is the template that revision belongs to.
export default function TemplateRegistryScreen() {
  const [searchParams] = useSearchParams();
  const selectedTemplateRevisionID = searchParams.get("selected") ?? "";
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);

  if (templateRevisionsQuery.status === "pending") {
    return (
      <section className="grid min-w-0 gap-6 text-foreground" data-testid="template-registry-loading">
        <p className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
          <Loader2 className="size-4 animate-spin" /> Loading templates…
        </p>
      </section>
    );
  }

  if (templateRevisionsQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid min-w-0 gap-6 text-foreground" data-testid="template-registry-error">
        <Breadcrumb items={[{ label: "Templates" }]} />
        <Alert variant="destructive">
          <AlertDescription>Something went wrong while loading templates.</AlertDescription>
        </Alert>
        <Button
          className="justify-self-start pointer-coarse:h-11"
          type="button"
          data-testid="template-registry-retry"
          onClick={() => templateRevisionsQuery.refetch()}
        >
          <RefreshCw className="size-4" />
          Retry
        </Button>
      </section>
    );
  }

  const templateRevisions = templateRevisionsQuery.data;

  return (
    <section className="grid min-w-0 gap-6 text-foreground">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <Breadcrumb items={[{ label: "Templates" }]} className="mb-0" />
        <Link className={buttonVariants({ className: "pointer-coarse:h-11" })} to="/templates/new" data-testid="register-template-link">
          <Plus className="size-4" />
          Register template
        </Link>
      </header>
      {templateRevisions.length === 0 ? (
        <Empty className="rounded-lg border px-6 py-8" data-testid="templates-list-empty">
          <EmptyHeader>
            <EmptyTitle>No templates yet</EmptyTitle>
            <EmptyDescription>Register a Terraform module to make it available to your stacks.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="grid min-w-0 content-start gap-6" data-testid="templates-list">
          {groupTemplatesByRepository(templateRevisions).map((group) => (
            <Card className="min-w-0 gap-0" key={group.key} data-testid={`template-group-${group.key}`}>
              <CardHeader className="flex flex-row items-center justify-between gap-3 border-b py-3">
                <h2 className="min-w-0 break-all font-heading text-sm font-medium tracking-normal">
                  {group.repoOwner}/{group.repoName}
                </h2>
                <Badge variant="outline" className="shrink-0" data-testid={`template-group-count-${group.key}`}>
                  {group.sourceTemplates.length}
                </Badge>
              </CardHeader>
              <CardContent className="p-0">
                <ul className="divide-y">
                  {group.sourceTemplates.map((sourceTemplate) => {
                    const selected = sourceTemplate.revisions.some(
                      (revision) => revision.id === selectedTemplateRevisionID
                    );
                    const { status } = sourceTemplate.latestRevision;
                    const unsettledTone = unsettledStatusTone(status);
                    const rootPath = templateRootPathLabel(sourceTemplate.rootPath, sourceTemplate.name);
                    return (
                      <li
                        key={sourceTemplate.sourceTemplateID}
                        className="flex min-w-0 flex-wrap items-center justify-between gap-4 p-4 hover:bg-muted/50 data-[selected=true]:bg-muted/50"
                        data-testid={`template-row-${sourceTemplate.sourceTemplateID}`}
                        data-selected={selected ? "true" : undefined}
                        aria-current={selected ? "true" : undefined}
                      >
                        <div className="grid min-w-0 gap-1">
                          <Link
                            className="block min-h-8 break-all font-medium text-foreground no-underline hover:text-primary hover:underline pointer-coarse:min-h-11 pointer-coarse:py-3"
                            to={`/templates/${encodeURIComponent(sourceTemplate.sourceTemplateID)}`}
                          >
                            {sourceTemplate.name}
                          </Link>
                          {rootPath !== "" && (
                            <span className="break-all font-mono text-xs text-muted-foreground">{rootPath}</span>
                          )}
                        </div>
                        <span className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-3 text-xs text-muted-foreground">
                          {unsettledTone !== null && <StatusBadge tone={unsettledTone}>{status}</StatusBadge>}
                          <span className="break-all font-mono">
                            {sourceTemplate.sourceRef} · {shortCommitSHA(sourceTemplate.latestRevision.resolved_commit_sha)} ·{" "}
                            {revisionCountLabel(sourceTemplate.revisions.length)}
                          </span>
                        </span>
                      </li>
                    );
                  })}
                </ul>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </section>
  );
}

import ExternalLink from "../../shared/ExternalLink";
import { formatTimestamp } from "../../shared/formatTimestamp";
import StatusLabel from "../../shared/StatusLabel";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { revisionIndicator } from "./revisionIndicator";
import { useTemplate } from "./templateContext";
import { githubLinks } from "./templateLinks";
import { shortCommitSHA } from "./templateWorkflow";

const cellClass = "px-2 py-2 first:pl-4 last:pr-4";

// /templates/:id/revisions: every commit registered from the template's ref,
// in the API's order, newest first. Each commit links to GitHub. An active
// revision needs no word; any other state says what it is.
export default function RevisionsTab() {
  const { revisions, latest } = useTemplate();

  return (
    <section className="flex min-w-0 flex-col gap-3" data-testid="template-revisions">
      <p className="text-meta text-muted-foreground">
        Every commit registered from <span className="font-mono text-code-foreground">{latest.source_ref}</span>, newest first. A stack keeps the
        revision it was added with until someone changes it.
      </p>
      <div className="overflow-x-auto rounded-lg border">
        <Table className="min-w-md">
          <colgroup>
            <col className="w-56" />
            <col />
          </colgroup>
          <TableHeader>
            <TableRow className="border-divider hover:bg-transparent">
              {["Commit", "Registered"].map((heading) => (
                <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-xs font-medium text-muted-foreground")}>
                  {heading}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((templateRevision, index) => {
              const indicator = revisionIndicator(templateRevision.status);
              return (
                <TableRow key={templateRevision.id} className="h-12 border-divider hover:bg-transparent" data-testid={`revision-row-${templateRevision.id}`}>
                  <TableCell className={cellClass}>
                    <span className="flex flex-wrap items-center gap-2">
                      <ExternalLink href={githubLinks(templateRevision).commit} site="GitHub">
                        {shortCommitSHA(templateRevision.resolved_commit_sha)}
                      </ExternalLink>
                      {index === 0 && (
                        <span className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-xs text-tag-foreground" data-testid="revision-latest">
                          latest
                        </span>
                      )}
                      {indicator && (
                        <StatusLabel icon={indicator.icon} tone={indicator.tone} strong={indicator.strong}>
                          {indicator.label}
                        </StatusLabel>
                      )}
                    </span>
                  </TableCell>
                  <TableCell className={cn(cellClass, "text-meta text-muted-foreground")}>
                    <time dateTime={templateRevision.created_at} title={templateRevision.created_at}>
                      {formatTimestamp(templateRevision.created_at)}
                    </time>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

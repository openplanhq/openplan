import { useEffect, useRef, useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useParams, useSearchParams } from "react-router-dom";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import {
  useRegisterTemplateMutation,
  useTemplateRegistrationQuery,
  useTemplateRevisionsQuery
} from "../../api/queries";
import NotFound from "../../app/NotFound";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";
import StatusBadge from "../../shared/StatusBadge";
import { formatTimestamp } from "../../shared/formatTimestamp";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  revisionsForSourceTemplate,
  shortCommitSHA,
  templateDisplayName,
  templateRootPathLabel,
  unsettledStatusTone
} from "./templateWorkflow";

// /templates/:sourceTemplateId — every commit registered for one template.
//
// There is no source-template endpoint; the tenant-wide revisions list already
// carries `source_template_id` on every row, so this filters the same cache the
// registry screen renders. That list arrives ordered `created_at desc, id desc`
// and is not re-sorted here: sorting on `created_at` alone would drop the id
// tiebreaker and let same-timestamp rows shuffle between renders.
//
// The repository, root path and ref are part of the template's identity and so
// are fixed for every row below — they are stated once in the header rather
// than repeated down the list. Only the commit varies.
//
// Sync re-registers that same identity. A template tracks a ref, but its
// revisions only appear when someone registers it again, and retyping the four
// fields at /templates/new risks a typo minting a second template instead. The
// header already holds every field the POST needs, so the button just replays
// them through the ordinary RegisterTemplate → TemplateSyncWorkflow path.
export default function TemplateDetailScreen() {
  const { sourceTemplateId = "" } = useParams<{ sourceTemplateId: string }>();
  const [searchParams] = useSearchParams();
  const selectedTemplateRevisionID = searchParams.get("selected") ?? "";
  const templateRevisionsQuery = useTemplateRevisionsQuery(tenantID);
  const boundary = useQueryErrorBoundary(templateRevisionsQuery.error);

  // Read before the early returns below, because hooks cannot be. The rows are
  // derived here too so the sync handler can reach the identity they share.
  const revisions = revisionsForSourceTemplate(templateRevisionsQuery.data ?? [], sourceTemplateId);
  const latestRevision = revisions[0];

  const queryClient = useQueryClient();
  const registerTemplateMutation = useRegisterTemplateMutation(tenantID);
  const [syncRegistrationID, setSyncRegistrationID] = useState("");
  const [syncRequestError, setSyncRequestError] = useState("");
  // The revisions on screen when Sync was pressed. A sync whose registration
  // comes back naming one of them resolved the ref to a commit this template
  // already has — the insert hit its `do nothing` conflict and the existing
  // revision was reused — which is the only way to tell "nothing changed"
  // apart from "a new commit arrived": both are a `completed` registration.
  const revisionIDsBeforeSync = useRef<ReadonlySet<string>>(new Set());
  const syncRegistrationQuery = useTemplateRegistrationQuery(tenantID, syncRegistrationID);

  const syncRegistration = syncRegistrationQuery.data ?? null;
  const syncStatus = syncRegistration?.status ?? null;
  const syncSettled = syncStatus !== null && isTerminalRegistrationStatus(syncStatus);
  // Polling the registration failed (403/404, expired session, 5xx). React
  // Query keeps the last `pending` data through the error, so without this the
  // flow would read as busy forever with nothing on screen to say why.
  const syncPollError =
    syncRegistrationID !== "" && syncRegistrationQuery.isError
      ? syncRegistrationQuery.error instanceof Error
        ? syncRegistrationQuery.error.message
        : "Request failed"
      : "";
  // Busy spans the whole flow, not just the POST: the request is in flight, or
  // a registration is being polled and has not reached a terminal status.
  const syncing =
    registerTemplateMutation.isPending || (syncRegistrationID !== "" && !syncSettled && !syncPollError);

  const syncedRevisionID = syncRegistration?.template_revision_id ?? "";
  const syncedSomethingNew =
    syncStatus === "completed" &&
    syncedRevisionID !== "" &&
    !revisionIDsBeforeSync.current.has(syncedRevisionID);
  // A terminal status that is not `completed` is a failure the user has to
  // read. `error_summary` is the registration's own words; the status is the
  // fallback for the rare failure that recorded none.
  const syncFailure = syncSettled && syncStatus !== "completed";
  const syncErrorMessage =
    syncRequestError || syncPollError || (syncFailure ? syncRegistration?.error_summary || `Sync ${syncStatus}` : "");

  useEffect(() => {
    if (syncStatus !== "completed") {
      return;
    }
    // The workflow writes the revision; only a refetch of the tenant-wide list
    // this screen filters can show it.
    queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
  }, [syncStatus, syncedRevisionID, queryClient]);

  async function handleSync() {
    if (!latestRevision) {
      return;
    }
    setSyncRequestError("");
    // Drop any previous attempt so a retry polls the new registration rather
    // than reading the old terminal one.
    setSyncRegistrationID("");
    revisionIDsBeforeSync.current = new Set(revisions.map((revision) => revision.id));
    try {
      const next = await registerTemplateMutation.mutateAsync({
        repo_owner: latestRevision.repo_owner,
        repo_name: latestRevision.repo_name,
        source_ref: latestRevision.source_ref,
        root_path: latestRevision.root_path
      });
      setSyncRegistrationID(next.id);
    } catch (error) {
      setSyncRequestError(error instanceof Error ? error.message : "Request failed");
    }
  }

  if (templateRevisionsQuery.status === "pending") {
    return (
      <section className="grid min-w-0 gap-6" data-testid="template-detail-loading">
        <p className="flex items-center gap-2 text-sm text-muted-foreground" role="status">
          <Loader2 className="size-4 animate-spin" /> Loading revisions…
        </p>
      </section>
    );
  }

  if (templateRevisionsQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid min-w-0 gap-6" data-testid="template-detail-error">
        <Breadcrumb items={[{ label: "Templates", to: "/templates" }, { label: "Template" }]} />
        <Alert variant="destructive">
          <AlertDescription>Something went wrong while loading this template.</AlertDescription>
        </Alert>
        <Button
          className="justify-self-start pointer-coarse:h-11"
          type="button"
          data-testid="template-detail-retry"
          onClick={() => templateRevisionsQuery.refetch()}
        >
          <RefreshCw className="size-4" />
          Retry
        </Button>
      </section>
    );
  }

  if (!latestRevision) {
    // An id that matches nothing is a bad URL, not an empty template: a source
    // template only exists once it has a revision.
    return <NotFound />;
  }

  const name = templateDisplayName(latestRevision);
  const rootPath = templateRootPathLabel(latestRevision.root_path, "");

  return (
    <section className="grid min-w-0 gap-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <Breadcrumb
          items={[{ label: "Templates", to: "/templates", testId: "template-detail-back" }, { label: name }]}
          detail={
            <span data-testid="template-detail-identity">
              {latestRevision.repo_owner}/{latestRevision.repo_name}
              {rootPath !== "" && <> · {rootPath}</>} · {latestRevision.source_ref}
            </span>
          }
          className="mb-0"
        />

        {/* Hidden rather than disabled without the permission: the POST would
            be a 403, so there is nothing the user could do to make it work. */}
        <RequireCapability capability="canPublishTemplate">
          <div className="grid justify-items-end gap-2">
            <Button
              variant="outline"
              className="pointer-coarse:h-11"
              type="button"
              data-testid="template-sync"
              disabled={syncing}
              onClick={handleSync}
            >
              {syncing ? <Loader2 className="size-4 animate-spin" /> : <RefreshCw className="size-4" />}
              Sync
            </Button>
            {/* A new commit needs no words — it arrives as a new top row. An
                unchanged ref would otherwise look like nothing happened.
                The region is mounted empty rather than with its text, because
                a live region inserted already populated is not announced. */}
            <p className="text-right text-sm text-muted-foreground" aria-live="polite" role="status">
              {syncStatus === "completed" && !syncedSomethingNew && (
                <span data-testid="template-sync-result">Already up to date</span>
              )}
            </p>
            {syncErrorMessage !== "" && (
              <Alert variant="destructive" className="max-w-md" data-testid="template-sync-error">
                <AlertDescription>{syncErrorMessage}</AlertDescription>
              </Alert>
            )}
          </div>
        </RequireCapability>
      </header>

      <Card className="min-w-0 gap-0">
        <CardHeader className="border-b py-3">
          <h2 className="font-heading text-base leading-snug font-medium tracking-normal">Revisions</h2>
        </CardHeader>
        <CardContent className="p-0">
          <Table data-testid="template-revisions">
            <colgroup>
              <col className="w-32" />
              <col className="w-32" />
              <col className="w-40" />
              <col className="w-24" />
            </colgroup>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Commit</TableHead>
                <TableHead scope="col">Registered</TableHead>
                <TableHead scope="col">Status</TableHead>
                <TableHead scope="col">Latest</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {revisions.map((revision, index) => {
                const tone = unsettledStatusTone(revision.status);
                const selected = revision.id === selectedTemplateRevisionID;
                return (
                  <TableRow
                    key={revision.id}
                    data-testid={`revision-row-${revision.id}`}
                    data-selected={selected ? "true" : undefined}
                    aria-current={selected ? "true" : undefined}
                  >
                    <TableCell>
                      <span className={selected ? "font-mono text-sm text-primary" : "font-mono text-sm"}>
                        {shortCommitSHA(revision.resolved_commit_sha)}
                      </span>
                    </TableCell>
                    {/* Registration time, not the commit's authoring date — the
                        commit's own date is not stored on the revision. */}
                    <TableCell className="text-sm text-muted-foreground">
                      <time dateTime={revision.created_at} title={revision.created_at}>
                        {formatTimestamp(revision.created_at)}
                      </time>
                    </TableCell>
                    <TableCell>
                      {tone !== null && <StatusBadge tone={tone}>{revision.status}</StatusBadge>}
                    </TableCell>
                    {/* Rendered on every row, hidden where it does not apply, so
                        the marker reserves its width for the status column. */}
                    <TableCell>
                      <span
                        className={`font-mono text-xs text-muted-foreground ${index === 0 ? "" : "invisible"}`}
                        data-latest={index === 0 ? "true" : undefined}
                        data-testid={index === 0 ? "revision-latest" : undefined}
                      >
                        latest
                      </span>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </section>
  );
}

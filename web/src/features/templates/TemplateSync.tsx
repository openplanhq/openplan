import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw } from "lucide-react";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useRegisterTemplateMutation, useTemplateRegistrationQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { cn } from "@/lib/utils";
import { useTemplate } from "./templateContext";
import { shortCommitSHA } from "./templateWorkflow";

// Sync re-registers the template's own identity, so a branch it tracks picks
// up a new commit. Retyping the four fields at /templates/new could mint a
// second template from a typo; the panel already holds every field the POST
// needs.
//
// It polls the registration until it settles, then says what happened in
// words, in a status line mounted empty so the words are announced when they
// arrive: the commit it registered, or that nothing was new. A new commit can
// only be told from an old one by the revisions on screen when Sync was
// pressed: a registration that resolves to a commit the template has reuses
// that revision, and both read "completed".
//
// The panel mounts one per template (key), so a sync never follows the person
// to another template.
export default function TemplateSync() {
  const { revisions, latest } = useTemplate();
  const queryClient = useQueryClient();
  const registerTemplateMutation = useRegisterTemplateMutation(tenantID);
  const [registrationID, setRegistrationID] = useState("");
  const [requestError, setRequestError] = useState("");
  const revisionIDsBefore = useRef<ReadonlySet<string>>(new Set());
  const registrationQuery = useTemplateRegistrationQuery(tenantID, registrationID);

  const registration = registrationQuery.data ?? null;
  const status = registration?.status ?? null;
  const settled = status !== null && isTerminalRegistrationStatus(status);
  // React Query keeps the last `pending` data through a failing poll, so
  // without this the flow would read as busy forever.
  const pollError =
    registrationID !== "" && registrationQuery.isError
      ? registrationQuery.error instanceof Error
        ? registrationQuery.error.message
        : "Request failed"
      : "";
  const syncing = registerTemplateMutation.isPending || (registrationID !== "" && !settled && pollError === "");
  const syncedRevisionID = registration?.template_revision_id ?? "";
  const completed = status === "completed" && syncedRevisionID !== "";
  const somethingNew = completed && !revisionIDsBefore.current.has(syncedRevisionID);
  const failed = settled && status !== "completed";
  const errorMessage = requestError || pollError || (failed ? registration?.error_summary || "Sync failed" : "");

  useEffect(() => {
    if (status !== "completed") {
      return;
    }
    // The workflow writes the revision; only a refetch of the tenant's list
    // shows it.
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID) });
  }, [status, syncedRevisionID, queryClient]);

  async function handleSync() {
    if (syncing) {
      return;
    }
    setRequestError("");
    // Drop any earlier attempt, so this one polls its own registration.
    setRegistrationID("");
    revisionIDsBefore.current = new Set(revisions.map((templateRevision) => templateRevision.id));
    try {
      const next = await registerTemplateMutation.mutateAsync({
        repo_owner: latest.repo_owner,
        repo_name: latest.repo_name,
        source_ref: latest.source_ref,
        root_path: latest.root_path
      });
      setRegistrationID(next.id);
    } catch (error) {
      setRequestError(error instanceof Error ? error.message : "Request failed");
    }
  }

  return (
    <div className="flex max-w-sm shrink-0 flex-col items-end gap-1.5">
      <button
        type="button"
        className={cn(buttonClass("outline", "lg"), "pointer-coarse:h-11")}
        disabled={syncing}
        onClick={() => void handleSync()}
        data-testid="template-sync"
      >
        {syncing ? (
          <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
        ) : (
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
        )}
        Sync
      </button>
      <p className="min-h-4 text-right text-meta text-muted-foreground" role="status" aria-live="polite" data-testid="template-sync-result">
        {completed &&
          (somethingNew ? (
            <>
              Registered commit <span className="font-mono text-code-foreground">{shortCommitSHA(registration?.resolved_commit_sha ?? "")}</span>.
            </>
          ) : (
            "Already up to date."
          ))}
      </p>
      {errorMessage !== "" && <ErrorLine testId="template-sync-error">{errorMessage}</ErrorLine>}
    </div>
  );
}

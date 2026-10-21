import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Loader2, LoaderCircle, RefreshCw } from "lucide-react";
import { isTerminalRegistrationStatus } from "../../api/polling";
import { apiErrorMessage } from "../../api/problems";
import { queryKeys } from "../../api/queryKeys";
import { useRegisterTemplateMutation, useTemplateRegistrationQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorPanel from "../../shared/ErrorPanel";
import StatusLabel from "../../shared/StatusLabel";
import { cn } from "@/lib/utils";
import { useTemplate } from "./templateContext";
import { shortCommitSHA } from "./templateWorkflow";

// Sync re-registers the template's own identity, so a branch it tracks picks
// up a new commit. Retyping the four fields at /templates/new could mint a
// second template from a typo; the panel already holds every field the POST
// needs.
//
// It sits at the right of the header's top row, beside the template's name,
// description and tags (intro), as a subtle button: it keeps the template
// current, but the header is about the template. It polls the registration
// until it settles, then says what happened under that row: the commit it
// registered, that nothing was new, or, in an ErrorPanel, that it failed. A new commit can
// only be told from an old one by the revisions on screen when Sync was
// pressed: a registration that resolves to a commit the template has reuses
// that revision, and both read "completed".
//
// The panel mounts one per template (key), so a sync never follows the person
// to another template.
export default function TemplateSync({ intro }: { intro: ReactNode }) {
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
      ? apiErrorMessage(registrationQuery.error, "Request failed")
      : "";
  const syncing = registerTemplateMutation.isPending || (registrationID !== "" && !settled && pollError === "");
  const syncedRevisionID = registration?.template_revision_id ?? "";
  const completed = status === "completed" && syncedRevisionID !== "";
  const somethingNew = completed && !revisionIDsBefore.current.has(syncedRevisionID);
  const failed = settled && status !== "completed";
  const errorMessage = requestError || pollError || (failed ? registration?.error_summary || "Sync failed" : "");
  // A poll that fails says nothing about the sync itself, which may still
  // finish.
  const unchecked = requestError === "" && pollError !== "";

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
      setRequestError(apiErrorMessage(error, "Request failed"));
    }
  }

  // One place for every outcome, under the header's top row and over the
  // template's facts, beside the Latest commit it may have changed. The status
  // region is mounted empty, so its words are announced when they arrive; its
  // negative margin cancels the header's gap while it is empty.
  return (
    <>
      <div className="flex items-start justify-between gap-6">
        <div className="flex min-w-0 flex-1 flex-col gap-3">{intro}</div>
        {/* Pulled out by its own padding, so its words end on the content's
            edge and it centres on the name's line, not the row's top. While
            a sync runs it stays focusable, marked aria-disabled, and ignores
            presses: disabling it would drop a keyboard user's focus. */}
        <button type="button" className={cn(buttonClass("subtle"), "-my-0.75 -mr-3 shrink-0 pointer-coarse:h-11")} aria-disabled={syncing ? "true" : undefined}
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
      </div>
      <div className="-mt-5 flex flex-col" role="status" aria-live="polite" data-testid="template-sync-result">
        {syncing ? (
          <p className="mt-5">
            <StatusLabel icon={LoaderCircle} tone="settled">
              syncing
            </StatusLabel>
          </p>
        ) : completed ? (
          <p className="mt-5">
            <StatusLabel icon={Check} tone="settled">
              {somethingNew ? (
                <span>
                  synced commit <span className="font-mono text-foreground">{shortCommitSHA(registration?.resolved_commit_sha ?? "")}</span>
                </span>
              ) : (
                "up to date"
              )}
            </StatusLabel>
          </p>
        ) : null}
      </div>
      {!syncing && errorMessage !== "" && (
        <ErrorPanel
          testId="template-sync-error"
          title={unchecked ? "Could not check on this sync" : "Sync failed"}
          details={errorMessage}
        >
          {unchecked ? "It may still finish. Reload the page to see what it registered." : undefined}
        </ErrorPanel>
      )}
    </>
  );
}

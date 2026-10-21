import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Check, Loader2, LoaderCircle, Minus, RefreshCw } from "lucide-react";
import { isSyncSettled } from "../../api/polling";
import { apiErrorMessage } from "../../api/problems";
import { queryKeys } from "../../api/queryKeys";
import { useSyncTemplateMutation, useTemplateQuery } from "../../api/queries";
import type { Template, TemplateLatestSync } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorPanel from "../../shared/ErrorPanel";
import { formatDateTime } from "../../shared/formatTimestamp";
import StatusLabel from "../../shared/StatusLabel";
import { cn } from "@/lib/utils";
import { shortCommitSHA } from "./templateWorkflow";

// Sync asks the server to read the template's ref again, so a branch it
// tracks picks up a new commit.
//
// It sits at the right of the template's name, a small subtle button over
// the source line (source) and the description and tags (about): it keeps the
// template current, but the header is about the template. Once pressed, it
// follows the template's latest_sync until it settles, then says what
// happened under them: the commit it synced, that nothing was new, or, in an
// ErrorPanel, that it failed. The server tells a new commit from a known one
// (synced or up_to_date).
//
// Until Sync is pressed, the panel shows the template's latest sync from the
// list, whoever started it: an outcome as it stands, and a sync still running
// (started elsewhere, or before a reload) followed until it settles. For a
// template with no revision yet, that first sync says why nothing can be
// installed (still syncing, failed, stalled).
//
// Each outcome says when it happened: one read on a later day is not news,
// and a stalled sync is told from a slow one by when it started.
//
// Sync is hidden rather than disabled for people who cannot publish: the
// POST would be refused, so there is nothing anyone could do to make it
// work. They still see the latest sync's outcome. The template's other
// actions (Delete) come in as actions, and sit just before Sync.
//
// The panel mounts one per template (key), so a sync never follows the person
// to another template.
export default function TemplateSync({
  template,
  heading,
  actions,
  source,
  about
}: {
  template: Template;
  heading: ReactNode;
  actions?: ReactNode;
  source: ReactNode;
  about: ReactNode;
}) {
  const queryClient = useQueryClient();
  const syncTemplateMutation = useSyncTemplateMutation(tenantID);
  // Read once, at mount: once the first sync lands a revision the panel
  // keeps this component, and its outcome stays in view.
  const [unsynced] = useState(template.latest_template_revision_id === "");
  // Whether a running sync is being followed; the template query stays off
  // until then, so a settled outcome costs no request.
  const [following, setFollowing] = useState(template.latest_sync?.status === "syncing");
  // Once Sync is pressed, the outcome shown is that sync's alone.
  const [pressed, setPressed] = useState(false);
  const [requestError, setRequestError] = useState("");
  const templateQuery = useTemplateQuery(tenantID, following ? template.id : "");

  // Until the first read answers, the list's copy of the template says where
  // its sync stood, so a failed first sync never flashes "syncing".
  const sync = following ? (templateQuery.data ?? template).latest_sync : pressed ? null : template.latest_sync;
  const status = sync?.status ?? null;
  const settled = status !== null && isSyncSettled(status);
  // React Query keeps the last syncing data through a failing poll, so
  // without this the flow would read as busy forever.
  const pollError = following && templateQuery.isError ? apiErrorMessage(templateQuery.error, "Request failed") : "";
  const syncing = syncTemplateMutation.isPending || (following && !settled && pollError === "");
  const completed = status === "synced" || status === "up_to_date";
  const somethingNew = status === "synced";
  // Stalled is a failure too: the sync did not finish, and pressing Sync
  // again is the way on.
  const failed = settled && !completed;
  // A stalled sync has no words of its own, and a failure may come without
  // any: the headline says it, so the details are left out rather than
  // repeating it under itself.
  const stalled = status === "stalled";
  const errorMessage = requestError || pollError || (failed ? (sync?.error_summary ?? "") : "");
  const showError = !syncing && (errorMessage !== "" || failed);
  // A poll that fails says nothing about the sync itself, which may still
  // finish.
  const unchecked = requestError === "" && pollError !== "";
  // A refused request or a lost poll has no sync to date.
  const when = requestError !== "" || pollError !== "" ? null : syncTime(sync);

  useEffect(() => {
    // An outcome already in when the panel opened changed nothing since the
    // list and the revisions were read.
    if (!following || !somethingNew) {
      return;
    }
    // The sync wrote a revision, which may rename the template: refetch the
    // list and the template's revisions.
    void queryClient.invalidateQueries({ queryKey: queryKeys.templates(tenantID), exact: true });
    void queryClient.invalidateQueries({ queryKey: queryKeys.templateRevisions(tenantID, template.id) });
  }, [following, somethingNew, template.id, queryClient]);

  async function handleSync() {
    if (syncing) {
      return;
    }
    setRequestError("");
    // Drop any earlier outcome, so this one follows its own sync.
    setPressed(true);
    setFollowing(false);
    try {
      await syncTemplateMutation.mutateAsync(template.id);
      setFollowing(true);
    } catch (error) {
      setRequestError(apiErrorMessage(error, "Request failed"));
    }
  }

  // One place for every outcome, under the template's name, source line,
  // description and tags, so the Latest commit it may have changed is in view. The status
  // region is mounted empty, so its words are announced when they arrive; its
  // negative margin cancels the header's gap while it is empty.
  return (
    <>
      <div className="flex min-w-0 flex-col gap-3">
        <div className="flex min-w-0 flex-col gap-2.5">
          <div className="flex min-w-0 items-start justify-between gap-6">
            {heading}
            {/* The actions, then Sync, 4px apart, at the small size, so they
                sit on the name's line without outgrowing it. Pulled out by
                their own padding, so Sync's words end on the content's edge
                and the name's line keeps its height. While a sync runs Sync
                stays focusable, marked aria-disabled, and ignores presses:
                disabling it would drop a keyboard user's focus. */}
            <div className="-my-px -mr-2.5 flex shrink-0 items-start gap-1">
              {actions}
              <RequireCapability capability="can_publish_template">
                <button
                  type="button"
                  className={cn(buttonClass("subtle", "sm"), "pointer-coarse:h-11")}
                  aria-disabled={syncing ? "true" : undefined}
                  onClick={() => void handleSync()}
                  data-testid="template-sync"
                >
                  {syncing ? (
                    <Loader2 data-icon="inline-start" aria-hidden="true" strokeWidth={2.42} className="size-3.25 animate-spin" />
                  ) : (
                    <RefreshCw data-icon="inline-start" aria-hidden="true" strokeWidth={2.42} className="size-3.25" />
                  )}
                  Sync
                </button>
              </RequireCapability>
            </div>
          </div>
          {source}
        </div>
        {about}
      </div>
      <div className="-mt-5 flex flex-col" role="status" aria-live="polite" data-testid="template-sync-result">
        {syncing ? (
          <p className="mt-5">
            <StatusLabel icon={LoaderCircle} tone="settled">
              <span>
                syncing
                <When at={when} />
              </span>
            </StatusLabel>
          </p>
        ) : completed ? (
          <p className="mt-5">
            <StatusLabel icon={Check} tone="settled">
              {/* One run of text, so the time follows the words at a space,
                  not at the label's wider icon gap. */}
              <span>
                {somethingNew ? (
                  <>
                    synced commit <span className="font-mono text-foreground">{shortCommitSHA(sync?.resolved_commit_sha ?? "")}</span>
                  </>
                ) : (
                  "up to date"
                )}
                <When at={when} />
              </span>
            </StatusLabel>
          </p>
        ) : unsynced && !showError ? (
          // No sync to speak of (none requested, or one that ended without
          // a revision): the template simply has nothing to install yet.
          <p className="mt-5">
            <StatusLabel icon={Minus} tone="idle" data-testid="template-no-revision">
              no revision
            </StatusLabel>
          </p>
        ) : null}
      </div>
      {showError && (
        <ErrorPanel
          testId="template-sync-error"
          title={
            unchecked ? (
              "Could not check on this sync"
            ) : (
              <>
                {stalled ? "Sync stalled" : "Sync failed"}
                <When at={when} />
              </>
            )
          }
          details={errorMessage}
        >
          {unchecked ? (
            "It may still finish. Reload the page to see what it registered."
          ) : stalled || (unsynced && failed) ? (
            // What the failure leaves behind; for a stall, also the way on:
            // nothing broke that a person must fix first, so syncing again is
            // it. Only people who can press Sync are told to.
            <>
              {unsynced ? "No revision yet, so stacks can't add this template." : "It didn't finish."}
              {stalled && <RequireCapability capability="can_publish_template"> Sync again.</RequireCapability>}
            </>
          ) : undefined}
        </ErrorPanel>
      )}
    </>
  );
}

// When a sync ended; for one still running, or stalled, when it started,
// since it has not ended.
function syncTime(sync: TemplateLatestSync | null | undefined): { at: string; started: boolean } | null {
  if (!sync) {
    return null;
  }
  if (sync.status === "syncing" || sync.status === "stalled") {
    return sync.requested_at ? { at: sync.requested_at, started: true } : null;
  }
  return sync.completed_at ? { at: sync.completed_at, started: false } : null;
}

// The time after an outcome's words, " · 21 Oct, 09:13", muted and at weight
// 400 whatever the words around it, as the stack page's "Applied <date>" is.
function When({ at }: { at: { at: string; started: boolean } | null }) {
  if (at === null) {
    return null;
  }
  return (
    <span className="font-normal text-muted-foreground" data-testid="template-sync-when">
      {" · "}
      {at.started && "started "}
      <time dateTime={at.at} title={at.at}>
        {formatDateTime(at.at)}
      </time>
    </span>
  );
}

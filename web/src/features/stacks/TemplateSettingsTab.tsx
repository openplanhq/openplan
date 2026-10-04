import { Link } from "react-router-dom";
import { useTemplateRevisionsQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import TemplateDestroyPanel from "../runs/TemplateDestroyPanel";
import { revisionLockReason, useLockState } from "../runs/lockReasons";
import { revisionSourceLabel, shortCommitSHA } from "../templates/templateWorkflow";
import SettingsSection from "./SettingsSection";
import { useStackTemplate } from "./stackTemplateContext";
import { stackTemplatePath } from "./templateSelection";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/settings — the actions that
// change what the template is rather than run it: choosing another revision,
// and destroying it. They live here, a tab away from Plan, so the
// irreversible one is never under the cursor of routine work.
export default function TemplateSettingsTab() {
  const { stackId, stackTemplate } = useStackTemplate();
  const reason = revisionLockReason(useLockState(stackId, stackTemplate));
  // Read only to name the commit and where it comes from. Every revision of a
  // template shares its ref, so the commit is what tells them apart. The
  // sentence stands without it while the tenant's revisions load.
  const revision =
    useTemplateRevisionsQuery(tenantID).data?.find((candidate) => candidate.id === stackTemplate.desired_template_revision_id) ?? null;
  const commit = revision ? shortCommitSHA(revision.resolved_commit_sha) : "";
  const actionClass = cn(buttonClass("outline"), "pointer-coarse:h-11");

  return (
    <div className="flex min-w-0 flex-col gap-5" data-testid="template-settings-tab">
      <SettingsSection
        title="Revision"
        testId="stack-template-revision-action"
        description={
          <>
            This template runs revision{" "}
            <span className="font-mono text-code-foreground">
              {stackTemplate.source_ref}
              {commit && ` · ${commit}`}
            </span>
            {revision && (
              <>
                {" "}
                of <span className="font-mono text-code-foreground">{revisionSourceLabel(revision)}</span>
              </>
            )}
            .
          </>
        }
        reason={reason}
        reasonTestId="upgrade-disabled-reason"
        action={
          reason ? (
            <button type="button" disabled className={actionClass} data-testid="change-stack-template-revision-link">
              Change revision
            </button>
          ) : (
            <Link
              to={stackTemplatePath(stackId, stackTemplate.id, "upgrade")}
              className={actionClass}
              data-testid="change-stack-template-revision-link"
            >
              Change revision
            </Link>
          )
        }
      />
      <TemplateDestroyPanel stackId={stackId} stackTemplate={stackTemplate} />
    </div>
  );
}

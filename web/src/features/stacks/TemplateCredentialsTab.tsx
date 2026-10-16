import { RefreshCw } from "lucide-react";
import {
  useCreateStackTemplateCredentialMutation,
  useDeleteStackTemplateCredentialMutation,
  useStackTemplateCredentialsQuery
} from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import CredentialsPanel from "./CredentialsPanel";
import { useStackTemplate } from "./stackTemplateContext";
import { cn } from "@/lib/utils";

// /stacks/:stackId/templates/:stackTemplateId/credentials — credentials that
// override the stack's environment for this template only. The route itself is
// gated on canManageAccess, the same capability the stack Environment tab needs.
export default function TemplateCredentialsTab() {
  const { stackTemplate } = useStackTemplate();
  const credentialsQuery = useStackTemplateCredentialsQuery(tenantID, stackTemplate.id);
  const createMutation = useCreateStackTemplateCredentialMutation(tenantID, stackTemplate.id);
  const deleteMutation = useDeleteStackTemplateCredentialMutation(tenantID, stackTemplate.id);
  const boundary = useQueryErrorBoundary(credentialsQuery.error);

  // A list that never arrived is not an empty one: "No credentials for this
  // template" would tell someone its runs use the stack environment when they
  // may not, and invite a second copy of a credential already there. So the
  // form waits for the list too. A failed refetch keeps the list it had.
  if (credentialsQuery.status === "error" && credentialsQuery.data === undefined) {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <div className="flex flex-col items-start gap-3" data-testid="template-credentials-error">
        <ErrorLine live={false}>Something went wrong while loading this template's credentials.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="template-credentials-retry"
          onClick={() => void credentialsQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col" data-testid="template-credentials-tab">
      <CredentialsPanel
        note="Overrides the stack environment for this template only."
        emptyTitle="No credentials for this template"
        emptyDescription="Its runs use the stack environment."
        credentials={credentialsQuery.data ?? []}
        loading={credentialsQuery.isPending}
        busy={createMutation.isPending || deleteMutation.isPending}
        onCreate={async (name, value) => {
          await createMutation.mutateAsync({ name, value });
        }}
        onDelete={(id) => deleteMutation.mutateAsync(id)}
      />
    </div>
  );
}

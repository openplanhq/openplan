import {
  useCreateStackTemplateCredentialMutation,
  useDeleteStackTemplateCredentialMutation,
  useStackTemplateCredentialsQuery
} from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import EnvironmentTabs from "./EnvironmentTabs";
import { useStackTemplate } from "./stackTemplateContext";
import LoadError from "../../shared/LoadError";

// /stacks/:stackId/templates/:stackTemplateId/environment — variables that
// override the stack's environment for this template only. The route itself is
// gated on canManageAccess, the same capability the stack Environment tab needs.
export default function TemplateEnvironmentTab() {
  const tenantID = useTenantID();
  const { stackTemplate } = useStackTemplate();
  const credentialsQuery = useStackTemplateCredentialsQuery(tenantID, stackTemplate.id);
  const createMutation = useCreateStackTemplateCredentialMutation(tenantID, stackTemplate.id);
  const deleteMutation = useDeleteStackTemplateCredentialMutation(tenantID, stackTemplate.id);
  const boundary = useQueryErrorBoundary(credentialsQuery.error);

  // A list that never arrived is not an empty one: "No overrides for this
  // template" would tell someone its runs use the stack environment when they
  // may not, and invite a second copy of a variable already there. So the
  // form waits for the list too. A failed refetch keeps the list it had.
  if (credentialsQuery.status === "error" && credentialsQuery.data === undefined) {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <LoadError error={credentialsQuery.error} onRetry={() => void credentialsQuery.refetch()} testId="template-environment-error" retryTestId="template-environment-retry">
        Something went wrong while loading this template's environment.
      </LoadError>
    );
  }

  return (
    <div className="flex min-w-0 flex-col" data-testid="template-environment-tab">
      <EnvironmentTabs
        secrets={{
          emptyTitle: "No secret overrides for this template",
          emptyDescription: "Its runs use the stack's secrets."
        }}
        variables={{
          emptyTitle: "No variable overrides for this template",
          emptyDescription: "Its runs use the stack's variables."
        }}
        credentials={credentialsQuery.data ?? []}
        loading={credentialsQuery.isPending}
        busy={createMutation.isPending || deleteMutation.isPending}
        onCreate={async (credential) => {
          await createMutation.mutateAsync(credential);
        }}
        onDelete={(id) => deleteMutation.mutateAsync(id)}
      />
    </div>
  );
}

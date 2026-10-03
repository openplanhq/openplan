import {
  useCreateStackTemplateCredentialMutation,
  useDeleteStackTemplateCredentialMutation,
  useStackTemplateCredentialsQuery
} from "../../api/queries";
import { tenantID } from "../../config";
import CredentialsPanel from "./CredentialsPanel";
import { useStackTemplate } from "./stackTemplateContext";

// /stacks/:stackId/templates/:stackTemplateId/credentials — credentials that
// override the stack's environment for this template only. The route itself is
// gated on canManageAccess, the same capability the stack Environment tab needs.
export default function TemplateCredentialsTab() {
  const { stackTemplate } = useStackTemplate();
  const credentialsQuery = useStackTemplateCredentialsQuery(tenantID, stackTemplate.id);
  const createMutation = useCreateStackTemplateCredentialMutation(tenantID, stackTemplate.id);
  const deleteMutation = useDeleteStackTemplateCredentialMutation(tenantID, stackTemplate.id);

  return (
    <div className="grid min-w-0 grid-cols-1 content-start gap-6" data-testid="template-credentials-tab">
      <CredentialsPanel
        title="Template credentials"
        subtitle="Overrides the stack environment for this template only."
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

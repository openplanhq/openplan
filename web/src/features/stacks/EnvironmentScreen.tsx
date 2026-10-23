import { RefreshCw } from "lucide-react";
import { useParams } from "react-router-dom";
import {
  useCreateStackCredentialMutation,
  useDeleteStackCredentialMutation,
  useStackCredentialsQuery
} from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import EnvironmentTabs from "./EnvironmentTabs";
import RequestIdNote from "../../shared/RequestIdNote";
import { Button } from "@/components/ui/button";

export default function EnvironmentScreen() {
  const tenantID = useTenantID();
  const { stackId = "" } = useParams<{ stackId: string }>();
  const credentialsQuery = useStackCredentialsQuery(tenantID, stackId);
  const createMutation = useCreateStackCredentialMutation(tenantID, stackId);
  const deleteMutation = useDeleteStackCredentialMutation(tenantID, stackId);
  const boundary = useQueryErrorBoundary(credentialsQuery.error);

  if (credentialsQuery.status === "pending") {
    return (
      <section data-testid="environment-loading">
        <p className="text-muted-foreground">Loading environment…</p>
      </section>
    );
  }

  if (credentialsQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <section className="grid justify-items-start gap-4" data-testid="environment-error">
        <p className="text-muted-foreground">Something went wrong while loading the environment.</p>
        <RequestIdNote error={credentialsQuery.error} />
        <Button className="pointer-coarse:h-11" data-testid="environment-retry" onClick={() => credentialsQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  return (
    <section data-testid="environment-screen">
      <EnvironmentTabs
        secrets={{
          note: "Write-only: a value can't be read back once added. Set as environment variables when Terraform runs. Use TF_VAR_NAME for Terraform variables; provider credentials keep their provider-specific names.",
          emptyTitle: "No secrets in this environment",
          emptyDescription: "Secrets added here are available to every template in this stack."
        }}
        variables={{
          note: "Values are shown here, so keep credentials in Secrets. Set as environment variables when Terraform runs. Use TF_VAR_NAME for Terraform variables.",
          emptyTitle: "No variables in this environment",
          emptyDescription: "Variables added here are available to every template in this stack."
        }}
        credentials={credentialsQuery.data ?? []}
        loading={credentialsQuery.isPending}
        busy={createMutation.isPending || deleteMutation.isPending}
        onCreate={async (credential) => {
          await createMutation.mutateAsync(credential);
        }}
        onDelete={(id) => deleteMutation.mutateAsync(id)}
      />
    </section>
  );
}

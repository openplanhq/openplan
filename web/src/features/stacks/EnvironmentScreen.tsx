import { RefreshCw } from "lucide-react";
import { useParams } from "react-router-dom";
import {
  useCreateStackCredentialMutation,
  useDeleteStackCredentialMutation,
  useStackCredentialsQuery
} from "../../api/queries";
import { tenantID } from "../../config";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import CredentialsPanel from "./CredentialsPanel";
import { Button } from "@/components/ui/button";

export default function EnvironmentScreen() {
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
        <Button className="pointer-coarse:h-11" data-testid="environment-retry" onClick={() => credentialsQuery.refetch()}>
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </Button>
      </section>
    );
  }

  return (
    <section data-testid="environment-screen">
      <CredentialsPanel
        title="Environment credentials"
        credentials={credentialsQuery.data ?? []}
        loading={credentialsQuery.isPending}
        busy={createMutation.isPending || deleteMutation.isPending}
        onCreate={async (name, value) => {
          await createMutation.mutateAsync({ name, value });
        }}
        onDelete={(id) => deleteMutation.mutateAsync(id)}
      />
    </section>
  );
}

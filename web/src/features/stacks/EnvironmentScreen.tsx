import { LoaderCircle, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { useParams } from "react-router-dom";
import {
  useCreateStackCredentialMutation,
  useDeleteStackCredentialMutation,
  useStackCredentialsQuery,
  useStackQuery
} from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import EnvironmentTabs from "./EnvironmentTabs";
import RequestIdNote from "../../shared/RequestIdNote";
import { cn } from "@/lib/utils";

// A stack's Environment page, on openplan UI: its header and trail on the
// canvas, and the Secrets and Variables tabs in one card, centred at the
// width the stack's Access page uses, since the two sit side by side in the
// stack's header (the owner's pick on issue 524).
export default function EnvironmentScreen() {
  const tenantID = useTenantID();
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? stackId;
  const credentialsQuery = useStackCredentialsQuery(tenantID, stackId);
  const createMutation = useCreateStackCredentialMutation(tenantID, stackId);
  const deleteMutation = useDeleteStackCredentialMutation(tenantID, stackId);
  const boundary = useQueryErrorBoundary(credentialsQuery.error);

  if (credentialsQuery.status === "error" && boundary !== null) {
    return <>{boundary}</>;
  }

  let content: ReactNode;
  if (credentialsQuery.status === "pending") {
    content = (
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="environment-loading">
        <LoaderCircle aria-hidden="true" strokeWidth={2.25} className="size-3.5 animate-spin" />
        Loading environment…
      </p>
    );
  } else if (credentialsQuery.status === "error") {
    // A list that never arrived is not an empty one, so the tabs and their
    // form wait for it rather than invite a second copy of an entry.
    content = (
      <div className="flex flex-col items-start gap-3" data-testid="environment-error">
        <ErrorLine live={false}>Something went wrong while loading the environment.</ErrorLine>
        <RequestIdNote error={credentialsQuery.error} />
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="environment-retry"
          onClick={() => void credentialsQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  } else {
    content = (
      <EnvironmentTabs
        secrets={{
          note: "Set as environment variables when Terraform runs. Write-only: a value can't be read back once added.",
          emptyTitle: "No secrets in this environment",
          emptyDescription: "Secrets added here are available to every template in this stack."
        }}
        variables={{
          note: "Set as environment variables when Terraform runs. Values are shown here, so keep credentials in Secrets.",
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
    );
  }

  return (
    <section className="mx-auto max-w-260" data-testid="environment-screen">
      <PageHeader
        title="Environment"
        trail={[
          { label: "Stacks", to: "/stacks" },
          { label: stackName, to: `/stacks/${stackId}` }
        ]}
      />
      <div className="rounded-panel border bg-card px-7 pt-6 pb-7" data-testid="environment-card">
        {content}
      </div>
    </section>
  );
}

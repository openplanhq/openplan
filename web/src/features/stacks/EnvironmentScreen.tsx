import type { ReactNode } from "react";
import { useParams } from "react-router-dom";
import { useCredentials, useStackQuery } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import EnvironmentTabs from "./EnvironmentTabs";
import LoadError from "../../shared/LoadError";
import LoadingLine from "../../shared/LoadingLine";

// A stack's Environment page, on openplan UI: its header and trail on the
// canvas, and the Secrets and Variables tabs in one card, centred at the
// width the stack's Access page uses, since the two sit side by side in the
// stack's header (the owner's pick on issue 524).
export default function EnvironmentScreen() {
  const tenantID = useTenantID();
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? stackId;
  const { list, create, remove } = useCredentials(tenantID, { scope: "stacks", id: stackId });
  const boundary = useQueryErrorBoundary(list.error);
  // A list that never arrived is not an empty one, so the tabs and their form
  // wait for it rather than invite a second copy of an entry. A failed refetch
  // keeps the list it had.
  const failed = list.status === "error" && list.data === undefined;

  if (failed && boundary !== null) {
    return <>{boundary}</>;
  }

  let content: ReactNode;
  if (list.status === "pending") {
    content = (
      <LoadingLine testId="environment-loading">Loading environment…</LoadingLine>
    );
  } else if (failed) {
    content = (
      <LoadError error={list.error} onRetry={() => void list.refetch()} testId="environment-error" retryTestId="environment-retry">
        Something went wrong while loading the environment.
      </LoadError>
    );
  } else {
    content = (
      <EnvironmentTabs
        secrets={{
          emptyTitle: "No secrets in this environment",
          emptyDescription: "Secrets added here are available to every template in this stack."
        }}
        variables={{
          emptyTitle: "No variables in this environment",
          emptyDescription: "Variables added here are available to every template in this stack."
        }}
        credentials={list.data ?? []}
        loading={list.isPending}
        busy={create.isPending || remove.isPending}
        onCreate={async (credential) => {
          await create.mutateAsync(credential);
        }}
        onDelete={(id) => remove.mutateAsync(id)}
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

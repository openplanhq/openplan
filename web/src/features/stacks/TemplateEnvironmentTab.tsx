import { useCredentials } from "../../api/queries";
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
  const { list, create, remove } = useCredentials(tenantID, { scope: "stack-templates", id: stackTemplate.id });
  const boundary = useQueryErrorBoundary(list.error);

  // A list that never arrived is not an empty one: "No overrides for this
  // template" would tell someone its runs use the stack environment when they
  // may not, and invite a second copy of a variable already there. So the
  // form waits for the list too. A failed refetch keeps the list it had.
  if (list.status === "error" && list.data === undefined) {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <LoadError error={list.error} onRetry={() => void list.refetch()}>
        Something went wrong while loading this template's environment.
      </LoadError>
    );
  }

  return (
    <div className="flex min-w-0 flex-col">
      <EnvironmentTabs
        secrets={{
          emptyTitle: "No secret overrides for this template",
          emptyDescription: "Its runs use the stack's secrets."
        }}
        variables={{
          emptyTitle: "No variable overrides for this template",
          emptyDescription: "Its runs use the stack's variables."
        }}
        credentials={list.data ?? []}
        loading={list.isPending}
        busy={create.isPending || remove.isPending}
        onCreate={async (credential) => {
          await create.mutateAsync(credential);
        }}
        onDelete={(id) => remove.mutateAsync(id)}
      />
    </div>
  );
}

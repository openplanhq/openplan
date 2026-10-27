import { useQueryClient } from "@tanstack/react-query";
import { useParams } from "react-router-dom";
import {
  useAssignStackRoleMutation,
  useRevokeStackRoleMutation,
  useSearchUsersQuery,
  useStackGrantsQuery,
  useStackQuery
} from "../../api/queries";
import { queryKeys } from "../../api/queryKeys";
import { useAuth } from "../../auth/AuthContext";
import { useTenantID } from "../../auth/useTenantID";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import AccessList from "../access/AccessList";
import { STACK_ROLES } from "../access/roles";

// A stack's Access page, on openplan UI: who holds which of the stack's roles,
// and the ways to change that. The list and its parts are AccessList, which
// the platform's Access screen shares; this page gives it the stack's roles,
// words and API calls.
export default function StackAccessScreen() {
  const tenantID = useTenantID();
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? stackId;
  const grants = useStackGrantsQuery(tenantID, stackId);
  const assign = useAssignStackRoleMutation(tenantID, stackId);
  const revoke = useRevokeStackRoleMutation(tenantID, stackId);
  const me = useAuth().me;
  const queryClient = useQueryClient();
  const boundary = useQueryErrorBoundary(grants.error);
  const useSearch = (query: string) => useSearchUsersQuery(tenantID, stackId, query);

  // Your own grant changed: re-read the stack, so if you can no longer
  // manage its access the page turns into its refusal, as any page does.
  const onOwnAccessChanged = () => void queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackId) });

  if (boundary !== null) return <>{boundary}</>;

  return (
    <section className="mx-auto max-w-260" data-testid="stack-access-screen">
      <PageHeader
        title="Access"
        trail={[
          { label: "Stacks", to: "/stacks" },
          { label: stackName, to: `/stacks/${stackId}` }
        ]}
      />
      <AccessList
        roles={STACK_ROLES}
        copy={{
          access: `access to ${stackName}`,
          where: `on ${stackName}`,
          listLabel: `People with access to ${stackName}`,
          loadError: `Something went wrong while loading who has access to ${stackName}.`,
          emptyTitle: `No one has access to ${stackName} yet`
        }}
        grants={grants.data}
        status={grants.status}
        error={grants.error}
        onRetry={() => void grants.refetch()}
        meSub={me?.sub}
        // The server refuses to remove or demote a stack's last owner.
        lockLastTop
        useSearch={useSearch}
        assign={(userSub, role) => assign.mutateAsync({ userSub, role })}
        revoke={(userSub) => revoke.mutateAsync(userSub)}
        onOwnAccessChanged={onOwnAccessChanged}
      />
    </section>
  );
}

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { getStack } from "../../api/client";
import {
  useAssignStackRoleMutation,
  useRevokeStackRoleMutation,
  useSearchUsersQuery,
  useStackGrantsQuery,
  useStackQuery
} from "../../api/queries";
import { queryKeys } from "../../api/queryKeys";
import { useAuth } from "../../auth/AuthContext";
import { tenantID } from "../../config";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import AccessList from "../access/AccessList";
import { STACK_ROLES } from "../access/roles";

/** The line /stacks shows after you've changed your own access to a stack. */
export interface LeftStackNotice {
  notice: string;
}

// A stack's Access page, on openplan UI: who holds which of the stack's roles,
// and the ways to change that. The list and its parts are AccessList, which
// the platform's Access screen shares; this page gives it the stack's roles,
// words and API calls.
export default function StackAccessScreen() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackName = useStackQuery(tenantID, stackId).data?.stack.name ?? stackId;
  const grants = useStackGrantsQuery(tenantID, stackId);
  const assign = useAssignStackRoleMutation(tenantID, stackId);
  const revoke = useRevokeStackRoleMutation(tenantID, stackId);
  const me = useAuth().me;
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const boundary = useQueryErrorBoundary(grants.error);
  const useSearch = (query: string) => useSearchUsersQuery(tenantID, stackId, query);

  // After you remove or demote yourself, ask the server what you can still
  // do here: a platform admin keeps managing the stack and stays; anyone else
  // goes to /stacks, told why, before this page turns into a refusal.
  const onOwnAccessChanged = useCallback(
    async (kind: "removed" | "demoted") => {
      let canManage = false;
      try {
        canManage = (await getStack(tenantID, stackId)).stack.effective_capabilities.can_manage_access;
      } catch {
        // The stack is gone from view: no access is left at all.
      }
      if (canManage) return true;
      const notice: LeftStackNotice = {
        notice: kind === "removed" ? `You no longer have access to ${stackName}.` : `You no longer manage access to ${stackName}.`
      };
      navigate("/stacks", { state: notice });
      void queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackId) });
      return false;
    },
    [navigate, queryClient, stackId, stackName]
  );

  if (boundary !== null) return <>{boundary}</>;

  return (
    <section className="mx-auto max-w-260" data-testid="stack-access-screen">
      <PageHeader
        title="Access"
        count={grants.data?.length}
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
          restorers: "another owner or a platform admin",
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

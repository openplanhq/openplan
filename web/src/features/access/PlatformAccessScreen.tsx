import {
  useAssignPlatformRoleMutation,
  usePlatformGrantsQuery,
  useRevokePlatformRoleMutation,
  useSearchUsersQuery
} from "../../api/queries";
import { useAuth } from "../../auth/AuthContext";
import { useTenantID } from "../../auth/useTenantID";
import PageHeader from "../../shared/PageHeader";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import AccessList from "./AccessList";
import RootRow from "./RootRow";
import { PLATFORM_ROLES } from "./roles";

// /access: who holds a platform role (admin, editor, viewer), on the same
// AccessList as a stack's Access page, with root locked above them. Behind
// can_manage_platform_access; a role change re-reads /v1/me (the mutations
// do), so losing your own admin turns this page into its refusal.
export default function PlatformAccessScreen() {
  const tenantID = useTenantID();
  const grants = usePlatformGrantsQuery(tenantID);
  const assign = useAssignPlatformRoleMutation(tenantID);
  const revoke = useRevokePlatformRoleMutation(tenantID);
  const me = useAuth().me;
  const boundary = useQueryErrorBoundary(grants.error);
  // No stack: the platform search, over everyone who has signed in.
  const useSearch = (query: string) => useSearchUsersQuery(tenantID, "", query);

  if (boundary !== null) return <>{boundary}</>;

  // Root holds no grant, so the API never lists it: you are root when you
  // may manage platform access and the loaded list doesn't name you.
  const meIsRoot = me != null && grants.data !== undefined && !grants.data.some((grant) => grant.user_sub === me.sub);

  return (
    <section className="mx-auto max-w-260" data-testid="platform-access-screen">
      {/* Root counts: it is a row on the list. */}
      <PageHeader title="Access" count={grants.data && grants.data.length + 1} />
      <AccessList
        roles={PLATFORM_ROLES}
        copy={{
          access: "platform access",
          where: "on the platform",
          listLabel: "People with platform access",
          loadError: "Something went wrong while loading who has platform access.",
          emptyTitle: "No one has a platform role yet"
        }}
        grants={grants.data}
        status={grants.status}
        error={grants.error}
        onRetry={() => void grants.refetch()}
        meSub={me?.sub}
        // Revoking the last admin is allowed: root always remains.
        lockLastTop={false}
        useSearch={useSearch}
        assign={(userSub, role) => assign.mutateAsync({ userSub, role })}
        revoke={(userSub) => revoke.mutateAsync(userSub)}
        lead={<RootRow me={meIsRoot && me ? me : undefined} />}
      />
    </section>
  );
}

import { Outlet, useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import Breadcrumb from "../../shared/Breadcrumb";

// /stacks/:stackId/environment keeps its own page until it is redesigned: the
// shared breadcrumb back to the stack, then the screen.
export default function StackSectionLayout() {
  const tenantID = useTenantID();
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stack = useStackQuery(tenantID, stackId).data?.stack;

  return (
    <section data-testid="stack-section-layout">
      <Breadcrumb
        items={[
          { label: "Stacks", to: "/stacks" },
          { label: stack?.name ?? stackId, to: `/stacks/${stackId}` },
          { label: "Environment" }
        ]}
      />
      <Outlet />
    </section>
  );
}

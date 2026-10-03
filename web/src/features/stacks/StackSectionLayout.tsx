import { matchPath, Outlet, useLocation, useParams } from "react-router-dom";
import { useStackQuery } from "../../api/queries";
import { tenantID } from "../../config";
import Breadcrumb from "../../shared/Breadcrumb";

// /stacks/:stackId/environment and /access keep their own pages until they
// are redesigned: the shared breadcrumb back to the stack, then the screen.
export default function StackSectionLayout() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stack = useStackQuery(tenantID, stackId).data?.stack;
  const section = matchPath("/stacks/:stackId/access", useLocation().pathname) ? "Access" : "Environment";

  return (
    <section data-testid="stack-section-layout">
      <Breadcrumb
        items={[
          { label: "Stacks", to: "/stacks" },
          { label: stack?.name ?? stackId, to: `/stacks/${stackId}` },
          { label: section }
        ]}
      />
      <Outlet />
    </section>
  );
}

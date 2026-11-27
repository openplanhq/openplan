import { KeyRound, Users } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import { cn } from "@/lib/utils";

/** A stack header's buttons: its section links and the preview's Open stack. */
export const stackHeaderLinkClass = cn(buttonClass("outline", "lg"), "pointer-coarse:h-11");

// A stack's Environment and Access, for the people who manage its access, in
// the stack page's header and in the stacks index's preview, so the preview
// and the page it leads to agree. What follows them (the preview's Open stack)
// shares their row. The row holds its buttons: a long stack name wraps instead.
export default function StackSectionLinks({ stackId, testId, children }: { stackId: string; testId?: string; children?: ReactNode }) {
  const row = (content: ReactNode) => <div className="flex shrink-0 flex-wrap items-center gap-2" data-testid={testId}>{content}</div>;
  const gate = (content: ReactNode) => <RequireCapability capability="can_manage_access" stackId={stackId}>{content}</RequireCapability>;
  const links = (
    <>
      <Link to={`/stacks/${stackId}/environment`} className={stackHeaderLinkClass}>
        <KeyRound data-icon="inline-start" aria-hidden="true" />
        Environment
      </Link>
      <Link to={`/stacks/${stackId}/access`} className={stackHeaderLinkClass}>
        <Users data-icon="inline-start" aria-hidden="true" />
        Access
      </Link>
    </>
  );
  // Alone, the row is gated with them, so a header without them has no
  // empty row taking its gap.
  return children ? row(<>{gate(links)}{children}</>) : gate(row(links));
}

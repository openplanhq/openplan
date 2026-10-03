import { ArrowLeft, KeyRound, Users } from "lucide-react";
import { Link, Outlet, useMatch, useParams } from "react-router-dom";
import { useAttentionQuery, useStackQuery } from "../../api/queries";
import RequireCapability from "../../auth/RequireCapability";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import PageHeader from "../../shared/PageHeader";
import { cn } from "@/lib/utils";
import { attentionByStackTemplate } from "./attention";
import StackMeta from "./StackMeta";
import StackTemplateList from "./StackTemplateList";
import { defaultStackTemplate } from "./templateSelection";

const sectionLinkClass = cn(buttonClass("outline", "lg"), "pointer-coarse:h-11");

// /stacks/:stackId: the stack's one page. Its templates on the left; on the
// right, the selected template's panel, which is the route below rendered
// into <Outlet />. With no template in the URL, the index draws the default
// one (see defaultStackTemplate) without changing the URL.
//
// On a phone the page shows the list or the panel, never both: the list on
// the stack's own path, the panel on a template's, with a link back. Each
// column hides below md on the paths where it gives way; nothing measures
// the screen.
//
// The canView guard above this route has loaded the stack.
export default function StackPage() {
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackView = useStackQuery(tenantID, stackId).data;
  const attention = attentionByStackTemplate((useAttentionQuery(tenantID).data ?? []).filter((item) => item.stack.id === stackId));
  const templateMatch = useMatch("/stacks/:stackId/templates/:stackTemplateId/*");

  if (!stackView) {
    return null;
  }

  const { stack, templates } = stackView;
  const onPanel = templateMatch !== null;
  const adding = templateMatch?.params.stackTemplateId === "new";
  const selectedId = onPanel ? (adding ? null : templateMatch.params.stackTemplateId ?? null) : defaultStackTemplate(templates)?.id ?? null;

  return (
    <section data-testid="stack-page">
      <PageHeader
        title={stack.name}
        trail={[{ label: "Stacks", to: "/stacks" }]}
        action={
          <RequireCapability capability="canManageAccess" stackId={stackId}>
            <div className="flex flex-wrap items-center gap-2">
              <Link to={`/stacks/${stackId}/environment`} className={sectionLinkClass}>
                <KeyRound data-icon="inline-start" aria-hidden="true" />
                Environment
              </Link>
              <Link to={`/stacks/${stackId}/access`} className={sectionLinkClass}>
                <Users data-icon="inline-start" aria-hidden="true" />
                Access
              </Link>
            </div>
          </RequireCapability>
        }
      >
        <StackMeta stack={stack} />
      </PageHeader>
      <div className="flex flex-col overflow-clip rounded-panel border bg-card md:flex-row">
        <div
          className={cn("min-w-0 flex-col md:flex md:w-90 md:shrink-0 md:border-r", onPanel ? "hidden" : "flex")}
          data-testid="stack-list-column"
        >
          <StackTemplateList stackId={stackId} templates={templates} selectedId={selectedId} adding={adding} attention={attention} />
        </div>
        <div className={cn("min-w-0 flex-1 flex-col md:flex", onPanel ? "flex" : "hidden")} data-testid="stack-panel-column">
          {onPanel && (
            <Link
              to={`/stacks/${stackId}`}
              className="flex min-h-11 items-center gap-1.5 self-start px-7 pt-4 text-meta font-medium text-primary hover:underline md:hidden"
            >
              <ArrowLeft aria-hidden="true" className="size-3.5" />
              Templates
            </Link>
          )}
          <Outlet />
        </div>
      </div>
    </section>
  );
}

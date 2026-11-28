import { useState } from "react";
import { Outlet, useMatch, useParams } from "react-router-dom";
import { useAttentionQuery, useStackQuery } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import PageHeader from "../../shared/PageHeader";
import StackSectionLinks from "./StackSectionLinks";
import { attentionByStackTemplate } from "./attention";
import StackMeta from "./StackMeta";
import type { IndexOutletContext } from "../../shared/indexOutlet";
import StackTemplateList from "./StackTemplateList";
import { defaultStackTemplate } from "./templateSelection";

// /stacks/:stackId: the stack's one page. Its templates on the left; on the
// right, the selected template's panel, which is the route below rendered
// into <Outlet />. With no template in the URL, the index draws the default
// one (see defaultStackTemplate) without changing the URL.
//
// The default is picked once, when the stack's own path opens, and kept while
// the person stays on it: a plan approved underneath must not swap the panel
// to another template mid-read. It gives way only if its template goes, and
// is picked afresh on coming back to the path.
//
// The canView guard above this route has loaded the stack.
// TemplatesPage mirrors this list-and-panel frame.
export default function StackPage() {
  const tenantID = useTenantID();
  const { stackId = "" } = useParams<{ stackId: string }>();
  const stackView = useStackQuery(tenantID, stackId).data;
  const attention = attentionByStackTemplate((useAttentionQuery(tenantID).data ?? []).filter((item) => item.stack.id === stackId));
  const templateMatch = useMatch("/stacks/:stackId/templates/:stackTemplateId/*");
  const onPanel = templateMatch !== null;
  const templates = stackView?.templates ?? [];

  const [pinnedId, setPinnedId] = useState<string | null>(null);
  const pinnedStays = pinnedId !== null && templates.some((stackTemplate) => stackTemplate.id === pinnedId);
  const indexTemplateId = onPanel ? null : pinnedStays ? pinnedId : defaultStackTemplate(templates)?.id ?? null;
  if (indexTemplateId !== pinnedId) {
    setPinnedId(indexTemplateId);
  }

  if (!stackView) {
    return null;
  }

  const { stack } = stackView;
  const adding = templateMatch?.params.stackTemplateId === "new";
  const selectedId = onPanel ? (adding ? null : templateMatch.params.stackTemplateId ?? null) : indexTemplateId;
  const outletContext: IndexOutletContext = { indexTemplateId };

  return (
    <section data-testid="stack-page">
      <PageHeader
        title={stack.name}
        trail={[{ label: "Stacks", to: "/stacks" }]}
        action={<StackSectionLinks stackId={stackId} />}
      >
        <StackMeta stack={stack} />
      </PageHeader>
      <div className="flex overflow-clip rounded-panel border bg-card">
        <div className="flex w-90 min-w-0 shrink-0 flex-col border-r">
          <StackTemplateList stackId={stackId} templates={templates} selectedId={selectedId} adding={adding} attention={attention} />
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <Outlet context={outletContext} />
        </div>
      </div>
    </section>
  );
}

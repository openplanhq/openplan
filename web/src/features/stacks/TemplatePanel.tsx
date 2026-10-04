import { useState } from "react";
import type { ReactNode } from "react";
import { Check } from "lucide-react";
import { Outlet, useParams } from "react-router-dom";
import { useAttentionQuery, useStackQuery } from "../../api/queries";
import type { StackTemplate } from "../../api/types";
import { tenantID } from "../../config";
import StatusLabel from "../../shared/StatusLabel";
import { useRefreshStackOnRunChange } from "../runs/useRefreshStackOnRunChange";
import { attentionByStackTemplate } from "./attention";
import { StackTemplateContext } from "./stackTemplateContext";
import StackTemplateStatusLabel, { stackTemplateActivity } from "./StackTemplateStatusLabel";
import { findSelectedStackTemplate, stackTemplateLabel } from "./stackWorkflow";
import TemplateTabs from "./TemplateTabs";
import { cn } from "@/lib/utils";

// The right side of the stack's page: one template, its state and its tabs.
// It is the template's own page, inside the stack's. The route below it draws
// the tab into <Outlet />, except on the stack's index, which passes the
// default template's Runs tab as children.
//
// The stack's route guard has already loaded the stack, so the template is
// read from cache. The content is keyed on the template, so a tab's local
// state, such as unsaved variable edits, never carries over to another.
export default function TemplatePanel({ stackTemplateId, children }: { stackTemplateId?: string; children?: ReactNode }) {
  const params = useParams<{ stackId: string; stackTemplateId: string }>();
  const stackId = params.stackId ?? "";
  const id = stackTemplateId ?? params.stackTemplateId ?? "";
  const templates = useStackQuery(tenantID, stackId).data?.templates ?? [];
  const found = findSelectedStackTemplate(templates, id);
  // A destroy that finishes takes its template out of the stack, while
  // whoever approved it may still be reading its run. A template this panel
  // has shown and then lost was destroyed: the panel keeps it, says so, and
  // drops the tabs, whose sections are gone with it.
  const [lastSeen, setLastSeen] = useState<StackTemplate | null>(null);
  if (found && found !== lastSeen) {
    setLastSeen(found);
  }
  const destroyed = !found && lastSeen?.id === id;
  const stackTemplate = found ?? (destroyed ? lastSeen : null);
  // The attention list dates a failed destroy, which the template cannot.
  const attention = attentionByStackTemplate(useAttentionQuery(tenantID).data ?? []).get(id);
  useRefreshStackOnRunChange(stackId, stackTemplate?.id ?? "");

  if (!stackTemplate) {
    return (
      <p className="px-7 py-6 text-meta text-muted-foreground" data-testid="stack-template-missing">
        That template is not installed on this stack.
      </p>
    );
  }

  const activity = stackTemplateActivity(stackTemplate, attention);
  return (
    <StackTemplateContext.Provider value={{ stackId, stackTemplate }}>
      <div className="flex min-w-0 flex-col" data-testid="template-panel">
        <div className={cn("flex flex-col gap-5 border-b border-divider px-7 pt-6", destroyed && "pb-6")}>
          <div className="flex min-w-0 flex-col gap-2">
            <h2 className="font-heading text-panel-title font-semibold tracking-title wrap-anywhere">{stackTemplateLabel(stackTemplate)}</h2>
            <div className="flex flex-wrap items-center gap-2 text-meta text-muted-foreground">
              {destroyed ? (
                <StatusLabel icon={Check} tone="settled">
                  destroyed
                </StatusLabel>
              ) : (
                <StackTemplateStatusLabel stackTemplate={stackTemplate} attention={attention} />
              )}
              <span aria-hidden="true" className="text-separator">
                ·
              </span>
              <span className="font-mono text-xs">{stackTemplate.source_ref}</span>
              {activity && !destroyed && (
                <>
                  <span aria-hidden="true" className="text-separator">
                    ·
                  </span>
                  <span>{activity}</span>
                </>
              )}
            </div>
          </div>
          {!destroyed && <TemplateTabs stackId={stackId} stackTemplateId={stackTemplate.id} />}
        </div>
        <div key={stackTemplate.id} className="flex min-w-0 flex-col gap-5 px-7 pt-5 pb-7">
          {children ?? <Outlet />}
        </div>
      </div>
    </StackTemplateContext.Provider>
  );
}

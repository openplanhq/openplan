import { useLocation } from "react-router-dom";
import { useStackCapabilities } from "../../auth/useStackCapabilities";
import UnderlineTabs from "../../shared/UnderlineTabs";
import { stackTemplatePath, templateTabOf } from "./templateSelection";
import type { TemplateTab } from "./templateSelection";

// A template's tabs on the stack's page, one per route below the template.
// The route decides the current tab, so a run keeps Runs lit and Change
// revision keeps Settings lit. Credentials shows only to people who may
// manage access, the capability its route requires; while the stack's
// capabilities load, it stays hidden, as RequireCapability would keep it.
export default function TemplateTabs({ stackId, stackTemplateId }: { stackId: string; stackTemplateId: string }) {
  const current = templateTabOf(useLocation().pathname);
  const canManageAccess = useStackCapabilities(stackId)?.canManageAccess === true;
  const tabs: [TemplateTab, string][] = [
    ["runs", "Runs"],
    ["variables", "Variables"],
    ...(canManageAccess ? ([["credentials", "Credentials"]] as [TemplateTab, string][]) : []),
    ["settings", "Settings"]
  ];

  return (
    <UnderlineTabs
      label="Template sections"
      tabs={tabs.map(([value, label]) => ({ to: stackTemplatePath(stackId, stackTemplateId, value), label, current: value === current }))}
    />
  );
}

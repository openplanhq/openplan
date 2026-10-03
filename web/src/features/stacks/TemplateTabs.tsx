import { Link, useLocation } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { cn } from "@/lib/utils";
import { stackTemplatePath, templateTabOf } from "./templateSelection";
import type { TemplateTab } from "./templateSelection";

// openplan UI's tabs as underline links, one per route below the template.
// The route decides the current tab, so a run keeps Runs lit and Change
// revision keeps Settings lit. Credentials shows only to people who may
// manage access, the capability its route requires.
export default function TemplateTabs({ stackId, stackTemplateId }: { stackId: string; stackTemplateId: string }) {
  const current = templateTabOf(useLocation().pathname);

  const tab = (value: TemplateTab, label: string) => (
    <Link
      to={stackTemplatePath(stackId, stackTemplateId, value)}
      aria-current={value === current ? "page" : undefined}
      className={cn(
        "-mb-px inline-flex h-10 items-center border-b-2 text-sm font-medium transition-colors focus-visible:-outline-offset-2 pointer-coarse:h-11",
        value === current ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
      )}
    >
      {label}
    </Link>
  );

  return (
    <nav aria-label="Template sections" className="flex flex-wrap items-center gap-x-6">
      {tab("runs", "Runs")}
      {tab("variables", "Variables")}
      <RequireCapability capability="canManageAccess" stackId={stackId}>
        {tab("credentials", "Credentials")}
      </RequireCapability>
      {tab("settings", "Settings")}
    </nav>
  );
}

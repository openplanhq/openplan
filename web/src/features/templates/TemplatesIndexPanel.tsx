import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { cn } from "@/lib/utils";
import TemplatePanel from "./TemplatePanel";
import { useIndexTemplateId } from "./templatesPageOutlet";
import VariablesTab from "./VariablesTab";

// /templates itself: the first template's panel on its Variables tab, drawn
// here rather than redirected to, so the URL stays /templates and a phone can
// show the list at it. With no templates, openplan UI's EmptyState says what
// to do, or who does it.
export default function TemplatesIndexPanel() {
  const selectedId = useIndexTemplateId();

  if (!selectedId) {
    return (
      <div className="p-7">
        <Empty className="gap-3 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid="templates-empty">
          <EmptyHeader className="gap-3">
            <h2 className="text-sm font-medium">No templates yet</h2>
            <RequireCapability
              capability="canPublishTemplate"
              fallback={<EmptyDescription className="text-meta">Templates appear here once someone who can publish registers one.</EmptyDescription>}
            >
              <EmptyDescription className="text-meta">Register a Terraform module from a Git repository to make it available to your stacks.</EmptyDescription>
            </RequireCapability>
          </EmptyHeader>
          <RequireCapability capability="canPublishTemplate">
            <Link to="/templates/new" className={cn(buttonClass("primary"), "pointer-coarse:h-11")}>
              <Plus data-icon="inline-start" aria-hidden="true" />
              Register template
            </Link>
          </RequireCapability>
        </Empty>
      </div>
    );
  }

  return (
    <TemplatePanel sourceTemplateId={selectedId}>
      <VariablesTab />
    </TemplatePanel>
  );
}

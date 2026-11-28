import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import { buttonClass } from "../../shared/buttonClass";
import { useAuth } from "../../auth/AuthContext";
import EmptyState from "../../shared/EmptyState";
import { cn } from "@/lib/utils";
import TemplatePanel from "./TemplatePanel";
import { useIndexTemplateId } from "../../shared/indexOutlet";
import VariablesTab from "./VariablesTab";

// /templates itself: the first template's panel on its Variables tab, drawn
// here rather than redirected to, so the URL stays /templates and a phone can
// show the list at it. With no templates, openplan UI's EmptyState says what
// to do, or who does it.
export default function TemplatesIndexPanel() {
  const selectedId = useIndexTemplateId();
  const canPublish = useAuth().me?.global_capabilities.can_manage_templates === true;

  if (!selectedId) {
    return (
      <div className="p-7">
        <EmptyState
          title="No templates yet"
          titleAs="h2"
          description={
            canPublish
              ? "Register a Terraform module from a Git repository to make it available to your stacks."
              : "Templates appear here once someone who can publish registers one."
          }
          action={
            canPublish && (
              <Link to="/templates/new" className={cn(buttonClass("primary"), "pointer-coarse:h-11")}>
                <Plus data-icon="inline-start" aria-hidden="true" />
                Register template
              </Link>
            )
          }
        />
      </div>
    );
  }

  return (
    <TemplatePanel templateId={selectedId}>
      <VariablesTab />
    </TemplatePanel>
  );
}

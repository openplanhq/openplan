import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import EmptyState from "../../shared/EmptyState";
import { cn } from "@/lib/utils";

// openplan UI's EmptyState inside a panel, for a stack with no templates: what
// is missing, what to do, and Add template for someone who may.
export default function NoTemplatesState({ stackId, heading }: { stackId: string; heading: "h2" | "h4" }) {
  return (
    <EmptyState
      title="No templates in this stack yet"
      titleAs={heading}
      description="Add a template to plan and apply its infrastructure here."
      action={
        <RequireCapability capability="can_operate" stackId={stackId}>
          <Link to={`/stacks/${stackId}/templates/new`} className={cn(buttonClass("primary"), "pointer-coarse:h-11")}>
            <Plus data-icon="inline-start" aria-hidden="true" />
            Add template
          </Link>
        </RequireCapability>
      }
    />
  );
}

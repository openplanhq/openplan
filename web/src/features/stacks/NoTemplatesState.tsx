import { Plus } from "lucide-react";
import { Link } from "react-router-dom";
import RequireCapability from "../../auth/RequireCapability";
import { buttonClass } from "../../shared/buttonClass";
import { Empty, EmptyDescription, EmptyHeader } from "@/components/ui/empty";
import { cn } from "@/lib/utils";

// openplan UI's EmptyState inside a panel, for a stack with no templates: what
// is missing, what to do, and Add template for someone who may.
export default function NoTemplatesState({ stackId, heading: Heading, testId }: { stackId: string; heading: "h2" | "h4"; testId?: string }) {
  return (
    <Empty className="gap-3 rounded-lg border border-dashed border-dashed-border px-5 py-10" data-testid={testId}>
      <EmptyHeader className="gap-3">
        <Heading className="text-sm font-medium">No templates in this stack yet</Heading>
        <EmptyDescription className="text-meta">Add a template to plan and apply its infrastructure here.</EmptyDescription>
      </EmptyHeader>
      <RequireCapability capability="canOperate" stackId={stackId}>
        <Link to={`/stacks/${stackId}/templates/new`} className={cn(buttonClass("primary"), "pointer-coarse:h-11")}>
          <Plus data-icon="inline-start" aria-hidden="true" />
          Add template
        </Link>
      </RequireCapability>
    </Empty>
  );
}

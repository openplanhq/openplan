import { Hourglass, Plus, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import type { AttentionItem, StackTemplate } from "../../api/types";
import RequireCapability from "../../auth/RequireCapability";
import Chip from "../../shared/Chip";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import { cn } from "@/lib/utils";
import { failedOperation, runFailedChipLabel } from "./attention";
import { stackTemplateActivity } from "./StackTemplateStatusLabel";
import { stackTemplatePath, templateTabOf } from "./templateSelection";
import { stackTemplateLabel } from "./stackWorkflow";

// The left side of the stack's page: its templates in the API's order,
// narrowed as the person types, each a link to the same tab of that template.
// Add template ends the list for people who may add one.
//
// Rows push a history entry, unlike /stacks: a selection here is a working
// page, and on a phone its own screen, so Back returns to the list.
export default function StackTemplateList({
  stackId,
  templates,
  selectedId,
  adding,
  attention
}: {
  stackId: string;
  templates: StackTemplate[];
  /** The template the panel shows, if any. */
  selectedId: string | null;
  /** On templates/new, where Add template is the current row. */
  adding: boolean;
  /** Each template's attention item, which dates a failed destroy. */
  attention: Map<string, AttentionItem>;
}) {
  const [filter, setFilter] = useState("");
  const tab = templateTabOf(useLocation().pathname);
  const query = filter.trim().toLowerCase();
  const visible = query === "" ? templates : templates.filter((stackTemplate) => stackTemplateLabel(stackTemplate).toLowerCase().includes(query));

  return (
    <div className="flex min-w-0 flex-col">
      <div className="border-b border-divider p-3">
        <SearchField label="Filter templates" value={filter} onChange={setFilter} testId="stack-templates-filter" />
      </div>
      <div className="flex flex-col gap-0.5 p-2">
        {templates.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="stack-templates-none">
            No templates in this stack yet.
          </p>
        ) : visible.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="stack-templates-filter-empty">
            No templates match this filter.
          </p>
        ) : (
          <ul className="flex flex-col gap-0.5" aria-label="Templates">
            {visible.map((stackTemplate) => (
              <li key={stackTemplate.id}>
                <StackTemplateRow
                  stackTemplate={stackTemplate}
                  to={stackTemplatePath(stackId, stackTemplate.id, tab)}
                  selected={!adding && stackTemplate.id === selectedId}
                  attention={attention.get(stackTemplate.id)}
                />
              </li>
            ))}
          </ul>
        )}
        <RequireCapability capability="can_operate" stackId={stackId}>
          <Link
            to={`/stacks/${stackId}/templates/new`}
            aria-current={adding ? "true" : undefined}
            className={cn(listItemClass(adding), "min-h-11 justify-start gap-2 text-label", adding ? "text-primary-strong" : "text-primary")}
            data-testid="add-stack-template-link"
          >
            <Plus aria-hidden="true" className="size-4 shrink-0" />
            {adding ? "New template" : "Add template"}
          </Link>
        </RequireCapability>
      </div>
    </div>
  );
}

// One template: its name, then its ref and what last happened, and a chip
// only when something on it needs a person.
function StackTemplateRow({
  stackTemplate,
  to,
  selected,
  attention
}: {
  stackTemplate: StackTemplate;
  to: string;
  selected: boolean;
  attention: AttentionItem | undefined;
}) {
  const label = stackTemplateLabel(stackTemplate);
  const activity = stackTemplateActivity(stackTemplate, attention);
  const waiting = stackTemplate.pending_plan_run_id !== "";
  const failed = stackTemplate.lifecycle === "failed";
  const runFailed = attention?.kind === "run_failed" ? failedOperation(attention) : null;

  return (
    <Link to={to} aria-current={selected ? "true" : undefined} className={listItemClass(selected)} data-testid={`stack-template-link-${stackTemplate.id}`}>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate font-mono text-meta leading-label font-medium", selected && "text-primary-strong")} title={label}>
          {label}
        </span>
        {/* A branch can be a long ref. It gives way, cut with its full text in
            a title, so what last happened (a failed destroy's time, say)
            stays readable and nothing runs under the chips. */}
        <span className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <span className="min-w-0 truncate font-mono" title={stackTemplate.source_ref}>
            {stackTemplate.source_ref}
          </span>
          {activity && (
            <>
              <span aria-hidden="true" className="shrink-0 text-separator">
                ·
              </span>
              <span className="shrink-0">{activity}</span>
            </>
          )}
        </span>
      </span>
      {(waiting || failed || runFailed) && (
        <span className="flex shrink-0 flex-col items-end gap-1">
          {waiting && (
            <Chip tone="warning" icon={Hourglass}>
              plan to approve
            </Chip>
          )}
          {failed && (
            <Chip tone="destructive" icon={TriangleAlert}>
              destroy failed
            </Chip>
          )}
          {runFailed && (
            <Chip tone="destructive" icon={TriangleAlert}>
              {runFailedChipLabel(runFailed, 1)}
            </Chip>
          )}
        </span>
      )}
    </Link>
  );
}

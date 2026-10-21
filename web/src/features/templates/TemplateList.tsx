import { useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import { cn } from "@/lib/utils";
import { templatePath, templateTabOf } from "./templateRoutes";
import { matchesTemplateFilter } from "./templateWorkflow";
import type { SourceTemplateGroup } from "./templateWorkflow";

// The left side of /templates: every template by name, narrowed as the person
// types. A row links to the same tab of its template, and pushes a history
// entry: a template is a working page, and on a phone its own screen, so Back
// returns to the list.
export default function TemplateList({ templates, selectedId }: { templates: SourceTemplateGroup[]; selectedId: string | null }) {
  const [filter, setFilter] = useState("");
  const tab = templateTabOf(useLocation().pathname);
  const visible = templates.filter((template) => matchesTemplateFilter(template, filter));

  return (
    <div className="flex min-w-0 flex-col">
      <div className="border-b border-divider p-3">
        <SearchField label="Filter templates" value={filter} onChange={setFilter} testId="templates-filter" />
      </div>
      <div className="flex flex-col p-2">
        {templates.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="templates-none">
            No templates yet.
          </p>
        ) : visible.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="templates-filter-empty">
            No templates match this filter.
          </p>
        ) : (
          <ul className="flex flex-col gap-0.5" aria-label="Templates">
            {visible.map((template) => (
              <li key={template.sourceTemplateID}>
                <TemplateRow template={template} to={templatePath(template.sourceTemplateID, tab)} selected={template.sourceTemplateID === selectedId} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

// One template: its name, then the repository it comes from and the ref it
// tracks. Each part is cut with an ellipsis rather than broken, the ref at
// most half the line so the repository stays readable, and both are whole in
// the title.
function TemplateRow({ template, to, selected }: { template: SourceTemplateGroup; to: string; selected: boolean }) {
  const latest = template.latestRevision;
  const repository = `${latest.repo_owner}/${latest.repo_name}`;

  return (
    <Link to={to} aria-current={selected ? "true" : undefined} className={listItemClass(selected)} data-testid={`template-link-${template.sourceTemplateID}`}>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate text-sm leading-label font-medium", selected && "text-primary-strong")} title={template.name}>
          {template.name}
        </span>
        <span className="flex min-w-0 gap-1.5 font-mono text-xs text-muted-foreground" title={`${repository} · ${template.sourceRef}`}>
          <span className="min-w-0 truncate">{repository}</span>
          <span aria-hidden="true">·</span>
          <span className="max-w-1/2 shrink-0 truncate">{template.sourceRef}</span>
        </span>
      </span>
    </Link>
  );
}

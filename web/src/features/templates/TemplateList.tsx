import { useId, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import { cn } from "@/lib/utils";
import { templatePath, templateTabOf } from "./templateRoutes";
import { matchesTemplateFilter, templateRootPathLabel } from "./templateWorkflow";
import type { SourceTemplateGroup, TemplateRepositoryGroup } from "./templateWorkflow";

// The left side of /templates: every template under the repository it comes
// from, narrowed as the person types. A row links to the same tab of its
// template, and pushes a history entry: a template is a working page, and on
// a phone its own screen, so Back returns to the list.
export default function TemplateList({ groups, selectedId }: { groups: TemplateRepositoryGroup[]; selectedId: string | null }) {
  const [filter, setFilter] = useState("");
  const headingId = useId();
  const tab = templateTabOf(useLocation().pathname);
  const visible = groups
    .map((group) => ({ ...group, sourceTemplates: group.sourceTemplates.filter((template) => matchesTemplateFilter(template, filter)) }))
    .filter((group) => group.sourceTemplates.length > 0);

  return (
    <div className="flex min-w-0 flex-col">
      <div className="border-b border-divider p-3">
        <SearchField label="Filter templates" value={filter} onChange={setFilter} testId="templates-filter" />
      </div>
      <div className="flex flex-col gap-0.5 p-2">
        {groups.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="templates-none">
            No templates yet.
          </p>
        ) : visible.length === 0 ? (
          <p className="p-3 text-meta text-muted-foreground" data-testid="templates-filter-empty">
            No templates match this filter.
          </p>
        ) : (
          visible.map((group, index) => (
            <section
              key={group.key}
              aria-labelledby={`${headingId}-${index}`}
              className="flex flex-col gap-0.5"
              data-testid={`template-group-${group.key}`}
            >
              <h2 id={`${headingId}-${index}`} className="px-3 pt-2.5 pb-1 font-mono text-xs font-normal text-muted-foreground wrap-anywhere">
                {group.key}
              </h2>
              <ul className="flex flex-col gap-0.5">
                {group.sourceTemplates.map((template) => (
                  <li key={template.sourceTemplateID}>
                    <TemplateRow
                      template={template}
                      to={templatePath(template.sourceTemplateID, tab)}
                      selected={template.sourceTemplateID === selectedId}
                    />
                  </li>
                ))}
              </ul>
            </section>
          ))
        )}
      </div>
    </div>
  );
}

// One template: its name, then where it lives in the repository and the ref
// it tracks. The path is left out for a module at the root, or when it is
// already the name.
function TemplateRow({ template, to, selected }: { template: SourceTemplateGroup; to: string; selected: boolean }) {
  const path = templateRootPathLabel(template.rootPath, template.name);
  const where = path === "" ? template.sourceRef : `${path} · ${template.sourceRef}`;

  return (
    <Link to={to} aria-current={selected ? "true" : undefined} className={listItemClass(selected)} data-testid={`template-link-${template.sourceTemplateID}`}>
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate text-sm leading-label font-medium", selected && "text-primary-strong")} title={template.name}>
          {template.name}
        </span>
        <span className="truncate font-mono text-xs text-muted-foreground" title={where}>
          {where}
        </span>
      </span>
    </Link>
  );
}

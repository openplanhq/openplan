import { useState } from "react";
import { Minus, TriangleAlert } from "lucide-react";
import { Link, useLocation } from "react-router-dom";
import Chip from "../../shared/Chip";
import { listItemClass } from "../../shared/listItemClass";
import SearchField from "../../shared/SearchField";
import StatusLabel from "../../shared/StatusLabel";
import { cn } from "@/lib/utils";
import { templatePath, templateTabOf } from "./templateRoutes";
import type { Template, TemplateLatestSync } from "../../api/types";
import { matchesTemplateFilter, templateDisplayName } from "./templateWorkflow";

// The left side of /templates: every template by name, narrowed as the person
// types. A row links to the same tab of its template, and pushes a history
// entry: a template is a working page, and on a phone its own screen, so Back
// returns to the list.
export default function TemplateList({ templates, selectedId }: { templates: Template[]; selectedId: string | null }) {
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
              <li key={template.id}>
                <TemplateRow template={template} to={templatePath(template.id, tab)} selected={template.id === selectedId} />
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
//
// A template whose latest sync failed (invalid counts: the panel words it
// the same) or stalled carries a Chip at the end of its name's line, where
// "no revision" sits, so every mark in the list lines up and cuts the name at
// one place, and the repository line keeps its full width. Like a stack's
// template whose destroy failed, it waits on a platform engineer, who should
// spot it without opening every template. The words are the panel's headline.
// Syncing gets no mark: it needs no one and settles by itself, and with the
// list polling while it runs, a mark would come and go under the reader.
//
// A template no sync has recorded a revision for says so at the end of its
// name's line, "no revision" in the idle tone, as a template never applied
// does on a stack: it exists, but has nothing to install. When its sync
// failed or stalled, the Chip says it alone: that is why there is no
// revision, and two marks would tell one story twice. On the selected row
// the label takes the muted colour, since the idle one on primary-soft falls
// under AA.
function TemplateRow({ template, to, selected }: { template: Template; to: string; selected: boolean }) {
  const repository = `${template.repo_owner}/${template.repo_name}`;
  const name = templateDisplayName(template);
  const syncTrouble = syncTroubleOf(template.latest_sync);
  const unsynced = template.latest_template_revision_id === "" && syncTrouble === null;

  return (
    <Link to={to} aria-current={selected ? "true" : undefined} className={listItemClass(selected)} data-testid={`template-link-${template.id}`}>
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="flex h-5 min-w-0 items-baseline justify-between gap-3">
          <span className={cn("truncate text-label", selected && "text-primary-strong")} title={name}>
            {name}
          </span>
          {unsynced && (
            <StatusLabel icon={Minus} tone="idle" className={cn("shrink-0", selected && "text-muted-foreground")} data-testid={`template-no-revision-${template.id}`}>
              no revision
            </StatusLabel>
          )}
          {syncTrouble && (
            <span className="flex shrink-0 self-center" data-testid={`template-sync-trouble-${template.id}`}>
              <Chip tone="destructive" icon={TriangleAlert}>
                {syncTrouble}
              </Chip>
            </span>
          )}
        </span>
        <span className="flex min-w-0 gap-1.5 font-mono text-caption text-muted-foreground" title={`${repository} · ${template.source_ref}`}>
          <span className="min-w-0 truncate">{repository}</span>
          <span aria-hidden="true">·</span>
          <span className="max-w-1/2 shrink-0 truncate">{template.source_ref}</span>
        </span>
      </span>
    </Link>
  );
}

// The words for a latest sync that waits on a person, or null.
function syncTroubleOf(sync: TemplateLatestSync | null): string | null {
  switch (sync?.status) {
    case "failed":
    case "invalid":
      return "sync failed";
    case "stalled":
      return "sync stalled";
    default:
      return null;
  }
}

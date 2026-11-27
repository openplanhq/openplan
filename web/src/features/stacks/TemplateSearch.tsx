import { useId, useState } from "react";
import { Link } from "react-router-dom";
import StatusLabel from "../../shared/StatusLabel";
import { revisionIndicator } from "../templates/revisionIndicator";
import type { Template, TemplateRevision } from "../../api/types";
import { activeRevisions, searchTemplates, templateDisplayName, templateRootPathLabel } from "../templates/templateWorkflow";
import type { TemplateSearchMatch } from "../templates/templateWorkflow";
import Separator from "../../shared/Separator";
import { matchCountLabel } from "../../shared/matchCountLabel";
import { pluralCount } from "../../shared/plural";
import SearchPicker, { ChoiceRow, Highlighted } from "../../shared/SearchPicker";
import { cn } from "@/lib/utils";

// The most choices the list shows at once. The registry can hold hundreds of
// templates; past a handful, typing finds one faster than scrolling does.
const MAX_CHOICES = 6;

/**
 * Add template's search: a SearchField that opens a list of ChoiceRows, the
 * best few matches for what is typed. It only finds a template; the screen
 * decides what a pick means. `onDismiss` is called when the person leaves the
 * list without picking (Escape, or focus or a press elsewhere).
 */
export default function TemplateSearch({
  templates,
  repositoryCount,
  autoFocus = false,
  onPick,
  onDismiss
}: {
  templates: Template[];
  repositoryCount: number;
  /** Focused and open on mount: the search reopened by Change. */
  autoFocus?: boolean;
  onPick: (template: Template) => void;
  onDismiss?: (reason: "escape" | "elsewhere") => void;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(autoFocus);
  const hintId = useId();

  const matches = searchTemplates(templates, query);
  const shown = matches.slice(0, MAX_CHOICES);

  return (
    <div className="flex flex-col gap-1.5">
      <SearchPicker<TemplateSearchMatch>
        items={shown}
        query={query}
        onQueryChange={setQuery}
        onPick={(match) => onPick(match.template)}
        open={open}
        onOpenChange={(next, { reason }) => {
          setOpen(next);
          if (next) return;
          if (reason === "escape-key") onDismiss?.("escape");
          else if (reason === "focus-out" || reason === "outside-press") onDismiss?.("elsewhere");
        }}
        itemToStringLabel={(match) => match.name}
        isItemEqualToValue={(a, b) => a.template.id === b.template.id}
        input={{
          "aria-label": "Search templates",
          "aria-describedby": hintId,
          placeholder: "Search templates by name, repository or path",
          autoFocus,
          onFocus: () => setOpen(true),
          // Leaving a closed list fires no close, so say so here. An open
          // list reports its own close, and acting here could shut it under
          // a press on one of its choices.
          onBlur: () => !open && onDismiss?.("elsewhere")
        }}
        emptyClassName="gap-2"
        empty={
          <>
            <span>No templates match this search.</span>
            <Link to="/templates/new" className="font-medium text-primary hover:underline" data-testid="add-template-search-register">
              Register template
            </Link>
          </>
        }
        count={countLabel(matches.length, shown.length, query.trim() !== "")}
      >
        {(match) => (
          // Its revisions are read only once it is picked, so it has no
          // revision count or state of its own here.
          <ChoiceRow
            key={match.template.id}
            value={match}
            name={<Highlighted text={match.name} hits={match.nameHits} markClassName="font-semibold" />}
            caption={<TemplateSource template={match.template} repositoryHits={match.repositoryHits} pathHits={match.pathHits} className="truncate" />}
          />
        )}
      </SearchPicker>
      <p id={hintId} className="text-caption text-muted-foreground">
        {pluralCount(templates.length, "template", "templates")} in{" "}
        {pluralCount(repositoryCount, "repository", "repositories")}.
      </p>
    </div>
  );
}

/** Where a template lives, under its name: its repository, root path and ref,
    with the letters a search matched marked. */
export function TemplateSource({
  template,
  repositoryHits = [],
  pathHits = [],
  className
}: {
  template: Template;
  repositoryHits?: number[];
  pathHits?: number[];
  className?: string;
}) {
  const { repo_owner, repo_name, root_path, source_ref } = template;
  // The root path adds nothing at the repository root, or when it is the name.
  const showPath = templateRootPathLabel(root_path, templateDisplayName(template)) !== "";
  return (
    <span className={cn("font-mono text-caption text-muted-foreground", className)}>
      <Highlighted text={`${repo_owner}/${repo_name}`} hits={repositoryHits} markClassName="font-medium" />
      {showPath && (
        <>
          <Separator spaced />
          <Highlighted text={root_path} hits={pathHits} markClassName="font-medium" />
        </>
      )}
      {source_ref && (
        <>
          <Separator spaced />
          {source_ref}
        </>
      )}
    </span>
  );
}

/** The picked ChoiceRow's right side, from the template's revisions, newest
    first: a StatusLabel when the newest is not active, and how many can be
    installed. */
export function TemplateChoiceState({ revisions }: { revisions: TemplateRevision[] }) {
  const latestState = revisions.length > 0 ? revisionIndicator(revisions[0].status) : null;
  const installableCount = activeRevisions(revisions).length;
  return (
    <span className="flex shrink-0 flex-wrap items-center justify-end gap-x-3 gap-y-1">
      {latestState && <StatusLabel indicator={latestState} />}
      {installableCount > 0 && <span className="text-meta text-muted-foreground">{pluralCount(installableCount, "revision", "revisions")}</span>}
    </span>
  );
}

function countLabel(total: number, shown: number, searching: boolean): string {
  if (!searching) {
    if (total > shown) return `Showing ${shown} of ${total} templates · type to narrow`;
    return pluralCount(total, "template", "templates");
  }
  return matchCountLabel(total, shown);
}

import { Fragment, useId, useState } from "react";
import { Combobox } from "@base-ui/react";
import { Search } from "lucide-react";
import { Link } from "react-router-dom";
import { searchFieldClass } from "../../shared/SearchField";
import StatusLabel from "../../shared/StatusLabel";
import { revisionIndicator } from "../templates/revisionIndicator";
import { activeRevisions, revisionCountLabel, searchTemplates, templateRootPathLabel } from "../templates/templateWorkflow";
import type { SourceTemplateGroup, TemplateSearchMatch } from "../templates/templateWorkflow";
import { ComboboxContent, ComboboxEmpty, ComboboxList, useComboboxAnchor } from "@/components/ui/combobox";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
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
  sourceTemplates,
  repositoryCount,
  autoFocus = false,
  onPick,
  onDismiss
}: {
  sourceTemplates: SourceTemplateGroup[];
  repositoryCount: number;
  /** Focused and open on mount: the search reopened by Change. */
  autoFocus?: boolean;
  onPick: (sourceTemplate: SourceTemplateGroup) => void;
  onDismiss?: (reason: "escape" | "elsewhere") => void;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(autoFocus);
  const hintId = useId();
  // The whole field, glass and all, so the list lines up with its edges.
  const anchorRef = useComboboxAnchor();

  const matches = searchTemplates(sourceTemplates, query);
  const shown = matches.slice(0, MAX_CHOICES);

  return (
    <div className="flex flex-col gap-1.5">
      {/* The list answers what is typed as searchTemplates ranks it
          (filter={null}), and the pick is never the Combobox's value: a pick
          closes the search, and the screen shows the picked template instead. */}
      <Combobox.Root<TemplateSearchMatch>
        items={shown}
        filter={null}
        value={null}
        onValueChange={(match) => {
          if (match) onPick(match.sourceTemplate);
        }}
        inputValue={query}
        onInputValueChange={(value, { reason }) => {
          // A pick writes the template's name into the input; only typing is a query.
          if (reason === "input-change") setQuery(value);
        }}
        open={open}
        onOpenChange={(next, { reason }) => {
          setOpen(next);
          if (next) return;
          if (reason === "escape-key") onDismiss?.("escape");
          else if (reason === "focus-out" || reason === "outside-press") onDismiss?.("elsewhere");
        }}
        autoHighlight
        itemToStringLabel={(match) => match.sourceTemplate.name}
        isItemEqualToValue={(a, b) => a.sourceTemplate.sourceTemplateID === b.sourceTemplate.sourceTemplateID}
      >
        <InputGroup ref={anchorRef} className={cn(searchFieldClass, "pointer-coarse:h-11")}>
          <InputGroupAddon className="pl-2.5">
            <Search aria-hidden="true" className="text-subtle-foreground" />
          </InputGroupAddon>
          <Combobox.Input
            render={<InputGroupInput />}
            aria-label="Search templates"
            aria-describedby={hintId}
            placeholder="Search templates by name, repository or path"
            autoFocus={autoFocus}
            onFocus={() => setOpen(true)}
            // Leaving a closed list fires no close, so say so here. An open
            // list reports its own close, and acting here could shut it under
            // a press on one of its choices.
            onBlur={() => {
              if (!open) onDismiss?.("elsewhere");
            }}
            className="placeholder:text-subtle-foreground"
          />
        </InputGroup>
        {/* The vendored popup and list, as wide as the field and drawn like a
            ChoiceRow group: a border, no shadow, choices a divider apart. */}
        <ComboboxContent anchor={anchorRef} className="flex min-w-0 flex-col border bg-card shadow-none ring-0">
          <ComboboxEmpty className="flex-col items-start justify-start gap-2 px-4 py-3 text-left text-meta">
            <span>No templates match this search.</span>
            <Link to="/templates/new" className="font-medium text-primary hover:underline" data-testid="add-template-search-register">
              Register template
            </Link>
          </ComboboxEmpty>
          <ComboboxList className="max-h-96 min-h-0 divide-y divide-divider p-0">
            {(match: TemplateSearchMatch) => <Choice key={match.sourceTemplate.sourceTemplateID} match={match} />}
          </ComboboxList>
          {shown.length > 0 && (
            <div className="flex items-center justify-between gap-4 border-t border-divider bg-canvas px-4 py-2 text-xs text-muted-foreground">
              <span>{countLabel(matches.length, shown.length, query.trim() !== "")}</span>
              <span aria-hidden="true" className="whitespace-nowrap pointer-coarse:hidden">
                ↑ ↓ to move · Enter to choose · Esc to close
              </span>
            </div>
          )}
        </ComboboxContent>
      </Combobox.Root>
      <p id={hintId} className="text-xs text-muted-foreground">
        {sourceTemplates.length === 1 ? "1 template" : `${sourceTemplates.length} templates`} in{" "}
        {repositoryCount === 1 ? "1 repository" : `${repositoryCount} repositories`}.
      </p>
    </div>
  );
}

// One match as a ChoiceRow. A template with no active revision cannot be
// picked, and its StatusLabel says why.
function Choice({ match }: { match: TemplateSearchMatch }) {
  const { sourceTemplate } = match;
  const installable = activeRevisions(sourceTemplate.revisions).length > 0;
  return (
    <Combobox.Item
      value={match}
      disabled={!installable}
      className="flex min-h-13 w-full cursor-pointer items-center justify-between gap-4 px-4 py-2 text-left outline-none select-none data-disabled:cursor-not-allowed data-highlighted:bg-primary-tint pointer-coarse:min-h-14"
    >
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate text-sm leading-label font-medium", installable ? "text-foreground" : "text-muted-foreground")}>
          <Highlighted text={sourceTemplate.name} hits={match.nameHits} markClassName="font-semibold" />
        </span>
        <TemplateSource sourceTemplate={sourceTemplate} repositoryHits={match.repositoryHits} pathHits={match.pathHits} className="truncate" />
      </span>
      <TemplateChoiceState sourceTemplate={sourceTemplate} />
    </Combobox.Item>
  );
}

/** Where a template lives, under its name: its repository, root path and ref,
    with the letters a search matched marked. */
export function TemplateSource({
  sourceTemplate,
  repositoryHits = [],
  pathHits = [],
  className
}: {
  sourceTemplate: SourceTemplateGroup;
  repositoryHits?: number[];
  pathHits?: number[];
  className?: string;
}) {
  const { repo_owner, repo_name } = sourceTemplate.latestRevision;
  // The root path adds nothing at the repository root, or when it is the name.
  const showPath = templateRootPathLabel(sourceTemplate.rootPath, sourceTemplate.name) !== "";
  return (
    <span className={cn("font-mono text-xs text-muted-foreground", className)}>
      <Highlighted text={`${repo_owner}/${repo_name}`} hits={repositoryHits} markClassName="font-medium" />
      {showPath && (
        <>
          <Separator />
          <Highlighted text={sourceTemplate.rootPath} hits={pathHits} markClassName="font-medium" />
        </>
      )}
      {sourceTemplate.sourceRef && (
        <>
          <Separator />
          {sourceTemplate.sourceRef}
        </>
      )}
    </span>
  );
}

/** A ChoiceRow's right side: a StatusLabel when the newest revision is not
    active, and how many revisions can be installed. */
export function TemplateChoiceState({ sourceTemplate }: { sourceTemplate: SourceTemplateGroup }) {
  const latestState = revisionIndicator(sourceTemplate.latestRevision.status);
  const installableCount = activeRevisions(sourceTemplate.revisions).length;
  return (
    <span className="flex shrink-0 flex-wrap items-center justify-end gap-x-3 gap-y-1">
      {latestState && (
        <StatusLabel icon={latestState.icon} tone={latestState.tone} strong={latestState.strong}>
          {latestState.label}
        </StatusLabel>
      )}
      {installableCount > 0 && <span className="text-meta text-muted-foreground">{revisionCountLabel(installableCount)}</span>}
    </span>
  );
}

function Separator() {
  return (
    <span aria-hidden="true" className="text-separator">
      {" · "}
    </span>
  );
}

// The matched letters drawn heavier, never in colour: primary is kept for the
// action, the link, the focus ring and the selection.
function Highlighted({ text, hits, markClassName }: { text: string; hits: number[]; markClassName: string }) {
  if (hits.length === 0) {
    return <>{text}</>;
  }
  const hit = new Set(hits);
  const runs: Array<{ text: string; hit: boolean }> = [];
  for (let index = 0; index < text.length; index++) {
    const isHit = hit.has(index);
    const last = runs[runs.length - 1];
    if (last && last.hit === isHit) {
      last.text += text[index];
    } else {
      runs.push({ text: text[index], hit: isHit });
    }
  }
  return (
    <>
      {runs.map((run, index) =>
        run.hit ? (
          <mark key={index} className={cn("bg-transparent text-foreground", markClassName)}>
            {run.text}
          </mark>
        ) : (
          <Fragment key={index}>{run.text}</Fragment>
        )
      )}
    </>
  );
}

function countLabel(total: number, shown: number, searching: boolean): string {
  if (!searching) {
    if (total > shown) return `Showing ${shown} of ${total} templates · type to narrow`;
    return total === 1 ? "1 template" : `${total} templates`;
  }
  if (total > shown) return `${shown} of ${total} matches · keep typing to narrow`;
  return total === 1 ? "1 match" : `${total} matches`;
}

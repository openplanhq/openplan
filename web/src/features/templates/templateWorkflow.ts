import type { Template, TemplateRevision } from "../../api/types";

/**
 * The templates the registry and Add template show: those a sync has produced
 * a revision for. A template whose first sync is still running, or failed, has
 * nothing to show or install yet, so it stays out of both, as it did before
 * templates were read on their own.
 */
export function templatesWithRevision(templates: Template[]): Template[] {
  return templates.filter((template) => template.latest_template_revision_id !== "");
}

/**
 * Every template in one list, by name, for the templates page. People look
 * for a template by what it is; its repository is a detail of it, so it is
 * not a heading. Ties keep the API's order, newest first.
 */
export function templatesByName(templates: Template[]): Template[] {
  return [...templates].sort((a, b) =>
    templateDisplayName(a).localeCompare(templateDisplayName(b), undefined, { sensitivity: "base", numeric: true })
  );
}

/**
 * The revisions of a template that can actually be installed.
 *
 * Order is the order received, which is the API's `created_at desc, id desc` —
 * most recently registered first. Callers rely on that (the install picker
 * offers them in this order and defaults to the first), so this filters and
 * never sorts: sorting on `created_at` alone would drop the id tiebreaker and
 * let revisions registered in the same tick shuffle between renders.
 */
export function activeRevisions(templateRevisions: TemplateRevision[]): TemplateRevision[] {
  return templateRevisions.filter((templateRevision) => templateRevision.status === "active");
}

/**
 * The revision an install should default to: the most recently registered
 * *active* one, or null when the template has none.
 *
 * Deliberately not simply the latest revision. A template whose newest revision
 * failed validation must stay installable at the last one that passed — which
 * is what the old per-revision picker achieved by accident, by disabling the
 * inactive rows and leaving the older active ones selectable.
 */
export function latestActiveRevision(templateRevisions: TemplateRevision[]): TemplateRevision | null {
  return activeRevisions(templateRevisions)[0] ?? null;
}

/**
 * Whether a template matches what someone typed in the list's filter: its
 * name, repository, root path, description or tags, ignoring case and the
 * spaces around the query. An empty query matches everything.
 */
export function matchesTemplateFilter(template: Template, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  return [templateDisplayName(template), `${template.repo_owner}/${template.repo_name}`, template.root_path, template.description, ...template.tags].some(
    (text) => text.toLowerCase().includes(needle)
  );
}

/** A template found by searchTemplates, and which characters of it matched. */
export interface TemplateSearchMatch {
  sourceTemplate: Template;
  /** What the template is called, as templateDisplayName says it. */
  name: string;
  /** "owner/name". */
  repository: string;
  /** Indices into the name, the repository and the root path, for highlighting. */
  nameHits: number[];
  repositoryHits: number[];
  pathHits: number[];
}

/**
 * The templates matching what someone typed in the Add template search, best
 * first. Every word must match the name, the repository or the root path.
 * Names match fuzzily, their letters in order; the repository and the path
 * only as a run of letters, so a short word cannot light up every long path.
 * With nothing typed, every template, by name.
 */
export function searchTemplates(sourceTemplates: Template[], query: string): TemplateSearchMatch[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const scored: Array<{ match: TemplateSearchMatch; score: number }> = [];

  for (const sourceTemplate of sourceTemplates) {
    const match: TemplateSearchMatch = {
      sourceTemplate,
      name: templateDisplayName(sourceTemplate),
      repository: `${sourceTemplate.repo_owner}/${sourceTemplate.repo_name}`,
      nameHits: [],
      repositoryHits: [],
      pathHits: []
    };
    let score = 0;
    let matchesEveryWord = true;
    for (const word of words) {
      // A word found only in the repository or the path ranks below one found
      // in the name, since the name is what people search for.
      const candidates = [
        { hits: match.nameHits, found: scoreWord(word, match.name, true), penalty: 0 },
        { hits: match.repositoryHits, found: scoreWord(word, match.repository, false), penalty: 60 },
        { hits: match.pathHits, found: scoreWord(word, sourceTemplate.root_path, false), penalty: 60 }
      ];
      let best: (typeof candidates)[number] | null = null;
      for (const candidate of candidates) {
        if (candidate.found && (!best?.found || candidate.found.score - candidate.penalty > best.found.score - best.penalty)) {
          best = candidate;
        }
      }
      if (!best?.found) {
        matchesEveryWord = false;
        break;
      }
      score += best.found.score - best.penalty;
      best.hits.push(...best.found.hits);
    }
    if (matchesEveryWord) {
      scored.push({ match, score });
    }
  }

  if (words.length === 0) {
    return scored.map(({ match }) => match).sort((a, b) => a.name.localeCompare(b.name));
  }
  // Ties go to the shorter name: "redis" before "redis-operator".
  return scored
    .sort((a, b) => b.score - a.score || a.match.name.length - b.match.name.length)
    .map(({ match }) => match);
}

// How well one word matches one string, and where; null when it does not. A
// run of letters beats letters apart, and either beats more for starting a
// word ("-", "_", "/", "." or a space before it) and for starting earlier.
function scoreWord(word: string, text: string, fuzzy: boolean): { score: number; hits: number[] } | null {
  const haystack = text.toLowerCase();
  const startsWord = (index: number) => index === 0 || "-_/. ".includes(haystack[index - 1]);

  const at = haystack.indexOf(word);
  if (at >= 0) {
    return {
      score: 100 + (startsWord(at) ? 30 : 0) - at - haystack.length * 0.5,
      hits: Array.from({ length: word.length }, (_, offset) => at + offset)
    };
  }
  if (!fuzzy) {
    return null;
  }

  const hits: number[] = [];
  let score = 0;
  for (let index = 0; index < haystack.length && hits.length < word.length; index++) {
    if (haystack[index] !== word[hits.length]) {
      continue;
    }
    const follows = hits.length > 0 && hits[hits.length - 1] === index - 1;
    score += 1 + (startsWord(index) ? 4 : 0) + (follows ? 3 : 0);
    hits.push(index);
  }
  if (hits.length < word.length) {
    return null;
  }
  // Letters spread across a long name are more likely chance than intent.
  const span = hits[hits.length - 1] - hits[0] + 1;
  if (span > Math.max(word.length * 3, word.length + 6)) {
    return null;
  }
  return { score: score - (span - word.length) * 0.5, hits };
}

/**
 * What to call a template, or a revision of one. `name` comes from a
 * revision and can be blank, so fall through to the root path and finally
 * the repository. A root path of "." names nothing and is skipped.
 */
export function templateDisplayName(named: Pick<Template, "name" | "root_path" | "repo_name">): string {
  const name = named.name.trim();
  if (name !== "") {
    return name;
  }
  const rootPath = named.root_path.trim();
  if (rootPath !== "" && rootPath !== ".") {
    return rootPath;
  }
  return named.repo_name;
}

/**
 * The root path as a subline, or "" when it would add nothing — a module at the
 * repository root, or a path already serving as the template's name.
 */
export function templateRootPathLabel(rootPath: string, name: string): string {
  const trimmed = rootPath.trim();
  if (trimmed === "" || trimmed === "." || trimmed === name) {
    return "";
  }
  return trimmed;
}

/**
 * Where a revision's code lives, as a person would name it: its root path,
 * or the repository for a template that sits at the repository's root.
 */
export function revisionSourceLabel(revision: Pick<TemplateRevision, "root_path" | "repo_owner" | "repo_name">): string {
  const rootPath = revision.root_path.trim();
  return rootPath === "" || rootPath === "." ? `${revision.repo_owner}/${revision.repo_name}` : rootPath;
}

export function revisionCountLabel(count: number): string {
  return count === 1 ? "1 revision" : `${count} revisions`;
}

export function templateRevisionLabel(templateRevision: TemplateRevision): string {
  const name = templateRevision.name.trim() || `${templateRevision.repo_owner}/${templateRevision.repo_name}`;
  const shortSHA = shortCommitSHA(templateRevision.resolved_commit_sha);
  return shortSHA ? `${name} @ ${templateRevision.source_ref} · ${shortSHA}` : `${name} @ ${templateRevision.source_ref}`;
}

export function shortCommitSHA(commitSHA: string): string {
  return commitSHA.trim().slice(0, 7);
}

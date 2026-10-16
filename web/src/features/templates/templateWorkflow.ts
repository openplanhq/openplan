import type { TemplateRevision } from "../../api/types";

/**
 * One source template: the stable identity that revisions accumulate under.
 *
 * The backend keys `source_templates` on (tenant, repo owner, repo name, root
 * path, source ref) and stamps every revision with the resulting
 * `source_template_id`, so bucketing on that one field reproduces the server's
 * identity exactly — there is no need to rebuild the tuple here.
 *
 * The consequence that shapes both screens: `sourceRef` cannot vary within a
 * group (it is part of the key), while `resolved_commit_sha` is the only thing
 * that does. So the ref belongs on the template row and the commits belong on
 * the revisions behind it.
 */
export interface SourceTemplateGroup {
  /** Also the React key and the `:sourceTemplateId` route param. */
  sourceTemplateID: string;
  name: string;
  rootPath: string;
  sourceRef: string;
  /**
   * First in API order, which is `created_at desc, id desc` — most recently
   * registered, not necessarily the newest commit. `SourceTemplate`'s own
   * `latest_template_revision_id` is not exposed to the client.
   */
  latestRevision: TemplateRevision;
  revisions: TemplateRevision[];
}

export interface TemplateRepositoryGroup {
  /** "owner/name" — stable within a tenant, so it doubles as the React key. */
  key: string;
  repoOwner: string;
  repoName: string;
  sourceTemplates: SourceTemplateGroup[];
}

/**
 * Buckets revisions into templates, then templates into the repositories they
 * came from. One repository can hold several templates — a different root path
 * or a different ref is a different template.
 *
 * Insertion order is preserved at both levels. The API returns revisions
 * newest-first, so a template sorts by its most recent revision, a repository
 * by its most recently touched template, and a freshly registered revision
 * surfaces its template at the top.
 */
export function groupTemplatesByRepository(templateRevisions: TemplateRevision[]): TemplateRepositoryGroup[] {
  const repositories = new Map<string, TemplateRepositoryGroup>();
  const sourceTemplates = new Map<string, SourceTemplateGroup>();

  for (const templateRevision of templateRevisions) {
    const sourceTemplateID = sourceTemplateKey(templateRevision);
    const existing = sourceTemplates.get(sourceTemplateID);
    if (existing) {
      existing.revisions.push(templateRevision);
      continue;
    }

    // First sighting is the newest revision, so it names the template.
    const sourceTemplate: SourceTemplateGroup = {
      sourceTemplateID,
      name: templateDisplayName(templateRevision),
      rootPath: templateRevision.root_path,
      sourceRef: templateRevision.source_ref,
      latestRevision: templateRevision,
      revisions: [templateRevision]
    };
    sourceTemplates.set(sourceTemplateID, sourceTemplate);

    const repositoryKey = `${templateRevision.repo_owner}/${templateRevision.repo_name}`;
    const repository = repositories.get(repositoryKey);
    if (repository) {
      repository.sourceTemplates.push(sourceTemplate);
      continue;
    }
    repositories.set(repositoryKey, {
      key: repositoryKey,
      repoOwner: templateRevision.repo_owner,
      repoName: templateRevision.repo_name,
      sourceTemplates: [sourceTemplate]
    });
  }

  return Array.from(repositories.values());
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
 * Every revision of one source template, newest-first. Reads from the same
 * tenant-wide list the registry screen renders — `canUpgradeStackTemplate`
 * already filters that list the same way, and no per-template endpoint exists.
 */
export function revisionsForSourceTemplate(
  templateRevisions: TemplateRevision[],
  sourceTemplateID: string
): TemplateRevision[] {
  if (sourceTemplateID === "") {
    return [];
  }
  return templateRevisions.filter((templateRevision) => sourceTemplateKey(templateRevision) === sourceTemplateID);
}

/**
 * Whether a template matches what someone typed in the list's filter: its
 * name, repository, root path, description or tags, ignoring case and the
 * spaces around the query. An empty query matches everything.
 */
export function matchesTemplateFilter(sourceTemplate: SourceTemplateGroup, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const latest = sourceTemplate.latestRevision;
  return [sourceTemplate.name, `${latest.repo_owner}/${latest.repo_name}`, latest.root_path, latest.description, ...latest.tags].some(
    (text) => text.toLowerCase().includes(needle)
  );
}

/** A template found by searchTemplates, and which characters of it matched. */
export interface TemplateSearchMatch {
  sourceTemplate: SourceTemplateGroup;
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
export function searchTemplates(sourceTemplates: SourceTemplateGroup[], query: string): TemplateSearchMatch[] {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const scored: Array<{ match: TemplateSearchMatch; score: number }> = [];

  for (const sourceTemplate of sourceTemplates) {
    const latest = sourceTemplate.latestRevision;
    const match: TemplateSearchMatch = {
      sourceTemplate,
      repository: `${latest.repo_owner}/${latest.repo_name}`,
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
        { hits: match.nameHits, found: scoreWord(word, sourceTemplate.name, true), penalty: 0 },
        { hits: match.repositoryHits, found: scoreWord(word, match.repository, false), penalty: 60 },
        { hits: match.pathHits, found: scoreWord(word, sourceTemplate.rootPath, false), penalty: 60 }
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
    return scored.map(({ match }) => match).sort((a, b) => a.sourceTemplate.name.localeCompare(b.sourceTemplate.name));
  }
  // Ties go to the shorter name: "redis" before "redis-operator".
  return scored
    .sort((a, b) => b.score - a.score || a.match.sourceTemplate.name.length - b.match.sourceTemplate.name.length)
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
 * What to call a template. `name` is inferred per revision and can be blank, so
 * fall through to the root path and finally the repository. A root path of "."
 * names nothing and is skipped.
 */
export function templateDisplayName(templateRevision: TemplateRevision): string {
  const name = templateRevision.name.trim();
  if (name !== "") {
    return name;
  }
  const rootPath = templateRevision.root_path.trim();
  if (rootPath !== "" && rootPath !== ".") {
    return rootPath;
  }
  return templateRevision.repo_name;
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

/**
 * `source_template_id` is `not null default ''`, so a row written before the
 * backfill would carry an empty id. Falling back to the identity tuple the
 * backend keys on keeps those rows apart instead of collapsing every template
 * in the tenant into one. JSON encoding the tuple keeps the parts unambiguous
 * without picking a separator a repository name or path might contain.
 */
export function sourceTemplateKey(templateRevision: TemplateRevision): string {
  if (templateRevision.source_template_id !== "") {
    return templateRevision.source_template_id;
  }
  return JSON.stringify([
    templateRevision.repo_owner,
    templateRevision.repo_name,
    templateRevision.root_path,
    templateRevision.source_ref
  ]);
}

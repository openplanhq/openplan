import type { TemplateRevision } from "../../api/types";

const GITHUB = "https://github.com";

type LinkedRevision = Pick<TemplateRevision, "repo_owner" | "repo_name" | "source_ref" | "root_path" | "resolved_commit_sha">;

/** Where a template revision lives on GitHub, which every repository is cloned from. */
export interface GitHubLinks {
  repository: string;
  /** The module's folder at the ref; the ref itself for a module at the root. */
  tree: string;
  ref: string;
  commit: string;
}

// Each segment of a ref or a path is encoded on its own, so release/1.2 keeps
// its slash while a # or a space cannot end the URL early. "." and empty
// segments name nothing: ./modules/eks/ is modules/eks.
function segments(path: string): string {
  return path
    .trim()
    .split("/")
    .filter((segment) => segment !== "" && segment !== ".")
    .map(encodeURIComponent)
    .join("/");
}

export function githubLinks(revision: LinkedRevision): GitHubLinks {
  const repository = `${GITHUB}/${encodeURIComponent(revision.repo_owner.trim())}/${encodeURIComponent(revision.repo_name.trim())}`;
  const ref = `${repository}/tree/${segments(revision.source_ref)}`;
  const path = segments(revision.root_path);
  return {
    repository,
    ref,
    tree: path === "" ? ref : `${ref}/${path}`,
    commit: `${repository}/commit/${encodeURIComponent(revision.resolved_commit_sha.trim())}`
  };
}

/** The root path as written, or null for a module at the repository root. */
export function rootPathOf(rootPath: string): string | null {
  const trimmed = rootPath.trim();
  return trimmed === "" || trimmed === "." ? null : trimmed;
}

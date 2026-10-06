import { describe, expect, it } from "vitest";
import type { TemplateRevision } from "../../api/types";
import {
  activeRevisions,
  groupTemplatesByRepository,
  latestActiveRevision,
  matchesTemplateFilter,
  revisionCountLabel,
  revisionSourceLabel,
  revisionsForSourceTemplate,
  searchTemplates,
  sourceTemplateKey,
  templateDisplayName,
  templateRevisionLabel,
  templateRootPathLabel
} from "./templateWorkflow";
import { revision } from "./testSupport";

describe("template workflow helpers", () => {
  it("formats menu labels from persisted template revision rows", () => {
    const selectedTemplateRevision = templateRevision({
      id: "template_123",
      repo_owner: "acme",
      repo_name: "infra",
      source_ref: "main",
      resolved_commit_sha: "abcdef1234567890",
      name: ""
    });

    expect(templateRevisionLabel(selectedTemplateRevision)).toBe("acme/infra @ main · abcdef1");
  });
});

describe("templateDisplayName", () => {
  it("prefers the revision's own name", () => {
    expect(templateDisplayName(templateRevision({ name: "Supabase" }))).toBe("Supabase");
  });

  it("falls back to the root path when the name is blank", () => {
    expect(templateDisplayName(templateRevision({ name: "  ", root_path: "environments/dev" }))).toBe(
      "environments/dev"
    );
  });

  it("falls back to the repository when the module sits at the root", () => {
    expect(templateDisplayName(templateRevision({ name: "", root_path: ".", repo_name: "infra" }))).toBe("infra");
  });
});

describe("templateRootPathLabel", () => {
  it("drops a path that would add nothing", () => {
    expect(templateRootPathLabel(".", "infra")).toBe("");
    expect(templateRootPathLabel("", "infra")).toBe("");
    // Already serving as the name, so repeating it below is noise.
    expect(templateRootPathLabel("environments/dev", "environments/dev")).toBe("");
  });

  it("keeps a path that locates the module", () => {
    expect(templateRootPathLabel("environments/dev", "Supabase")).toBe("environments/dev");
  });
});

describe("revisionCountLabel", () => {
  it("singularises one revision", () => {
    expect(revisionCountLabel(1)).toBe("1 revision");
    expect(revisionCountLabel(4)).toBe("4 revisions");
  });
});

describe("groupTemplatesByRepository", () => {
  it("returns no groups for an empty list", () => {
    expect(groupTemplatesByRepository([])).toEqual([]);
  });

  it("collapses every revision of one source template into a single template", () => {
    const groups = groupTemplatesByRepository([
      templateRevision({ id: "rev_2", source_template_id: "src_1", resolved_commit_sha: "bbbbbbb" }),
      templateRevision({ id: "rev_1", source_template_id: "src_1", resolved_commit_sha: "aaaaaaa" })
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].sourceTemplates).toHaveLength(1);
    expect(groups[0].sourceTemplates[0].revisions.map((revision) => revision.id)).toEqual(["rev_2", "rev_1"]);
  });

  it("keeps different refs of one repository apart, because the ref is part of the identity", () => {
    const groups = groupTemplatesByRepository([
      templateRevision({ id: "rev_1", source_template_id: "src_main", source_ref: "main" }),
      templateRevision({ id: "rev_2", source_template_id: "src_v2", source_ref: "v2" })
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].sourceTemplates.map((template) => template.sourceRef)).toEqual(["main", "v2"]);
  });

  it("separates repositories that share a name under different owners", () => {
    const groups = groupTemplatesByRepository([
      templateRevision({ id: "rev_1", source_template_id: "src_1", repo_owner: "acme", repo_name: "infra" }),
      templateRevision({ id: "rev_2", source_template_id: "src_2", repo_owner: "globex", repo_name: "infra" })
    ]);

    expect(groups.map((group) => group.key)).toEqual(["acme/infra", "globex/infra"]);
  });

  it("takes the newest revision as the template's name and latest", () => {
    // The API returns revisions created_at desc, so the first sighting is the
    // newest — and `name` is per-revision, so it can drift between them.
    const groups = groupTemplatesByRepository([
      templateRevision({ id: "rev_2", source_template_id: "src_1", name: "Renamed", resolved_commit_sha: "f17f983a" }),
      templateRevision({ id: "rev_1", source_template_id: "src_1", name: "Original", resolved_commit_sha: "c17860ca" })
    ]);

    const template = groups[0].sourceTemplates[0];
    expect(template.name).toBe("Renamed");
    expect(template.latestRevision.id).toBe("rev_2");
  });

  it("preserves the incoming newest-first order across templates and repositories", () => {
    const groups = groupTemplatesByRepository([
      templateRevision({ id: "rev_3", source_template_id: "src_a", repo_owner: "acme", repo_name: "newest" }),
      templateRevision({ id: "rev_2", source_template_id: "src_b", repo_owner: "acme", repo_name: "older" }),
      templateRevision({ id: "rev_1", source_template_id: "src_a", repo_owner: "acme", repo_name: "newest" })
    ]);

    expect(groups.map((group) => group.key)).toEqual(["acme/newest", "acme/older"]);
    expect(groups[0].sourceTemplates[0].revisions.map((revision) => revision.id)).toEqual(["rev_3", "rev_1"]);
  });

  it("keeps templates apart when the source template id is missing", () => {
    // `source_template_id` is `not null default ''`; without the tuple fallback
    // every such row would collapse into one bogus template.
    const groups = groupTemplatesByRepository([
      templateRevision({ id: "rev_1", source_template_id: "", root_path: "a" }),
      templateRevision({ id: "rev_2", source_template_id: "", root_path: "b" })
    ]);

    expect(groups[0].sourceTemplates).toHaveLength(2);
  });
});

describe("activeRevisions", () => {
  it("keeps only what can be installed, in the order received", () => {
    // The API's order is created_at desc, id desc; filtering must not disturb
    // it, because the install picker offers these in this order.
    const kept = activeRevisions([
      templateRevision({ id: "rev_3", status: "invalid" }),
      templateRevision({ id: "rev_2", status: "active" }),
      templateRevision({ id: "rev_1", status: "active" }),
      templateRevision({ id: "rev_0", status: "pending_validation" })
    ]);

    expect(kept.map((revision) => revision.id)).toEqual(["rev_2", "rev_1"]);
  });
});

describe("latestActiveRevision", () => {
  it("skips a newer revision that failed validation", () => {
    // The whole point: a template whose newest revision is broken must stay
    // installable at the last one that passed.
    const latest = latestActiveRevision([
      templateRevision({ id: "rev_bad", status: "invalid" }),
      templateRevision({ id: "rev_good", status: "active" })
    ]);

    expect(latest?.id).toBe("rev_good");
  });

  it("returns null when nothing can be installed", () => {
    expect(latestActiveRevision([templateRevision({ status: "validating" })])).toBeNull();
    expect(latestActiveRevision([])).toBeNull();
  });
});

describe("revisionsForSourceTemplate", () => {
  const revisions = [
    templateRevision({ id: "rev_3", source_template_id: "src_1" }),
    templateRevision({ id: "rev_2", source_template_id: "src_2" }),
    templateRevision({ id: "rev_1", source_template_id: "src_1" })
  ];

  it("returns only that template's revisions, in the order received", () => {
    expect(revisionsForSourceTemplate(revisions, "src_1").map((revision) => revision.id)).toEqual(["rev_3", "rev_1"]);
  });

  it("returns nothing for an unknown or empty id", () => {
    expect(revisionsForSourceTemplate(revisions, "src_missing")).toEqual([]);
    expect(revisionsForSourceTemplate(revisions, "")).toEqual([]);
  });
});

function templateRevision(overrides: Partial<TemplateRevision>): TemplateRevision {
  return {
    id: "template_123",
    tenant_id: "tenant_123",
    source_template_id: "source_template_123",
    repo_owner: "hashicorp",
    repo_name: "terraform-template",
    source_ref: "main",
    resolved_commit_sha: "abc123",
    root_path: ".",
    name: "Terraform Template",
    description: "",
    tags: [],
    status: "active",
    created_at: "2026-07-06T00:00:00Z",
    ...overrides
  };
}

describe("revisionSourceLabel", () => {
  it("names a revision by its root path", () => {
    expect(revisionSourceLabel(templateRevision({ root_path: "aws/eks", repo_owner: "acme", repo_name: "infra-modules" }))).toBe("aws/eks");
  });

  it("names the repository for a template at its root", () => {
    for (const rootPath of [".", "", "  "]) {
      expect(revisionSourceLabel(templateRevision({ root_path: rootPath, repo_owner: "acme", repo_name: "infra-modules" }))).toBe("acme/infra-modules");
    }
  });
});

describe("matchesTemplateFilter", () => {
  const [network] = groupTemplatesByRepository([
    revision({ name: "network", root_path: "aws/network", description: "A VPC with public and private subnets.", tags: ["networking"] })
  ])[0].sourceTemplates;

  it.each(["network", "acme/infra", "aws/net", "private subnets", "networking", ""])("matches %j", (query) => {
    expect(matchesTemplateFilter(network, query)).toBe(true);
  });

  it("ignores case and the spaces around what was typed", () => {
    expect(matchesTemplateFilter(network, "  NETWORKING  ")).toBe(true);
    expect(matchesTemplateFilter(network, "VPC")).toBe(true);
  });

  it("does not match what is not there", () => {
    expect(matchesTemplateFilter(network, "kafka")).toBe(false);
  });
});

describe("searchTemplates", () => {
  // One template per spec, each its own source template.
  const templates = (...specs: Array<Partial<TemplateRevision>>) =>
    groupTemplatesByRepository(specs.map((spec, index) => revision({ id: `rev_${index}`, source_template_id: `tpl_${index}`, ...spec }))).flatMap(
      (group) => group.sourceTemplates
    );
  const names = (query: string, ...specs: Array<Partial<TemplateRevision>>) =>
    searchTemplates(templates(...specs), query).map((match) => match.sourceTemplate.name);

  it("lists every template by name when nothing is typed", () => {
    expect(names("  ", { name: "redis" }, { name: "network" }, { name: "eks" })).toEqual(["eks", "network", "redis"]);
  });

  it("ranks a name that starts with the query first, then one that contains it, then one with its letters apart", () => {
    expect(
      names("redis", { name: "redshift-cluster" }, { name: "memorystore-redis" }, { name: "redis-operator" }, { name: "redis" }, { name: "network" })
    ).toEqual(["redis", "redis-operator", "memorystore-redis", "redshift-cluster"]);
  });

  it("matches a name's letters in order, and says which ones matched", () => {
    const [match] = searchTemplates(templates({ name: "rds-postgres" }), "rpg");
    expect(match.nameHits).toEqual([0, 4, 8]);
  });

  it("does not match letters scattered across a long name", () => {
    expect(names("ab", { name: "a-very-long-name-b" })).toEqual([]);
  });

  // Fuzzy matching a long repository or path would light up nearly every
  // template for a short query.
  it("matches the repository and the path only as a run of letters", () => {
    const eks = { name: "eks", repo_owner: "acme", repo_name: "infra-modules", root_path: "aws/eks" };
    const [byRepository] = searchTemplates(templates(eks), "infra");
    expect(byRepository.repositoryHits).toEqual([5, 6, 7, 8, 9]);
    expect(byRepository.nameHits).toEqual([]);
    const [byPath] = searchTemplates(templates(eks), "aws/");
    expect(byPath.pathHits).toEqual([0, 1, 2, 3]);
    expect(names("aim", eks)).toEqual([]);
  });

  it("needs every word to match somewhere", () => {
    expect(
      names("gcp redis", { name: "memorystore-redis", repo_name: "gcp-modules", root_path: "memorystore" }, { name: "redis", root_path: "aws/redis" })
    ).toEqual(["memorystore-redis"]);
  });

  it("ignores case and the spaces around what was typed", () => {
    expect(names("  REDIS ", { name: "redis" }, { name: "network" })).toEqual(["redis"]);
  });
});

describe("sourceTemplateKey", () => {
  it("is the source template id, or the identity tuple when there is none", () => {
    expect(sourceTemplateKey(revision({ source_template_id: "tpl_9" }))).toBe("tpl_9");
    expect(sourceTemplateKey(revision({ source_template_id: "", root_path: "." }))).toBe(
      JSON.stringify(["acme", "infra-modules", ".", "main"])
    );
  });
});

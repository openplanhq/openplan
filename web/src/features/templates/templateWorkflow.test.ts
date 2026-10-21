import { describe, expect, it } from "vitest";
import type { Template, TemplateRevision } from "../../api/types";
import {
  activeRevisions,
  latestActiveRevision,
  matchesTemplateFilter,
  revisionCountLabel,
  revisionSourceLabel,
  searchTemplates,
  templateDisplayName,
  templateRevisionLabel,
  templateRootPathLabel,
  templatesByName,
  templatesWithRevision
} from "./templateWorkflow";
import { template } from "./testSupport";

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

describe("templatesWithRevision", () => {
  it("leaves out a template no sync has produced a revision for", () => {
    const templates = [template({ id: "tpl_1" }), template({ id: "tpl_2", latest_template_revision_id: "" })];
    expect(templatesWithRevision(templates).map((candidate) => candidate.id)).toEqual(["tpl_1"]);
  });
});

describe("templatesByName", () => {
  it("sorts by the name shown, ignoring case and reading numbers as numbers", () => {
    const templates = [
      template({ id: "tpl_1", name: "redis-10" }),
      template({ id: "tpl_2", name: "", root_path: "aws/eks" }),
      template({ id: "tpl_3", name: "Network" }),
      template({ id: "tpl_4", name: "redis-9" })
    ];
    expect(templatesByName(templates).map((candidate) => candidate.id)).toEqual(["tpl_2", "tpl_3", "tpl_4", "tpl_1"]);
  });

  it("keeps the API's order for equal names, and leaves the list it was given alone", () => {
    const templates = [template({ id: "tpl_b", name: "eks" }), template({ id: "tpl_a", name: "eks" })];
    expect(templatesByName(templates).map((candidate) => candidate.id)).toEqual(["tpl_b", "tpl_a"]);
    expect(templates.map((candidate) => candidate.id)).toEqual(["tpl_b", "tpl_a"]);
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
  const network = template({ name: "network", root_path: "aws/network", description: "A VPC with public and private subnets.", tags: ["networking"] });

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
  const templates = (...specs: Array<Partial<Template>>) => specs.map((spec, index) => template({ id: `tpl_${index}`, ...spec }));
  const names = (query: string, ...specs: Array<Partial<Template>>) => searchTemplates(templates(...specs), query).map((match) => match.name);

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

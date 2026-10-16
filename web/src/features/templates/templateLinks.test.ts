import { describe, expect, it } from "vitest";
import { githubLinks, rootPathOf } from "./templateLinks";
import { revision } from "./testSupport";

describe("githubLinks", () => {
  it("links the repository, the module's folder at the ref, the ref and the commit", () => {
    expect(githubLinks(revision())).toEqual({
      repository: "https://github.com/acme/infra-modules",
      tree: "https://github.com/acme/infra-modules/tree/main/aws/eks",
      ref: "https://github.com/acme/infra-modules/tree/main",
      commit: "https://github.com/acme/infra-modules/commit/3f9c2a1d8e7b6a5c4d3e2f1a0b9c8d7e6f5a4b3c"
    });
  });

  it.each([".", "", "./", " . "])("points a module at the root (%j) at the ref itself", (rootPath) => {
    const links = githubLinks(revision({ root_path: rootPath }));
    expect(links.tree).toBe("https://github.com/acme/infra-modules/tree/main");
  });

  it("keeps the slashes of a ref and a path, and drops . segments", () => {
    const links = githubLinks(revision({ source_ref: "release/1.2", root_path: "./modules/eks/" }));
    expect(links.ref).toBe("https://github.com/acme/infra-modules/tree/release/1.2");
    expect(links.tree).toBe("https://github.com/acme/infra-modules/tree/release/1.2/modules/eks");
  });

  it("encodes each segment, so a # or a space cannot end the URL early", () => {
    const links = githubLinks(revision({ source_ref: "feature#1", root_path: "my module" }));
    expect(links.tree).toBe("https://github.com/acme/infra-modules/tree/feature%231/my%20module");
  });
});

describe("rootPathOf", () => {
  it("returns the path, or null for a module at the repository root", () => {
    expect(rootPathOf("aws/eks")).toBe("aws/eks");
    expect(rootPathOf(" aws/eks ")).toBe("aws/eks");
    expect(rootPathOf(".")).toBeNull();
    expect(rootPathOf("")).toBeNull();
  });
});

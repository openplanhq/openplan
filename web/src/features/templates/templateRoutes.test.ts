import { describe, expect, it } from "vitest";
import { templatePath, templateTabOf } from "./templateRoutes";

describe("templateTabOf", () => {
  it.each([
    ["/templates", "variables"],
    ["/templates/new", "variables"],
    ["/templates/tpl_1", "variables"],
    ["/templates/tpl_1/variables", "variables"],
    ["/templates/tpl_1/revisions", "revisions"]
  ])("reads %s as %s", (pathname, tab) => {
    expect(templateTabOf(pathname)).toBe(tab);
  });
});

describe("templatePath", () => {
  it("addresses a template's tab, Variables by default", () => {
    expect(templatePath("tpl_1")).toBe("/templates/tpl_1/variables");
    expect(templatePath("tpl_1", "revisions")).toBe("/templates/tpl_1/revisions");
  });

  it("encodes an id that is the identity tuple of a template with no source id", () => {
    const id = JSON.stringify(["acme", "edge", ".", "main"]);
    expect(templatePath(id)).toBe(`/templates/${encodeURIComponent(id)}/variables`);
  });
});

// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import RoleBadge from "./RoleBadge";

afterEach(cleanup);

describe("RoleBadge", () => {
  it.each([
    ["owner", "progress"],
    ["operator", "success"],
    ["approver", "warning"],
    ["viewer", "muted"]
  ])("paints %s as the %s variant", (role, variant) => {
    render(<RoleBadge stackRole={role} />);
    const badge = screen.getByText(role);
    expect(badge.getAttribute("data-variant")).toBe(variant);
    expect(badge.getAttribute("data-role")).toBe(role);
  });

  // The API types a role as a string, so a role the UI doesn't know yet still
  // renders, named and neutral. A plain lookup object would resolve
  // "constructor" to Object.prototype.constructor.
  it.each(["auditor", "constructor"])("renders the unknown role %s as muted", (role) => {
    render(<RoleBadge stackRole={role} />);
    expect(screen.getByText(role).getAttribute("data-variant")).toBe("muted");
  });
});

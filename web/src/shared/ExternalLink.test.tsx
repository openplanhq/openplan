// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import ExternalLink from "./ExternalLink";

afterEach(cleanup);

describe("ExternalLink", () => {
  it("opens a new tab without handing over the opener, and says where it goes", () => {
    render(
      <ExternalLink href="https://github.com/acme/infra-modules" site="GitHub">
        acme/infra-modules
      </ExternalLink>
    );
    const link = screen.getByRole("link", { name: "acme/infra-modules, on GitHub (opens in a new tab)" });
    expect(link.getAttribute("href")).toBe("https://github.com/acme/infra-modules");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noreferrer");
    expect(link.className).toContain("font-mono");
  });

  it("sets plain words in the sans face", () => {
    render(
      <ExternalLink href="https://github.com/acme/edge/tree/main" site="GitHub" mono={false}>
        the repository root
      </ExternalLink>
    );
    expect(screen.getByRole("link").className).not.toContain("font-mono");
  });

  it("wraps a long value only after a slash or hyphen, never cuts it, and keeps the arrow with its last piece", () => {
    const value = "acme-platform-engineering/terraform-aws-modules";
    render(
      <ExternalLink href="https://github.com/x" site="GitHub" term="Repository">
        {value}
      </ExternalLink>
    );
    // jsdom's name drops the space a browser keeps after "Repository:".
    const link = screen.getByRole("link", { name: /^Repository: ?acme-platform-engineering\/terraform-aws-modules, on GitHub \(opens in a new tab\)$/ });
    expect(link.getAttribute("title")).toBeNull();
    expect(link.querySelector(".truncate")).toBeNull();
    expect(link.querySelectorAll("wbr")).toHaveLength(5);
    const tail = link.querySelector(".whitespace-nowrap");
    expect(tail?.textContent).toBe("modules");
    expect(tail?.querySelector("svg")).not.toBeNull();
  });
});

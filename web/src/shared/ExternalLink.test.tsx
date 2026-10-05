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
});

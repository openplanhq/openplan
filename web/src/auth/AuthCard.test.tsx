// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import AuthCard, { AuthCardTitle } from "./AuthCard";

afterEach(cleanup);

describe("AuthCard", () => {
  it("is the page's main landmark, with the card inside it", () => {
    render(<AuthCard data-testid="frame-card">body</AuthCard>);
    const card = screen.getByTestId("frame-card");
    expect(card.getAttribute("data-slot")).toBe("card");
    expect(screen.getByRole("main").contains(card)).toBe(true);
  });

  // The frame paints its own background, so the threshold reads as one
  // whatever page it is reached from.
  it("sets its own background", () => {
    render(<AuthCard>body</AuthCard>);
    expect(screen.getByRole("main").classList).toContain("bg-background");
  });
});

describe("AuthCardTitle", () => {
  // Preflight leaves a heading with the body's type.
  it("is a level-one heading that sets its own type", () => {
    render(<AuthCardTitle>Sign in</AuthCardTitle>);
    const heading = screen.getByRole("heading", { level: 1, name: "Sign in" });
    for (const name of ["font-heading", "text-base", "font-medium", "tracking-normal"]) {
      expect(heading.classList).toContain(name);
    }
  });
});

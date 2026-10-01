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

  // body keeps the legacy colours until PR 8, so the frame paints its own.
  it("sets its own background and text colour", () => {
    render(<AuthCard>body</AuthCard>);
    const classes = screen.getByRole("main").classList;
    expect(classes).toContain("bg-background");
    expect(classes).toContain("text-foreground");
  });
});

describe("AuthCardTitle", () => {
  // base.css gives every h1 the legacy 40px display type until PR 8.
  it("is a level-one heading that sets its own type", () => {
    render(<AuthCardTitle>Sign in</AuthCardTitle>);
    const heading = screen.getByRole("heading", { level: 1, name: "Sign in" });
    for (const name of ["font-heading", "text-base", "font-medium", "tracking-normal"]) {
      expect(heading.classList).toContain(name);
    }
  });
});

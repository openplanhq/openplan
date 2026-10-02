// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import SignInScreen, { safeReturnTo } from "./SignInScreen";
import { clearLoginAttempts, maxLoginAttempts } from "./loginAttempts";

const attemptsKey = "openplan.auth.loginAttempts";
const assign = vi.fn();
const reload = vi.fn();

function renderSignIn(url = "/signin") {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <SignInScreen />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.stubGlobal("location", { assign, reload, pathname: "/signin", search: "" });
});

afterEach(() => {
  cleanup();
  // clearLoginAttempts, not sessionStorage.clear: the module also holds a
  // once-per-page-load latch, and leaving it set makes every later test's
  // recorded attempt a silent no-op.
  clearLoginAttempts();
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

describe("safeReturnTo", () => {
  it("keeps an in-app path", () => {
    expect(safeReturnTo("/stacks?selected=st_1")).toBe("/stacks?selected=st_1");
  });

  // Everything here would send a freshly signed-in browser somewhere it should
  // not go: the first group all leave the origin, and /v1/auth/login restarts
  // sign-in the instant it succeeds. The rows mirror the server's table in
  // authentication.TestSafeReturnTo -- the two are meant to be the same rule, and this
  // path never reaches the server to be caught there.
  it.each([
    ["absolute URL", "https://evil.test/steal"],
    ["protocol-relative", "//evil.test/steal"],
    ["backslash-relative", "/\\evil.test"],
    ["backslash-relative with a path", "/\\evil.test/steal"],
    ["a tab before the host", "/\t/evil.test"],
    ["a newline before the host", "/\n/evil.test"],
    ["a relative path", "stacks"],
    ["parent traversal", "/../../etc"],
    ["traversal into the API", "/stacks/../v1/auth/login"],
    ["the API", "/v1/auth/login"],
    ["the API root", "/v1"],
    ["nothing", null],
    ["empty", ""]
  ])("refuses %s", (_label, raw) => {
    expect(safeReturnTo(raw)).toBe("/");
  });

  // Only the /v1 segment itself is the API; a path that merely starts with
  // those characters is an app route like any other.
  it("keeps a /v1 lookalike path", () => {
    expect(safeReturnTo("/v10/stacks")).toBe("/v10/stacks");
  });
});

describe("SignInScreen", () => {
  // The identity provider is the only way in, so the screen asks for nothing:
  // no username, no password, and no request to find out which ways in exist.
  it("offers one sign-in button and no credential fields", () => {
    renderSignIn();

    expect(screen.getByRole("button", { name: /sign in/i })).toBeTruthy();
    expect(screen.queryByLabelText("Username")).toBeNull();
    expect(screen.queryByLabelText("Password")).toBeNull();
  });

  // Sign-in leaves the SPA for the provider, carrying where to land afterwards.
  // A full navigation, never fetch: the route redirects to the provider's
  // origin, where an XHR dies on CORS.
  it("starts the OIDC flow with a full navigation to the requested page", async () => {
    renderSignIn("/signin?return_to=%2Fstacks%3Fselected%3Dst_1");

    await userEvent.setup().click(screen.getByRole("button", { name: /sign in/i }));

    expect(assign).toHaveBeenCalledWith("/v1/auth/login?return_to=%2Fstacks%3Fselected%3Dst_1");
  });

  it("refuses an unsafe return_to on the way out", async () => {
    renderSignIn("/signin?return_to=%2F%2Fevil.test");

    await userEvent.setup().click(screen.getByRole("button", { name: /sign in/i }));

    expect(assign).toHaveBeenCalledWith("/v1/auth/login?return_to=%2F");
  });

  // A trip to the provider is what the loop guard counts: if the cookie does
  // not survive the round trip, this is the record that proves it.
  it("records the sign-in attempt", async () => {
    renderSignIn();

    await userEvent.setup().click(screen.getByRole("button", { name: /sign in/i }));

    expect(sessionStorage.getItem(attemptsKey)).toBe("1");
  });

  // Three completed sign-ins that did not stick is a browser refusing the
  // cookie, not a user who cannot sign in.
  it("explains a blocked cookie once the attempts are spent", () => {
    sessionStorage.setItem(attemptsKey, String(maxLoginAttempts));
    renderSignIn();

    expect(screen.getByTestId("signin-cookies-blocked")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^sign in$/i })).toBeNull();
  });

  it("clears the count and reloads when the user tries again", async () => {
    sessionStorage.setItem(attemptsKey, String(maxLoginAttempts));
    renderSignIn();

    await userEvent.setup().click(screen.getByTestId("signin-cookies-retry"));

    expect(sessionStorage.getItem(attemptsKey)).toBeNull();
    expect(reload).toHaveBeenCalled();
  });
});

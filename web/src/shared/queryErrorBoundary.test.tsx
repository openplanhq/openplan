// web/src/shared/queryErrorBoundary.test.tsx
// @vitest-environment jsdom
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { AuthContext } from "../auth/AuthContext";
import type { AuthContextValue } from "../auth/AuthContext";
import { ApiRequestError } from "../api/client";
import { classifyQueryError, useQueryErrorBoundary } from "./queryErrorBoundary";
import AccessDenied from "../app/AccessDenied";
import NotFound from "../app/NotFound";
import ServiceUnavailable from "../app/ServiceUnavailable";

function authValue(overrides: Partial<AuthContextValue> = {}): AuthContextValue {
  return {
    me: { sub: "s1", tenant_id: "tenant_123", display_name: "Test User", global_capabilities: { can_create_stack: false, can_manage_templates: false, can_manage_platform_access: false } },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides
  };
}

function wrapper(auth: AuthContextValue) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <AuthContext.Provider value={auth}>{children}</AuthContext.Provider>;
  };
}

describe("classifyQueryError", () => {
  it.each([401, 403, 404, 503] as const)("classifies a %d ApiRequestError", (status) => {
    expect(classifyQueryError(new ApiRequestError({ type: "about:blank", title: "code", status: status, detail: "message" }))).toBe(status);
  });

  it("returns null for an unhandled status", () => {
    expect(classifyQueryError(new ApiRequestError({ type: "about:blank", title: "server_error", status: 500, detail: "boom" }))).toBeNull();
  });

  it("returns null for non-ApiRequestError values", () => {
    expect(classifyQueryError(new Error("boom"))).toBeNull();
    expect(classifyQueryError(undefined)).toBeNull();
    expect(classifyQueryError(null)).toBeNull();
  });
});

describe("useQueryErrorBoundary", () => {
  it("returns null and does not call logout for a non-error", () => {
    const logout = vi.fn();
    const { result } = renderHook(() => useQueryErrorBoundary(undefined), { wrapper: wrapper(authValue({ logout })) });

    expect(result.current).toBeNull();
    expect(logout).not.toHaveBeenCalled();
  });

  it("renders AccessDenied for a 403 without calling logout", () => {
    const logout = vi.fn();
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "forbidden", status: 403, detail: "nope" })), {
      wrapper: wrapper(authValue({ logout }))
    });

    // toMatchObject (not toEqual) here: an element created during this hook's render carries a
    // non-null `_owner` fiber reference, while one created in the test body does not — toEqual
    // would fail on that internal field alone even though type/props match.
    expect(result.current).toMatchObject({ type: AccessDenied, props: {} });
    expect(logout).not.toHaveBeenCalled();
  });

  it("renders NotFound for a 404", () => {
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "not_found", status: 404, detail: "nope" })), {
      wrapper: wrapper(authValue())
    });

    expect(result.current).toMatchObject({ type: NotFound, props: {} });
  });

  it("renders ServiceUnavailable for a 503", () => {
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "unavailable", status: 503, detail: "down" })), {
      wrapper: wrapper(authValue())
    });

    expect(result.current).toMatchObject({ type: ServiceUnavailable, props: {} });
  });

  it("returns null for an unrecognized status without calling logout", () => {
    const logout = vi.fn();
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "server_error", status: 500, detail: "boom" })), {
      wrapper: wrapper(authValue({ logout }))
    });

    expect(result.current).toBeNull();
    expect(logout).not.toHaveBeenCalled();
  });

  it("triggers logout exactly once for a 401, even across re-renders with the same error", async () => {
    const logout = vi.fn();
    const { result, rerender } = renderHook(({ error }: { error: unknown }) => useQueryErrorBoundary(error), {
      initialProps: { error: new ApiRequestError({ type: "about:blank", title: "unauthorized", status: 401, detail: "expired" }) as unknown },
      wrapper: wrapper(authValue({ logout }))
    });

    await waitFor(() => expect(logout).toHaveBeenCalledTimes(1));
    expect(result.current).toBeNull();

    rerender({ error: new ApiRequestError({ type: "about:blank", title: "unauthorized", status: 401, detail: "expired" }) });
    expect(logout).toHaveBeenCalledTimes(1);
  });


});

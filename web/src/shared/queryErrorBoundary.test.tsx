// web/src/shared/queryErrorBoundary.test.tsx
// @vitest-environment jsdom
import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiRequestError } from "../api/client";
import { classifyQueryError } from "../api/problems";
import { useQueryErrorBoundary } from "./queryErrorBoundary";
import AccessDenied from "../app/AccessDenied";
import NotFound from "../app/NotFound";
import ServiceUnavailable from "../app/ServiceUnavailable";

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
  it("returns null for a non-error", () => {
    const { result } = renderHook(() => useQueryErrorBoundary(undefined));

    expect(result.current).toBeNull();
  });

  it("renders AccessDenied for a 403", () => {
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "forbidden", status: 403, detail: "nope" })));

    // toMatchObject (not toEqual) here: an element created during this hook's render carries a
    // non-null `_owner` fiber reference, while one created in the test body does not — toEqual
    // would fail on that internal field alone even though type/props match.
    expect(result.current).toMatchObject({ type: AccessDenied, props: {} });
  });

  it("renders NotFound for a 404", () => {
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "not_found", status: 404, detail: "nope" })));

    expect(result.current).toMatchObject({ type: NotFound, props: {} });
  });

  it("renders ServiceUnavailable for a 503", () => {
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "unavailable", status: 503, detail: "down" })));

    expect(result.current).toMatchObject({ type: ServiceUnavailable, props: {} });
  });

  // A 401 is fetchWithAuth's to handle: it has already navigated to sign-in.
  it.each([401, 500])("returns null for a %d", (status) => {
    const { result } = renderHook(() => useQueryErrorBoundary(new ApiRequestError({ type: "about:blank", title: "code", status, detail: "message" })));

    expect(result.current).toBeNull();
  });
});

// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AuthContext } from "./AuthContext";
import { useStackCapabilities } from "./useStackCapabilities";
import { queryKeys } from "../api/queryKeys";
import type { StackView } from "../api/types";

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <AuthContext.Provider
        value={{
          me: { sub: "s1", tenant_id: "tenant_123", display_name: "Test User", global_capabilities: { can_create_stack: false, can_manage_templates: false, can_manage_platform_access: false } },
          status: "authenticated",
          login: () => {},
          logout: () => {}
        }}
      >
        <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
      </AuthContext.Provider>
    );
  };
}

function testQueryClient(): QueryClient {
  // staleTime: Infinity keeps seeded cache data from triggering a background
  // refetch on mount (the default staleTime: 0 would refetch stale data even
  // though it's already cached, defeating the "no additional fetch" assertions).
  return new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
}

function stackView(overrides: Partial<StackView["stack"]> = {}): StackView {
  return {
    stack: {
      id: "stack_1",
      tenant_id: "tenant_123",
      name: "Stack",
      slug: "stack",
      slug_is_derived: true,
      tags: {},
      default_credential_ids: [],
      created_by: "user_123",
      created_at: "2026-07-19T00:00:00Z",
      effective_capabilities: { can_view: true, can_operate: false, can_approve: false, can_manage_access: false },
      ...overrides
    },
    templates: []
  };
}

describe("useStackCapabilities", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("returns undefined before the stack query has resolved", () => {
    const { result } = renderHook(() => useStackCapabilities("stack_1"), { wrapper: wrapper(testQueryClient()) });

    expect(result.current).toBeUndefined();
  });

  it("returns the cached effective_capabilities once the stack query resolves, without a second fetch", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(stackView()), { status: 200, headers: { "content-type": "application/json" } })
    );
    const { result } = renderHook(() => useStackCapabilities("stack_1"), { wrapper: wrapper(testQueryClient()) });

    await waitFor(() =>
      expect(result.current).toEqual({ can_view: true, can_operate: false, can_approve: false, can_manage_access: false })
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("reads an already-cached stack query without triggering a fetch", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    const queryClient = testQueryClient();
    queryClient.setQueryData(queryKeys.stack("tenant_123", "stack_1"), stackView({ can_view: true } as never));

    const { result } = renderHook(() => useStackCapabilities("stack_1"), { wrapper: wrapper(queryClient) });

    expect(result.current).toEqual({ can_view: true, can_operate: false, can_approve: false, can_manage_access: false });
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

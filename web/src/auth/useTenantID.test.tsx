// @vitest-environment jsdom
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { AuthContext } from "./AuthContext";
import type { AuthContextValue } from "./AuthContext";
import { useTenantID } from "./useTenantID";

function wrapperWith(value: AuthContextValue) {
  return ({ children }: { children: ReactNode }) => <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

const session: Omit<AuthContextValue, "me" | "status"> = { login: () => {}, logout: () => {} };

describe("useTenantID", () => {
  it("returns the tenant the API reported for the session", () => {
    const wrapper = wrapperWith({
      ...session,
      status: "authenticated",
      me: { sub: "s1", tenant_id: "default", display_name: "Test User", global_capabilities: { can_create_stack: false, can_manage_templates: false, can_manage_platform_access: false } }
    });

    const { result } = renderHook(() => useTenantID(), { wrapper });

    expect(result.current).toBe("default");
  });

  it("throws before the session has loaded", () => {
    const wrapper = wrapperWith({ ...session, status: "loading", me: null });

    expect(() => renderHook(() => useTenantID(), { wrapper })).toThrow("useTenantID must be used within a loaded session");
  });
});

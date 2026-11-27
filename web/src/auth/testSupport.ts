import type { AuthContextValue } from "./AuthContext";
import type { Me } from "./types";

// A signed-in test user in tenant_123 holding no global capability unless a
// test grants one. `me` and `capabilities` merge into the defaults, so a test
// names only what it depends on; `me: null` drops the user (loading, signed
// out).
export function authValue({
  me,
  capabilities,
  ...overrides
}: Omit<Partial<AuthContextValue>, "me"> & {
  me?: Partial<Me> | null;
  capabilities?: Partial<Me["global_capabilities"]>;
} = {}): AuthContextValue {
  return {
    me:
      me === null
        ? null
        : {
            sub: "user_1",
            tenant_id: "tenant_123",
            display_name: "Test User",
            ...me,
            global_capabilities: { can_create_stack: false, can_manage_templates: false, can_manage_platform_access: false, ...capabilities }
          },
    status: "authenticated",
    login: () => {},
    logout: () => {},
    ...overrides
  };
}

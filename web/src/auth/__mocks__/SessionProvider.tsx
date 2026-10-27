import { Outlet } from "react-router-dom";
import { AuthContext } from "../AuthContext";

export default function SessionProvider() {
  return (
    <AuthContext.Provider
      value={{
        me: {
          sub: "test",
          display_name: "Test",
          email: "test@example.com",
          tenant_id: "tenant_123",
          global_capabilities: { can_create_stack: true, can_manage_templates: true, can_manage_platform_access: true },
          session_expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
        },
        status: "authenticated" as const,
        login: () => {},
        logout: () => {},
      }}
    >
      <Outlet />
    </AuthContext.Provider>
  );
}

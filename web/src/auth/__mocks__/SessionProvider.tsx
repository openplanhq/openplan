import { Outlet } from "react-router-dom";
import { AuthContext } from "../AuthContext";
import { authValue } from "../testSupport";

export default function SessionProvider() {
  return (
    <AuthContext.Provider
      value={authValue({
        me: { sub: "test", display_name: "Test", email: "test@example.com", session_expires_at: new Date(Date.now() + 60 * 60 * 1000).toISOString() },
        capabilities: { can_create_stack: true, can_manage_templates: true, can_manage_platform_access: true }
      })}
    >
      <Outlet />
    </AuthContext.Provider>
  );
}

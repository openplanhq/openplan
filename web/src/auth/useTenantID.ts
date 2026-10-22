import { useAuth } from "./AuthContext";

// The tenant every API path is scoped to, as the API reports it on /v1/me.
// The web keeps no copy of its own, so it can never disagree with the API.
// SessionProvider renders nothing until me has loaded, so a null me here
// means a caller outside the session: a bug, not a state to fall back from.
export function useTenantID(): string {
  const { me } = useAuth();
  if (!me) {
    throw new Error("useTenantID must be used within a loaded session");
  }
  return me.tenant_id;
}

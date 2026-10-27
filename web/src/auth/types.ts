// Identity and capability contract the frontend commits to for AUTH-017.
// The backend resolves capabilities server-side; the frontend only ever reads booleans —
// no raw tokens, no role-name strings, and no tenant ID (implicit in the deployment).

// GET /v1/me
export interface Me {
  sub: string;
  display_name: string;
  email?: string;
  tenant_id: string;
  global_capabilities: {
    can_create_stack: boolean;
    /** Registering a template — which is also what the detail screen's Sync
        button posts, so it gates that button too. */
    can_manage_templates: boolean;
    /** Granting and revoking platform roles on /access. Platform admins
        and root hold it. */
    can_manage_platform_access: boolean;
  };
  /** RFC 3339. When the session's ID token expires. */
  session_expires_at?: string;
}

// Attached to every stack resource (list + detail) as `effective_capabilities`.
export interface StackCapabilities {
  can_view: boolean;
  can_operate: boolean;
  can_approve: boolean;
  can_manage_access: boolean;
}

// web/src/shared/queryErrorBoundary.tsx
import { useEffect } from "react";
import type { ReactNode } from "react";
import { classifyQueryError } from "../api/problems";
import { useAuth } from "../auth/AuthContext";
import AccessDenied from "../app/AccessDenied";
import NotFound from "../app/NotFound";
import ServiceUnavailable from "../app/ServiceUnavailable";

export function useQueryErrorBoundary(error: unknown): ReactNode | null {
  const { logout } = useAuth();
  const status = classifyQueryError(error);

  useEffect(() => {
    if (status === 401) {
      logout();
    }
  }, [status, logout]);

  switch (status) {
    case 401:
      return null;
    case 403:
      return <AccessDenied />;
    case 404:
      return <NotFound />;
    case 503:
      return <ServiceUnavailable />;
    default:
      return null;
  }
}

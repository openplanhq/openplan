// web/src/shared/queryErrorBoundary.tsx
import type { ReactNode } from "react";
import { classifyQueryError } from "../api/problems";
import AccessDenied from "../app/AccessDenied";
import NotFound from "../app/NotFound";
import ServiceUnavailable from "../app/ServiceUnavailable";

// A 401 draws nothing here: fetchWithAuth has already sent the browser to
// sign-in, and that navigation is the only response a lost session gets.
export function useQueryErrorBoundary(error: unknown): ReactNode | null {
  switch (classifyQueryError(error)) {
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

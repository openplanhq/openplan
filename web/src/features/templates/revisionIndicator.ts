import { LoaderCircle, TriangleAlert } from "lucide-react";
import type { TemplateRevisionStatus } from "../../api/types";
import type { StatusIndicator } from "../../shared/StatusLabel";

/**
 * A template revision's state as a StatusLabel draws it, or null for an
 * active revision, which needs saying nowhere. Validation runs on its own,
 * so a revision on its way is grey, like a run still working; only a failed
 * one is coloured.
 */
export function revisionIndicator(status: TemplateRevisionStatus): StatusIndicator | null {
  switch (status) {
    case "pending_validation":
      return { label: "waiting for validation", icon: LoaderCircle, tone: "settled", strong: false };
    case "validating":
      return { label: "validating", icon: LoaderCircle, tone: "settled", strong: false };
    case "invalid":
      return { label: "failed validation", icon: TriangleAlert, tone: "failed", strong: true };
    default:
      return null;
  }
}

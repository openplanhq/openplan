import { RefreshCw, ServerCrash } from "lucide-react";
import { cn } from "@/lib/utils";
import { buttonClass } from "../shared/buttonClass";
import RouteMessage from "./RouteMessage";

export default function ServiceUnavailable() {
  return (
    <RouteMessage
      icon={ServerCrash}
      title="Service unavailable"
      description="openplan can't do this right now. Contact your operator."
      testId="route-service-unavailable"
      action={
        // A reload, not a refetch: this page has no handle on the query
        // that failed, and a fresh load retries everything at once.
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          onClick={() => globalThis.location.reload()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      }
    />
  );
}

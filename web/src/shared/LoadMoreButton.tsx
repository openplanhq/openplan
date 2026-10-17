import { Loader2 } from "lucide-react";
import { buttonClass } from "./buttonClass";
import ErrorLine from "./ErrorLine";
import { cn } from "@/lib/utils";

/**
 * The next page of a paged list, on request: openplan UI's outline button
 * under the list's last row. It says it is loading while the page is on its
 * way, and if the page fails the rows already shown stay, with a line saying
 * so above a button that tries again.
 */
export default function LoadMoreButton({
  onClick,
  loading,
  failed,
  noun,
  className,
  testId
}: {
  onClick: () => void;
  loading: boolean;
  failed: boolean;
  // What the list holds, for the failure line: "stacks", "runs".
  noun: string;
  className?: string;
  testId?: string;
}) {
  return (
    <div className={cn("flex flex-col items-start gap-2", className)}>
      {failed && !loading && <ErrorLine>{`More ${noun} couldn't be loaded.`}</ErrorLine>}
      <button
        type="button"
        className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
        onClick={onClick}
        disabled={loading}
        aria-busy={loading || undefined}
        data-testid={testId}
      >
        {loading && <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />}
        {loading ? "Loading…" : failed ? "Try again" : "Load more"}
      </button>
    </div>
  );
}

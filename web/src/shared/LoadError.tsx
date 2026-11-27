import { RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import ErrorLine from "./ErrorLine";
import RequestIdNote from "./RequestIdNote";
import { buttonClass } from "./buttonClass";
import { cn } from "@/lib/utils";

// openplan UI's load failure: a line saying what failed to load, the request
// ID when the server faulted, and Retry. live announces it, for a failure that
// replaces what was on screen; lead is what stays above it, such as the
// page's trail.
export default function LoadError({
  children,
  error,
  onRetry,
  live = false,
  lead,
  as: Container = "div",
  className,
  testId,
  retryTestId
}: {
  children: ReactNode;
  error?: unknown;
  onRetry: () => void;
  live?: boolean;
  lead?: ReactNode;
  as?: "div" | "section";
  className?: string;
  testId?: string;
  retryTestId?: string;
}) {
  return (
    <Container className={cn("flex flex-col items-start gap-3", className)} data-testid={testId}>
      {lead}
      <ErrorLine live={live}>{children}</ErrorLine>
      <RequestIdNote error={error} />
      <button type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} data-testid={retryTestId} onClick={onRetry}>
        <RefreshCw data-icon="inline-start" aria-hidden="true" />
        Retry
      </button>
    </Container>
  );
}

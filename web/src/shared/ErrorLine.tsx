import { TriangleAlert } from "lucide-react";
import type { ReactNode } from "react";

// A failure said in one line, destructive text after a triangle, in place of
// an alert box. live announces it: true for something that just happened,
// such as a refused request; false for a record, such as a run's error.
export default function ErrorLine({ children, live = true, testId }: { children: ReactNode; live?: boolean; testId?: string }) {
  return (
    <p role={live ? "alert" : undefined} className="flex items-start gap-2 text-meta text-destructive" data-testid={testId}>
      <TriangleAlert aria-hidden="true" strokeWidth={2.25} className="mt-0.5 size-3.5 shrink-0" />
      <span className="min-w-0 wrap-anywhere">{children}</span>
    </p>
  );
}

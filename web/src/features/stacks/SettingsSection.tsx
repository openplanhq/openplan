import { useId } from "react";
import type { ReactNode } from "react";

// One setting on a template's Settings tab: a bordered row with what it does
// at the left and its button at the right, the button wrapping under the text
// on a phone. A locked button's reason sits under the text, so a disabled
// button never stands unexplained.
export default function SettingsSection({
  title,
  description,
  reason,
  reasonTestId,
  error,
  action,
  testId
}: {
  title: string;
  description: ReactNode;
  /** Why the action is locked, or "" when it is free. */
  reason: string;
  reasonTestId: string;
  /** A refused request, as an ErrorLine. */
  error?: ReactNode;
  action: ReactNode;
  testId: string;
}) {
  const headingId = useId();
  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-lg border px-5 py-4"
      data-testid={testId}
    >
      <div className="flex min-w-0 flex-1 basis-60 flex-col gap-1">
        <h3 id={headingId} className="text-sm font-semibold">
          {title}
        </h3>
        <p className="text-meta text-muted-foreground wrap-anywhere">{description}</p>
        {reason && (
          <p className="text-meta text-muted-foreground" data-testid={reasonTestId}>
            {reason}
          </p>
        )}
        {error}
      </div>
      {action}
    </section>
  );
}

import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { Loader2 } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { buttonClass } from "./buttonClass";
import { cn } from "@/lib/utils";

// openplan UI confirms in place, never in a dialog: the question appears where
// the action was asked for, so the record it is about stays in view. A
// GrantRow confirms inside its own row; this is the same question for an
// action on a whole panel, such as deleting a template, drawn as a Notice is
// (one border, canvas fill) so it reads as a step of its own rather than as
// part of the record.
//
// The question in label weight, what goes with it in meta under it, and at
// the right Cancel and the confirm, both outline, the confirm's words in
// destructive. Focus moves to Cancel when it opens, so a stray Enter cancels;
// Escape cancels too. While the request runs the confirm's icon turns and it
// stays focusable (aria-disabled), ignoring presses.
export default function InlineConfirm({
  question,
  children,
  confirmLabel,
  confirmIcon: ConfirmIcon,
  busy = false,
  onConfirm,
  onCancel,
  testId
}: {
  /** What is about to happen, as a question: "Delete aws/eks?" */
  question: ReactNode;
  /** What goes with it, in one sentence, when the question doesn't say. */
  children?: ReactNode;
  /** The confirm's words, naming the action and its object: "Delete template". */
  confirmLabel: string;
  confirmIcon: LucideIcon;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  testId?: string;
}) {
  const cancelRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    cancelRef.current?.focus();
  }, []);

  return (
    <div
      role="group"
      aria-label="Confirm"
      className="flex min-w-0 items-center justify-between gap-6 rounded-lg border bg-canvas px-5 py-4"
      data-testid={testId}
      onKeyDown={(event) => {
        if (event.key === "Escape" && !busy) {
          event.stopPropagation();
          onCancel();
        }
      }}
    >
      <div className="flex min-w-0 flex-col gap-1">
        <p className="text-label text-foreground wrap-anywhere">{question}</p>
        {children !== undefined && <p className="text-meta text-muted-foreground">{children}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <button ref={cancelRef} type="button" className={cn(buttonClass("outline"), "pointer-coarse:h-11")} onClick={() => !busy && onCancel()}>
          Cancel
        </button>
        <button
          type="button"
          className={cn(buttonClass("outline"), "text-destructive hover:text-destructive pointer-coarse:h-11")}
          aria-disabled={busy ? "true" : undefined}
          onClick={() => !busy && onConfirm()}
        >
          {busy ? (
            <Loader2 data-icon="inline-start" aria-hidden="true" className="animate-spin" />
          ) : (
            <ConfirmIcon data-icon="inline-start" aria-hidden="true" />
          )}
          {confirmLabel}
        </button>
      </div>
    </div>
  );
}

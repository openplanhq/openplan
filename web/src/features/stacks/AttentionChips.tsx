import { Hourglass, TriangleAlert } from "lucide-react";
import Chip from "../../shared/Chip";
import { runFailedChipLabel, waitingChipLabel } from "./attention";
import type { StackAttention } from "./attention";

// The Chips at the right of a list row, one per kind of thing that needs a
// person, waiting plans first. A template has at most one plan waiting, so on
// a template's row (single) its Chip gives no count; a failure reads the same
// either way, the word alone for one.
export default function AttentionChips({ attention, single = false }: { attention: StackAttention; single?: boolean }) {
  const { waiting, destroyFailed, planFailed, applyFailed } = attention;
  const failures = [
    ["destroy", destroyFailed],
    ["plan", planFailed],
    ["apply", applyFailed]
  ] as const;
  if (waiting + destroyFailed + planFailed + applyFailed === 0) {
    return null;
  }
  return (
    <span className="flex shrink-0 flex-col items-end gap-1">
      {waiting > 0 && (
        <Chip tone="warning" icon={Hourglass}>
          {single ? "plan to approve" : waitingChipLabel(waiting)}
        </Chip>
      )}
      {failures.map(
        ([operation, failed]) =>
          failed > 0 && (
            <Chip key={operation} tone="destructive" icon={TriangleAlert}>
              {runFailedChipLabel(operation, failed)}
            </Chip>
          )
      )}
    </span>
  );
}

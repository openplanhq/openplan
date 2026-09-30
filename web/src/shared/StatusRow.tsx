import StatusBadge from "./StatusBadge";
import { statusTone } from "./statusTone";

// A labelled status under a form or panel: the label at one edge, its status
// at the other, set off from what is above by a rule.
export default function StatusRow({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="mt-4 flex items-center justify-between gap-3 border-t pt-3 text-sm text-muted-foreground"
      data-status={value}
    >
      <span>{label}</span>
      <StatusBadge tone={statusTone(value)}>{value}</StatusBadge>
    </div>
  );
}

import type { PlanSummary } from "../api/types";
import { cn } from "@/lib/utils";

// A plan's counts in tofu's own notation, every count shown, zeros too, so
// plans line up down a column. The minus is U+2212, which sits level with the
// plus. Screen readers get the words instead of the signs.
export default function PlanDiff({ summary, className }: { summary: PlanSummary; className?: string }) {
  const label = `${summary.add} to add, ${summary.change} to change, ${summary.destroy} to destroy`;
  return (
    <span role="img" aria-label={label} title={label} className={cn("inline-flex gap-2.5 font-mono text-meta", className)}>
      <span className="text-success">+{summary.add}</span>
      <span className="text-warning">~{summary.change}</span>
      <span className="text-destructive">−{summary.destroy}</span>
    </span>
  );
}

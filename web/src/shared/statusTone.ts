/**
 * The API exposes three separate status unions (TemplateRunStatus with 7
 * values, TemplateRegistrationStatus with 5, TemplateRevisionStatus with 4).
 * Every status maps onto one of five visual tones.
 */
export type StatusTone = "settled" | "progress" | "waiting" | "failed" | "canceled";

const FAILED = new Set(["failed", "invalid", "error"]);
const CANCELED = new Set(["canceled"]);
const WAITING = new Set(["pending", "pending_validation", "queued", "waiting_approval"]);
const PROGRESS = new Set(["running", "validating"]);

export function statusTone(value: string): StatusTone {
  if (FAILED.has(value)) return "failed";
  if (CANCELED.has(value)) return "canceled";
  if (WAITING.has(value)) return "waiting";
  if (PROGRESS.has(value)) return "progress";

  // Callers pass human phrases such as "not configured" for absent resources.
  if (value.startsWith("not ")) return "waiting";

  // Completed, active, approved, and anything unrecognised are settled.
  return "settled";
}

const GLYPHS: Record<StatusTone, string> = {
  settled: "●",
  progress: "◐",
  waiting: "○",
  failed: "✕",
  canceled: "⊘"
};

export function statusGlyph(tone: StatusTone): string {
  return GLYPHS[tone];
}

// A tone's text colour, the one its StatusBadge variant uses, for a glyph or
// a note that carries the tone without the pill. Literal class names, so
// Tailwind finds them.
const TEXT_CLASSES: Record<StatusTone, string> = {
  settled: "text-success",
  progress: "text-primary",
  waiting: "text-warning",
  failed: "text-destructive",
  canceled: "text-muted-foreground"
};

export function toneTextClass(tone: StatusTone): string {
  return TEXT_CLASSES[tone];
}

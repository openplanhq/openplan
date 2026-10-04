import { cn } from "@/lib/utils";

// openplan UI's form controls, drawn on shadcn's Input and SelectTrigger:
// 36px tall, 13px mono text, a 12px inset, and focus as the 2px ring outline
// every other control has, in place of the vendored translucent ring.
const FOCUS = cn(
  "focus-visible:border-input focus-visible:ring-0",
  "focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
);

/** For shadcn's Input. md:text-meta outranks the Input's own md:text-sm. */
export const inputClass = cn("h-9 px-3 font-mono text-meta md:text-meta placeholder:text-subtle-foreground pointer-coarse:h-11", FOCUS);

/** For shadcn's SelectTrigger at its default size; its chevron keeps the right inset. */
export const selectTriggerClass = cn(
  "w-full pl-3 font-mono text-meta data-[size=default]:h-9 pointer-coarse:data-[size=default]:h-11",
  FOCUS
);

/** A sans field label above its control: 13px at weight 500. */
export const fieldLabelClass = "text-meta leading-label font-medium";

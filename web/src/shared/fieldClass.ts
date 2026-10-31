import { cn } from "@/lib/utils";

// openplan UI's form controls, drawn on shadcn's Input and SelectTrigger:
// 36px tall, 13px mono text, a 12px inset, and focus as the 2px ring outline
// every other control has, in place of the vendored translucent ring.
const FOCUS = cn(
  "focus-visible:border-input focus-visible:ring-0",
  "focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
);

/** For shadcn's Input. */
export const inputClass = cn("h-9 px-3 font-mono text-meta placeholder:text-subtle-foreground pointer-coarse:h-11", FOCUS);

/** For shadcn's InputGroup holding a textarea and a trailing button: the
    Input's focus ring, drawn on the group when its textarea has focus, and
    the button kept by the first line. The group's own rule sizes it to the
    textarea, so it grows with what is typed. The button keeps its own ring,
    so the field's isn't drawn while the button is focused. */
export const inputGroupClass = cn(
  "items-start",
  "has-[[data-slot=input-group-control]:focus-visible]:border-input has-[[data-slot=input-group-control]:focus-visible]:ring-0",
  "has-[[data-slot=input-group-control]:focus-visible]:outline-solid has-[[data-slot=input-group-control]:focus-visible]:outline-2",
  "has-[[data-slot=input-group-control]:focus-visible]:outline-offset-2 has-[[data-slot=input-group-control]:focus-visible]:outline-ring"
);

/** For shadcn's Textarea, holding a JSON value: the Input's mono text and
    inset, two lines tall when empty, growing with its content to about
    seventeen, then scrolling, so a long value can't push Save off the page. */
export const textareaClass = cn(
  "max-h-80 resize-y px-3 py-2 font-mono text-meta",
  FOCUS
);

/** For shadcn's SelectTrigger at its default size; its chevron keeps the right inset. */
export const selectTriggerClass = cn(
  "w-full pl-3 font-mono text-meta data-[size=default]:h-9 pointer-coarse:data-[size=default]:h-11",
  FOCUS
);

/** A sans field label above its control: 13px at weight 500. */
export const fieldLabelClass = "text-meta leading-label font-medium";

import type { ComponentProps } from "react";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { textareaClass } from "./fieldClass";

/** The text, inset and size every environment Value box shares, plain or
    masked: the Input's mono text and 12px inset, one line tall when empty
    so it matches the Name box beside it, then growing with what is typed or
    pasted to about sixteen lines, and scrolling past that. It grows on its
    own, so there is no resize handle. */
export const valueTextClass = "min-h-0 max-h-80 resize-none px-3 py-2 font-mono text-meta placeholder:text-subtle-foreground pointer-coarse:py-3";

/** Opts both Value boxes out of Grammarly and LanguageTool. Those extensions
    leave password inputs alone but read textareas, and a secret's mask is
    only drawn: the box holds the plain value they would send away. */
export const writingAssistantsOff = {
  "data-gramm": "false",
  "data-gramm_editor": "false",
  "data-enable-grammarly": "false",
  "data-lt-active": "false"
} as const;

/**
 * openplan UI's Value box for an environment entry that is read back, a
 * variable: shadcn's Textarea, so a multi-line value such as a CA
 * certificate keeps its line breaks, where an input would fold them into
 * spaces. Enter is a line break unless the caller's onKeyDown takes it.
 */
export default function ValueTextarea({ className, ...props }: ComponentProps<"textarea">) {
  return (
    <Textarea
      // One row is the box's height where field-sizing isn't supported, so
      // it still lines up with the Name box; elsewhere the content sizes it.
      rows={1}
      {...props}
      // A value isn't prose: no spelling marks sent anywhere, nothing
      // corrected or capitalised.
      spellCheck={false}
      autoCorrect="off"
      autoCapitalize="off"
      {...writingAssistantsOff}
      className={cn(textareaClass, valueTextClass, className)}
    />
  );
}

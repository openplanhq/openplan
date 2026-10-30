import { Eye, EyeOff } from "lucide-react";
import type { ComponentProps, CSSProperties } from "react";
import { InputGroup, InputGroupAddon, InputGroupTextarea } from "@/components/ui/input-group";
import { cn } from "@/lib/utils";
import { buttonClass } from "./buttonClass";
import { inputGroupClass } from "./fieldClass";
import { valueTextClass, writingAssistantsOff } from "./ValueTextarea";

type SecretInputProps = ComponentProps<"textarea"> & {
  /** Whether the typed value shows as text. The caller holds it, so it can
      put the field back to hidden, after a save for one. */
  revealed: boolean;
  onRevealedChange: (revealed: boolean) => void;
};

// A textarea has no password type, so it is masked with the property that
// draws a password input's discs. csstype has no entry for it (it is
// non-standard, though Chrome, Safari and Firefox all draw it), hence the
// cast. An inline style, since the class guard refuses arbitrary properties.
const MASKED = { WebkitTextSecurity: "disc" } as CSSProperties;

/**
 * openplan UI's secret field: a masked multi-line box with a Show value
 * button at its end, so the person can check what they typed before they
 * save it. Multi-line because the commonest secret, a PEM key, is: an input
 * would fold its line breaks into spaces and break it. It reveals only what
 * is in the box; a stored secret never comes back to show.
 */
export default function SecretInput({ revealed, onRevealedChange, className, ...props }: SecretInputProps) {
  const Icon = revealed ? EyeOff : Eye;
  return (
    // The group grows with the box, and its toggle stays by the first line
    // instead of drifting to the middle of a tall key.
    <InputGroup className={inputGroupClass}>
      <InputGroupTextarea
        rows={1}
        {...props}
        style={revealed ? undefined : MASKED}
        // Shown as text, a secret must not go to a spellcheck service (Chrome's
        // enhanced spellcheck sends typed text to Google) or be "corrected".
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
        {...writingAssistantsOff}
        className={cn(valueTextClass, className)}
      />
      {/* py-1.25 centres the 24px button on the box's first line: 5px
          above and below it fill the 34px inside the border. */}
      <InputGroupAddon align="inline-end" className="py-1.25 pr-1.5 has-[>button]:mr-0 pointer-coarse:py-2.25">
        {/* One fixed name with aria-pressed, as WAI-ARIA's toggle button
            pattern asks: a name that flipped between Show and Hide would
            contradict the pressed state it announces beside it. type=button
            keeps it from ever submitting a form around the field. */}
        <button
          type="button"
          aria-label="Show value"
          aria-pressed={revealed}
          onClick={() => onRevealedChange(!revealed)}
          className={cn(buttonClass("icon"), "size-6 rounded-md hover:text-foreground aria-pressed:text-foreground")}
        >
          <Icon aria-hidden="true" className="size-3.5" strokeWidth={2.25} />
        </button>
      </InputGroupAddon>
    </InputGroup>
  );
}

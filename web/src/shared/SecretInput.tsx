import { Eye, EyeOff } from "lucide-react";
import type { ComponentProps } from "react";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { cn } from "@/lib/utils";
import { buttonClass } from "./buttonClass";
import { inputGroupClass, inputGroupInputClass } from "./fieldClass";

type SecretInputProps = Omit<ComponentProps<"input">, "type"> & {
  /** Whether the typed value shows as text. The caller holds it, so it can
      put the field back to hidden, after a save for one. */
  revealed: boolean;
  onRevealedChange: (revealed: boolean) => void;
};

/**
 * openplan UI's secret field: a password input with a Show value button at
 * its end, so the person can check what they typed before they save it. It
 * reveals only what is in the box; a stored secret never comes back to show.
 */
export default function SecretInput({ revealed, onRevealedChange, className, ...props }: SecretInputProps) {
  const Icon = revealed ? EyeOff : Eye;
  return (
    <InputGroup className={inputGroupClass}>
      <InputGroupInput
        {...props}
        type={revealed ? "text" : "password"}
        // Shown as text, a secret must not go to a spellcheck service (Chrome's
        // enhanced spellcheck sends typed text to Google) or be "corrected".
        spellCheck={false}
        autoCorrect="off"
        autoCapitalize="off"
        className={cn(inputGroupInputClass, className)}
      />
      <InputGroupAddon align="inline-end" className="pr-1.5 has-[>button]:mr-0">
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

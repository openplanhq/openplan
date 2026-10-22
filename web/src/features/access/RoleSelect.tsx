import { Select } from "@base-ui/react/select";
import { Check, ChevronDown, LoaderCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { roleDefinition } from "./roles";
import type { RoleDefinition } from "./roles";

const FOCUS = "focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring";

// The two places a role is chosen: "field" beside the person picker, an
// outline control as tall as it; "quiet" in a grant's row, no border or fill
// until hovered, so a column of them reads as a column of roles.
const TRIGGER = {
  field: "h-9 w-full border border-input bg-card pr-2.5 pl-3 text-body hover:bg-muted pointer-coarse:h-11",
  quiet: "-ml-2 h-8 px-2 text-meta hover:bg-muted pointer-coarse:h-11"
};

/**
 * One ladder's roles, each option saying what the role adds. The text is
 * sans, unlike a Field's mono: a role is a word people read, not a value a
 * machine reads. `pending` turns the icon into a loader while a change saves.
 */
export default function RoleSelect({
  roles,
  value,
  onChange,
  label,
  variant = "quiet",
  disabled = false,
  pending = false,
  id,
  testId
}: {
  roles: readonly RoleDefinition[];
  value: string;
  onChange: (role: string) => void;
  /** The accessible name: what the role is for ("Lena Novak’s role"). */
  label: string;
  variant?: keyof typeof TRIGGER;
  disabled?: boolean;
  pending?: boolean;
  id?: string;
  testId?: string;
}) {
  const current = roleDefinition(roles, value);
  const Icon = pending ? LoaderCircle : current.icon;
  return (
    <Select.Root
      value={value}
      onValueChange={(next) => {
        // Base UI types the value as nullable; a role is always chosen. While
        // a change saves, the next waits: the control stays enabled so it
        // keeps keyboard focus, which a disabled button would drop.
        if (pending) return;
        if (typeof next === "string" && next !== value) onChange(next);
      }}
      disabled={disabled}
    >
      <Select.Trigger
        id={id}
        aria-label={`${label}: ${value}`}
        aria-busy={pending || undefined}
        data-testid={testId}
        className={cn(
          "inline-flex items-center gap-1.75 rounded-lg whitespace-nowrap text-foreground transition-colors outline-none select-none",
          "disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent data-popup-open:bg-muted",
          FOCUS,
          TRIGGER[variant]
        )}
      >
        <Icon aria-hidden="true" strokeWidth={2.25} className={cn("size-3.5 shrink-0 text-muted-foreground", pending && "animate-spin")} />
        <span className={cn(variant === "field" && "flex-1 text-left")}>{value}</span>
        <ChevronDown aria-hidden="true" strokeWidth={2.25} className="size-3.5 shrink-0 text-subtle-foreground" />
      </Select.Trigger>
      <Select.Portal>
        <Select.Positioner side="bottom" align="start" sideOffset={4} alignItemWithTrigger={false} className="isolate z-50">
          {/* A list drawn as openplan UI draws a picker's: a border, no shadow. */}
          <Select.Popup className="w-80 rounded-lg border bg-card p-1 text-foreground outline-none duration-100 data-open:animate-in data-open:fade-in-0 data-open:zoom-in-95 data-closed:animate-out data-closed:fade-out-0">
            <Select.List>
              {roles.map((role) => (
                <Select.Item
                  key={role.value}
                  value={role.value}
                  className="flex cursor-pointer items-start gap-2.5 rounded-md px-2.5 py-2 outline-none select-none data-highlighted:bg-primary-tint pointer-coarse:min-h-11"
                >
                  <role.icon aria-hidden="true" strokeWidth={2.25} className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <Select.ItemText className="text-label">{role.value}</Select.ItemText>
                    <span className="text-caption text-muted-foreground">{role.description}</span>
                  </span>
                  <Select.ItemIndicator className="mt-0.5 flex size-4 shrink-0 text-primary">
                    <Check aria-hidden="true" className="size-4" />
                  </Select.ItemIndicator>
                </Select.Item>
              ))}
            </Select.List>
          </Select.Popup>
        </Select.Positioner>
      </Select.Portal>
    </Select.Root>
  );
}

import { cn } from "@/lib/utils";

export interface SegmentedOption<Value extends string> {
  value: Value;
  label: string;
  // A plain count after the label, as a tab carries one. Left out until known.
  count?: number;
}

/**
 * Two to four views of one record, such as a run's Changes and Logs: a track
 * of toggle buttons, one pressed. It switches what the record shows, not
 * where the person is, so its options are buttons with aria-pressed rather
 * than links, and it is drawn apart from the underline Tabs it often sits
 * under, so it never reads as a second row of them.
 *
 * The track is muted with 2px around and between the options; an option is
 * 28px tall (44px on a coarse pointer), 13px medium text, and the pressed one
 * is a card with a border.
 */
export default function SegmentedControl<Value extends string>({
  label,
  options,
  value,
  onChange,
  className
}: {
  // Names the group for assistive technology: "Run views".
  label: string;
  options: SegmentedOption<Value>[];
  value: Value;
  onChange: (value: Value) => void;
  className?: string;
}) {
  return (
    <div role="group" aria-label={label} className={cn("inline-flex gap-0.5 rounded-lg bg-muted p-0.5", className)}>
      {options.map((option) => {
        const pressed = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={pressed}
            onClick={() => onChange(option.value)}
            className={cn(
              "inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-meta font-medium transition-colors duration-150",
              "focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring pointer-coarse:h-11",
              pressed ? "border-border bg-card text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
            )}
          >
            {option.label}
            {option.count !== undefined && <span className="text-xs font-medium text-subtle-foreground">{option.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

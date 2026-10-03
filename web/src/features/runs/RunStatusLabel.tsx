import type { ComponentProps } from "react";
import StatusLabel from "../../shared/StatusLabel";
import { runIndicator } from "./runIndicator";
import type { RunFields } from "./runIndicator";

// One run's state as openplan UI draws it: an icon and a word.
export default function RunStatusLabel({ run, ...props }: { run: RunFields } & Omit<ComponentProps<"span">, "children">) {
  const { label, icon, tone, strong } = runIndicator(run);
  return (
    <StatusLabel icon={icon} tone={tone} strong={strong} {...props}>
      {label}
    </StatusLabel>
  );
}

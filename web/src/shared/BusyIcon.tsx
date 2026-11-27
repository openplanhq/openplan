import { LoaderCircle } from "lucide-react";
import type { LucideIcon, LucideProps } from "lucide-react";
import { cn } from "@/lib/utils";

// A button's leading icon: a turning loader while its request runs, otherwise
// its action's icon, or nothing for a button that has none. Other props go to
// either icon, for a button that draws its icon at its own size.
export default function BusyIcon({ busy, icon: Icon, className, ...props }: { busy: boolean; icon?: LucideIcon } & LucideProps) {
  const Shown = busy ? LoaderCircle : Icon;
  return Shown ? <Shown data-icon="inline-start" aria-hidden="true" className={cn(className, busy && "animate-spin")} {...props} /> : null;
}

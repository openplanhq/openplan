import { LoaderCircle } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

// What a place shows while what fills it is on its way: a turning loader and
// "Loading X…" in meta text. A status, so a screen reader hears it.
export default function LoadingLine({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <p role="status" className={cn("flex items-center gap-2 text-meta text-muted-foreground", className)}>
      <LoaderCircle aria-hidden="true" className="size-3.5 animate-spin" />
      {children}
    </p>
  );
}

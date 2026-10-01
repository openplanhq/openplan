import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ScrollArea, ScrollAreaContent, ScrollAreaScrollbar, ScrollAreaThumb, ScrollAreaViewport } from "@/components/ui/scroll-area";

// Logs laid out as CI systems lay out a job's steps: stacked in the order they
// ran, each a row that opens onto its log. The caller decides which rows are
// open and supplies each log.
export function LogSteps({ children }: { children: ReactNode }) {
  return <ol className="divide-y overflow-hidden rounded-lg border">{children}</ol>;
}

interface LogStepProps {
  name: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Mounted only while the row is open, so a log fetched by a component
  // passed here is fetched only once someone opens it. Base UI unmounts a
  // closed panel.
  children: ReactNode;
}

// The list clips to its rounded corners, which would cut off a focus ring drawn
// outside these full-width rows, so the rings are drawn inside them.
export function LogStep({ name, open, onOpenChange, children }: LogStepProps) {
  return (
    <Collapsible open={open} onOpenChange={onOpenChange} render={<li />}>
      <CollapsibleTrigger className="group flex min-h-9 w-full items-center gap-2 px-4 text-left text-sm outline-none transition-colors hover:bg-muted focus-visible:inset-ring-2 focus-visible:inset-ring-ring pointer-coarse:min-h-11">
        <ChevronRight
          className="size-4 shrink-0 text-muted-foreground transition-transform group-aria-expanded:rotate-90"
          aria-hidden="true"
        />
        {name}
      </CollapsibleTrigger>
      <CollapsibleContent render={<div />} className="bg-foreground">
        <ScrollArea data-testid={`log-scroll-area-${name}`} className="max-h-115">
          <ScrollAreaViewport className="max-h-115 overflow-auto">
            <ScrollAreaContent>
              <pre className="m-0 px-5 py-4 font-mono text-sm whitespace-pre-wrap text-background">{children}</pre>
            </ScrollAreaContent>
          </ScrollAreaViewport>
          <ScrollAreaScrollbar>
            <ScrollAreaThumb />
          </ScrollAreaScrollbar>
        </ScrollArea>
      </CollapsibleContent>
    </Collapsible>
  );
}

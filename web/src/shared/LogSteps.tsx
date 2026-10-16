import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { ScrollArea, ScrollAreaContent, ScrollAreaScrollbar, ScrollAreaThumb, ScrollAreaViewport } from "@/components/ui/scroll-area";

// Logs laid out as CI systems lay out a job's steps: stacked in the order they
// ran, each a row that opens onto its log. The caller decides which rows are
// open and supplies each log, and draws the frame around them. Logs read on
// the canvas, in the code colour, rather than inverted, and long lines wrap:
// nothing would show that a clipped line goes on.
export function LogSteps({ children }: { children: ReactNode }) {
  return <ol className="divide-y divide-divider">{children}</ol>;
}

// One phase's steps under a header that names it and says who started it and
// when. The header is what tells apart steps that run in both phases, such as
// init, so the steps never repeat the phase. Phases stack in a
// divide-y divide-divider parent.
export function LogPhase({ title, detail, children }: { title: string; detail?: ReactNode; children: ReactNode }) {
  return (
    <div>
      <h5 className="flex items-baseline gap-1.5 border-b border-divider bg-canvas px-4 py-2 text-xs leading-4 font-normal text-muted-foreground">
        <span className="font-medium text-foreground">{title}</span>
        {detail && <span>· {detail}</span>}
      </h5>
      <LogSteps>{children}</LogSteps>
    </div>
  );
}

interface LogStepProps {
  // Identifies the step, unique among its siblings.
  name: string;
  // What the row reads; the name when absent. Two steps can share a label,
  // as a run's two "tofu init" steps do, but never a name.
  label?: ReactNode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Mounted only while the row is open, so a log fetched by a component
  // passed here is fetched only once someone opens it. Base UI unmounts a
  // closed panel.
  children: ReactNode;
}

// The list clips to its rounded corners, which would cut off a focus ring drawn
// outside these full-width rows, so the rings are drawn inside them.
export function LogStep({ name, label, open, onOpenChange, children }: LogStepProps) {
  return (
    <Collapsible open={open} onOpenChange={onOpenChange} render={<li />}>
      <CollapsibleTrigger className="group flex min-h-10 w-full items-center gap-2 px-4 text-left font-mono text-meta outline-none transition-colors hover:bg-canvas focus-visible:inset-ring-2 focus-visible:inset-ring-ring pointer-coarse:min-h-11">
        <ChevronRight
          className="size-3.5 shrink-0 text-subtle-foreground transition-transform group-aria-expanded:rotate-90"
          aria-hidden="true"
        />
        {label ?? name}
      </CollapsibleTrigger>
      <CollapsibleContent render={<div />} className="border-t border-divider bg-canvas">
        <ScrollArea data-testid={`log-scroll-area-${name}`} className="max-h-115">
          <ScrollAreaViewport className="max-h-115 overflow-auto">
            <ScrollAreaContent>
              <pre className="m-0 py-3 pr-4 pl-9.5 font-mono text-xs leading-relaxed whitespace-pre-wrap wrap-anywhere text-code-foreground">{children}</pre>
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

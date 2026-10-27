import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";

export interface UnderlineTab {
  to: string;
  label: string;
  current: boolean;
  testId?: string;
}

// openplan UI's Tabs: underline links in a nav named for what they switch,
// so each tab has its own address. The current tab is foreground with a 2px
// primary line drawn over the header's divider; the rest are muted until
// hovered. The focus ring is drawn inside, where the header cannot clip it.
export default function UnderlineTabs({ label, tabs }: { label: string; tabs: UnderlineTab[] }) {
  return (
    <nav aria-label={label} className="flex flex-wrap items-center gap-x-6">
      {tabs.map((tab) => (
        <Link
          key={tab.to}
          to={tab.to}
          aria-current={tab.current ? "page" : undefined}
          data-testid={tab.testId}
          className={cn(
            "-mb-px inline-flex h-10 items-center border-b-2 text-label transition-colors focus-visible:-outline-offset-2 pointer-coarse:h-11",
            tab.current ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
          )}
        >
          {tab.label}
        </Link>
      ))}
    </nav>
  );
}

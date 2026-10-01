import type { ReactNode } from "react";
import { NavLink } from "react-router-dom";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

/**
 * A row of tabs where each tab is a route: shadcn's Tabs, with every trigger
 * rendered as a NavLink (Base UI's "tabs as links"). The route decides which
 * tab is active, so the caller derives `value` from the pathname, and a click
 * or Enter follows the link instead of setting state. Base UI supplies the
 * tablist semantics and the keyboard: Tab reaches the active tab, the arrow
 * keys move between tabs, and Enter opens the focused one.
 *
 * There is no tab panel. The route's own content, rendered by an <Outlet>, is
 * what each tab shows.
 */
export function RouteTabs({
  value,
  label,
  children,
  className
}: {
  /** The active tab's value, or null when the route matches none. */
  value: string | null;
  /** Names the tab list for assistive technology. */
  label: string;
  children: ReactNode;
  /** Replaces the page spacing below the row; a row that sits beside other content passes "mb-0". */
  className?: string;
}) {
  return (
    <Tabs value={value} className={cn("mb-6", className)}>
      {/* On a coarse pointer each tab is 44px tall, so the list drops its
          fixed h-8 and grows to fit them. */}
      <TabsList variant="line" aria-label={label} className="pointer-coarse:h-auto">
        {children}
      </TabsList>
    </Tabs>
  );
}

/**
 * One tab of a RouteTabs row. `to` and `end` are NavLink's: `end` keeps a
 * parent route's tab from matching its children.
 *
 */
export function RouteTab({ value, to, end, children }: { value: string; to: string; end?: boolean; children: ReactNode }) {
  return (
    <TabsTrigger
      value={value}
      nativeButton={false}
      render={<NavLink to={to} end={end} />}
      className="pointer-coarse:h-11"
    >
      {children}
    </TabsTrigger>
  );
}

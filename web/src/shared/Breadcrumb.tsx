import { Fragment } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import {
  Breadcrumb as BreadcrumbNav,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";
import { cn } from "@/lib/utils";

export type Crumb = {
  label: ReactNode;
  /** Omitted on the last crumb, which is the current page. */
  to?: string;
  testId?: string;
};

// The page's title bar. The last crumb is the page's h1, so every screen keeps
// exactly one top-level heading even though nothing on screen looks like one.
// The separators are aria-hidden, so assistive tech counts only real crumbs.
//
// Until PR 9, base.css styles bare a, a:hover and h1 from the legacy layer, and
// a rule on the element beats a colour inherited from the list. So the links
// and the h1 set their own colour, type and decoration (AppShell's navLinkClass
// says why no-underline, not hover:no-underline).
//
// Any crumb can hold a long, user-chosen name. wrap-anywhere, unlike shadcn's
// wrap-break-word, lets a crumb shrink below its longest word.
export default function Breadcrumb({
  items,
  detail,
  className
}: {
  items: Crumb[];
  detail?: ReactNode;
  /** Replaces the page spacing below it; header rows pass "mb-0". */
  className?: string;
}) {
  const current = items[items.length - 1];
  const trail = items.slice(0, -1);

  return (
    <BreadcrumbNav
      aria-label="Breadcrumb"
      className={cn("mb-6 flex min-h-9 min-w-0 flex-wrap items-center gap-x-3 gap-y-1", className)}
    >
      <BreadcrumbList className="wrap-anywhere">
        {trail.map((crumb, index) => (
          <Fragment key={index}>
            <BreadcrumbItem>
              {crumb.to ? (
                <BreadcrumbLink
                  render={<Link to={crumb.to} />}
                  className="text-muted-foreground no-underline"
                  data-testid={crumb.testId}
                >
                  {crumb.label}
                </BreadcrumbLink>
              ) : (
                <span data-testid={crumb.testId}>{crumb.label}</span>
              )}
            </BreadcrumbItem>
            <BreadcrumbSeparator />
          </Fragment>
        ))}
        <BreadcrumbItem>
          <h1
            aria-current="page"
            data-testid={current.testId}
            className="font-sans text-sm font-normal tracking-normal text-foreground"
          >
            {current.label}
          </h1>
        </BreadcrumbItem>
      </BreadcrumbList>
      {detail && (
        <span data-slot="breadcrumb-detail" className="font-mono text-xs text-muted-foreground wrap-anywhere">
          {detail}
        </span>
      )}
    </BreadcrumbNav>
  );
}

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

export type Crumb = {
  label: ReactNode;
  to: string;
};

// openplan UI's Breadcrumb: the pages above this one, each a link, then this
// one as plain text. label names the navigation: "Breadcrumb" above a page's
// title, or the view's own name for a view nested in a tab ("Runs / Run #14").
//
// Any crumb can hold a long, user-chosen name. wrap-anywhere, unlike shadcn's
// wrap-break-word, lets a crumb shrink below its longest word.
export default function Breadcrumb({
  crumbs,
  current,
  label = "Breadcrumb",
  className
}: {
  crumbs: Crumb[];
  current: ReactNode;
  label?: string;
  className?: string;
}) {
  return (
    <BreadcrumbNav aria-label={label} className={className}>
      <BreadcrumbList className="gap-1.5 text-body wrap-anywhere sm:gap-1.5">
        {crumbs.map((crumb, index) => (
          <Fragment key={index}>
            <BreadcrumbItem>
              <BreadcrumbLink render={<Link to={crumb.to} />} className="text-muted-foreground hover:underline">
                {crumb.label}
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator className="text-separator">/</BreadcrumbSeparator>
          </Fragment>
        ))}
        {/* Plain text: shadcn's BreadcrumbPage claims role="link". */}
        <BreadcrumbItem>
          <span aria-current="page" className="text-foreground">
            {current}
          </span>
        </BreadcrumbItem>
      </BreadcrumbList>
    </BreadcrumbNav>
  );
}

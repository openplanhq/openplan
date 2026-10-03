import { Fragment } from "react";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import type { Crumb } from "./Breadcrumb";
import { Badge } from "@/components/ui/badge";
import {
  Breadcrumb as BreadcrumbNav,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";

// openplan UI's page header: a 24px title with its count, an optional trail of
// the pages above it ending in this one, a line under the title (the attention
// summary), and the page's one action, right-aligned with the bottom of the
// title block. On a phone the action drops under the title at full width.
//
// Pages built on it carry their own h1, unlike the screens that still use
// Breadcrumb as their title bar.
export default function PageHeader({
  title,
  count,
  trail,
  action,
  children
}: {
  title: string;
  count?: number;
  /** The pages above this one, each a link. The trail ends with this page. */
  trail?: Crumb[];
  action?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <header className="mb-5 flex flex-col gap-4 md:flex-row md:items-end md:justify-between md:gap-6">
      <div className="flex min-w-0 flex-col gap-1.5">
        {trail && trail.length > 0 && (
          <BreadcrumbNav aria-label="Breadcrumb" className="mb-1">
            <BreadcrumbList className="gap-1.5 text-sm wrap-anywhere sm:gap-1.5">
              {trail.map((crumb, index) => (
                <Fragment key={index}>
                  <BreadcrumbItem>
                    <BreadcrumbLink
                      render={<Link to={crumb.to ?? "/"} />}
                      className="text-muted-foreground hover:underline"
                      data-testid={crumb.testId}
                    >
                      {crumb.label}
                    </BreadcrumbLink>
                  </BreadcrumbItem>
                  <BreadcrumbSeparator className="text-separator">/</BreadcrumbSeparator>
                </Fragment>
              ))}
              {/* Plain text: shadcn's BreadcrumbPage claims role="link". */}
              <BreadcrumbItem>
                <span aria-current="page" className="text-foreground">
                  {title}
                </span>
              </BreadcrumbItem>
            </BreadcrumbList>
          </BreadcrumbNav>
        )}
        <div className="flex min-w-0 items-center gap-2.5">
          <h1 className="font-heading text-page-title font-semibold tracking-title wrap-anywhere">{title}</h1>
          {count !== undefined && (
            <Badge variant="muted" className="bg-muted-strong" data-testid="page-count">
              {count}
            </Badge>
          )}
        </div>
        {children}
      </div>
      {action}
    </header>
  );
}

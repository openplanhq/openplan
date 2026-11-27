import type { ReactNode } from "react";
import Breadcrumb from "./Breadcrumb";
import type { Crumb } from "./Breadcrumb";

// openplan UI's page header: a 24px title, an optional trail of
// the pages above it ending in this one, a line under the title (the attention
// summary), and the page's one action, right-aligned with the bottom of the
// title block. On a phone the action drops under the title at full width.
export default function PageHeader({
  title,
  trail,
  action,
  children
}: {
  title: string;
  /** The pages above this one, each a link. The trail ends with this page. */
  trail?: Crumb[];
  action?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <header className="mb-5 flex flex-col gap-4 md:flex-row md:items-end md:justify-between md:gap-6">
      <div className="flex min-w-0 flex-col gap-1.5">
        {trail && trail.length > 0 && <Breadcrumb crumbs={trail} current={title} className="mb-1" />}
        <h1 className="font-heading text-page-title wrap-anywhere">{title}</h1>
        {children}
      </div>
      {action}
    </header>
  );
}

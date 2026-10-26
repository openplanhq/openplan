import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from "@/components/ui/empty";

/**
 * A page that is only a message: not found, not permitted, a service that is
 * down, a screen still to come. It renders inside the app shell, in place of
 * the screen that couldn't render.
 *
 * shadcn's Empty with a real h1: the message is the page's heading, and
 * EmptyTitle renders a div. Preflight leaves a heading with the body's type,
 * so the h1 sets its own family, size, weight and tracking.
 *
 * openplan UI's page-level EmptyState draws its icon plain in muted rather
 * than on shadcn's tile, and ends with the one action the page offers: a
 * way back when the page can't be reached, a retry when it may be soon.
 */
export default function RouteMessage({
  icon: Icon,
  title,
  description,
  testId,
  action
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  testId: string;
  action?: ReactNode;
}) {
  return (
    <Empty className="py-16 md:py-24" data-testid={testId}>
      <EmptyHeader>
        <EmptyMedia>
          <Icon aria-hidden="true" className="size-5 text-muted-foreground" />
        </EmptyMedia>
        <h1 className="font-heading text-title">{title}</h1>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      {action}
    </Empty>
  );
}

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
 */
export default function RouteMessage({
  icon: Icon,
  title,
  description,
  testId
}: {
  icon: LucideIcon;
  title: string;
  description: string;
  testId: string;
}) {
  return (
    <Empty className="py-16 md:py-24" data-testid={testId}>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon aria-hidden="true" />
        </EmptyMedia>
        <h1 className="font-heading text-lg font-medium tracking-tight">{title}</h1>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

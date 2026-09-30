import type { ComponentProps } from "react";
import { Card } from "@/components/ui/card";
import { cn } from "@/lib/utils";

/**
 * The page frame for the screens outside the app shell: sign-in, and the
 * session errors SessionProvider shows before there is a shell to show. A card
 * centred on the viewport, which marks them as a threshold rather than a place
 * inside the product.
 *
 * Until PR 9, body keeps the legacy colours (see base.css), so the frame sets
 * its own background and text colour.
 */
export default function AuthCard({ className, ...props }: ComponentProps<typeof Card>) {
  return (
    <main className="grid min-h-svh place-items-center bg-background p-6 text-foreground">
      <Card className={cn("w-full max-w-sm", className)} {...props} />
    </main>
  );
}

/**
 * CardTitle's look on a real h1, since CardTitle renders a div. Until PR 9,
 * base.css gives every h1 the legacy 40px display type, and a rule on the
 * element beats anything it would inherit, so this sets its own family, size,
 * weight and tracking.
 */
export function AuthCardTitle({ className, ...props }: ComponentProps<"h1">) {
  return (
    <h1 className={cn("font-heading text-base leading-snug font-medium tracking-normal", className)} {...props} />
  );
}

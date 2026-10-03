import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// openplan UI's Button, drawn on shadcn's. Two things differ from the vendored
// component, which other screens still use as it is:
//
//   - padding: 12px at the sides, 10px on the side that holds an icon (shadcn
//     pads 10px and 8px);
//   - focus: a 2px solid ring outline, offset 2px, as every other focusable
//     element has, in place of shadcn's translucent 3px ring. outline-solid is
//     needed because the base class's outline-none sets the style to none.
//
// The section variant is the outline button in a preview panel's section nav:
// 10px at both sides and 13px text.
const SHARED = cn(
  "px-3 has-data-[icon=inline-start]:pl-2.5 has-data-[icon=inline-end]:pr-2.5",
  "focus-visible:ring-0 focus-visible:outline-solid focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
);

const VARIANTS = {
  primary: cn(buttonVariants({ variant: "default" }), SHARED, "focus-visible:border-transparent"),
  outline: cn(buttonVariants({ variant: "outline" }), SHARED, "focus-visible:border-border"),
  section: cn(
    buttonVariants({ variant: "outline" }),
    SHARED,
    "focus-visible:border-border px-2.5 text-meta has-data-[icon=inline-start]:pl-2.5"
  )
};

/** The classes of an openplan UI button: 36px tall at "lg", 32px otherwise. */
export function buttonClass(variant: keyof typeof VARIANTS = "primary", size: "default" | "lg" = "default"): string {
  return cn(VARIANTS[variant], size === "lg" && "h-9");
}

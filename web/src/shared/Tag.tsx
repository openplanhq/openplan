import type { ReactNode } from "react";

// openplan UI's Tag: a fact about a thing (a tag it carries, "sensitive",
// "latest"), drawn the same on every screen and never coloured by its value.
export default function Tag({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-sm border bg-canvas px-1.75 py-px font-mono text-caption text-tag-foreground">
      {children}
    </span>
  );
}

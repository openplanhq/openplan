import { Search } from "lucide-react";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { cn } from "@/lib/utils";

// openplan UI's SearchField on shadcn's InputGroup: the glass 11px from the
// edge, the text 35px in, and focus drawn as the 2px ring outline every other
// control has rather than the group's translucent ring. Add template's search
// draws its field with it too.
export const searchFieldClass = cn(
  "h-9 bg-canvas has-[>[data-align=inline-start]]:[&>input]:pl-2",
  "has-[[data-slot=input-group-control]:focus-visible]:border-input has-[[data-slot=input-group-control]:focus-visible]:ring-0",
  "has-[[data-slot=input-group-control]:focus-visible]:outline-solid has-[[data-slot=input-group-control]:focus-visible]:outline-2",
  "has-[[data-slot=input-group-control]:focus-visible]:outline-offset-2 has-[[data-slot=input-group-control]:focus-visible]:outline-ring"
);

/** A filter box at the top of a list. Its label is also its placeholder, so
    the visible text and the accessible name say the same thing. */
export default function SearchField({
  label,
  value,
  onChange,
  testId
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  testId?: string;
}) {
  return (
    <InputGroup className={searchFieldClass}>
      <InputGroupAddon className="pl-2.5">
        <Search aria-hidden="true" className="text-subtle-foreground" />
      </InputGroupAddon>
      <InputGroupInput
        type="search"
        aria-label={label}
        placeholder={label}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="placeholder:text-subtle-foreground"
        data-testid={testId}
      />
    </InputGroup>
  );
}

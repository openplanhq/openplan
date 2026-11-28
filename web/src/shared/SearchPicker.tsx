import { Fragment } from "react";
import type { ComponentProps, ReactNode } from "react";
import { Combobox } from "@base-ui/react";
import { Search } from "lucide-react";
import { searchFieldClass } from "./SearchField";
import { ComboboxContent, ComboboxEmpty, ComboboxList, useComboboxAnchor } from "@/components/ui/combobox";
import { InputGroup, InputGroupAddon, InputGroupInput } from "@/components/ui/input-group";
import { cn } from "@/lib/utils";

type SearchPickerProps<Item> = Pick<Combobox.Root.Props<Item>, "open" | "onOpenChange" | "itemToStringLabel" | "isItemEqualToValue" | "disabled"> & {
  items: Item[];
  query: string;
  onQueryChange: (query: string) => void;
  onPick: (item: Item) => void;
  input: ComponentProps<typeof Combobox.Input>;
  /** What the list says when nothing matches, spaced and coloured by emptyClassName. */
  empty: ReactNode;
  emptyClassName: string;
  count: string;
  children: (item: Item) => ReactNode;
};

// openplan UI's SearchPicker: a SearchField opening a list of ChoiceRows, the
// best few matches, over a footer with their count and the keys. The caller
// ranks the matches and decides what a pick means, so the Combobox neither
// filters nor holds a value, and its input only ever holds what was typed.
export default function SearchPicker<Item>({ query, onQueryChange, onPick, input, empty, emptyClassName, count, children, ...root }: SearchPickerProps<Item>) {
  // The whole field, glass and all, so the list lines up with its edges.
  const anchorRef = useComboboxAnchor();
  return (
    <Combobox.Root<Item>
      {...root}
      filter={null}
      value={null}
      onValueChange={(item) => item && onPick(item)}
      inputValue={query}
      onInputValueChange={(value, { reason }) => {
        // A pick writes the item's name into the input; only typing is a query.
        if (reason === "input-change") onQueryChange(value);
      }}
      autoHighlight
    >
      <InputGroup ref={anchorRef} className={cn(searchFieldClass, "pointer-coarse:h-11", root.disabled && "opacity-50")}>
        <InputGroupAddon className="pl-2.5">
          <Search aria-hidden="true" className="text-subtle-foreground" />
        </InputGroupAddon>
        <Combobox.Input render={<InputGroupInput />} className="placeholder:text-subtle-foreground" {...input} />
      </InputGroup>
      {/* The vendored popup and list, as wide as the field and drawn like a
          ChoiceRow group: a border, no shadow, choices a divider apart. */}
      <ComboboxContent anchor={anchorRef} className="flex min-w-0 flex-col border bg-card shadow-none ring-0">
        <ComboboxEmpty className={cn("flex-col items-start justify-start px-4 py-3 text-left text-meta", emptyClassName)}>{empty}</ComboboxEmpty>
        <ComboboxList className="max-h-96 min-h-0 divide-y divide-divider p-0">{children}</ComboboxList>
        {root.items.length > 0 && (
          <div className="flex items-center justify-between gap-4 border-t border-divider bg-canvas px-4 py-2 text-caption text-muted-foreground">
            <span>{count}</span>
            <span aria-hidden="true" className="whitespace-nowrap pointer-coarse:hidden">
              ↑ ↓ to move · Enter to choose · Esc to close
            </span>
          </div>
        )}
      </ComboboxContent>
    </Combobox.Root>
  );
}

/** openplan UI's ChoiceRow: a name over a caption line, and what sits at its
    right. A choice that can't be picked keeps its place, its name muted. */
export function ChoiceRow<Item>({ value, disabled = false, name, caption, aside }: { value: Item; disabled?: boolean; name: ReactNode; caption: ReactNode; aside?: ReactNode }) {
  return (
    <Combobox.Item
      value={value}
      disabled={disabled}
      className="flex min-h-13 w-full cursor-pointer items-center justify-between gap-4 px-4 py-2 text-left outline-none select-none data-disabled:cursor-not-allowed data-highlighted:bg-primary-tint pointer-coarse:min-h-14"
    >
      <span className="flex min-w-0 flex-col gap-0.5">
        <span className={cn("truncate text-label", disabled ? "text-muted-foreground" : "text-foreground")}>{name}</span>
        {caption}
      </span>
      {aside}
    </Combobox.Item>
  );
}

// The matched letters drawn heavier, never in colour: primary is kept for the
// action, the link, the focus ring and the selection.
export function Highlighted({ text, hits, markClassName }: { text: string; hits: number[]; markClassName: string }) {
  if (hits.length === 0) {
    return <>{text}</>;
  }
  const hit = new Set(hits);
  const runs: Array<{ text: string; hit: boolean }> = [];
  for (let index = 0; index < text.length; index++) {
    const isHit = hit.has(index);
    const last = runs[runs.length - 1];
    if (last && last.hit === isHit) {
      last.text += text[index];
    } else {
      runs.push({ text: text[index], hit: isHit });
    }
  }
  return (
    <>
      {runs.map((run, index) =>
        run.hit ? (
          <mark key={index} className={cn("bg-transparent text-foreground", markClassName)}>
            {run.text}
          </mark>
        ) : (
          <Fragment key={index}>{run.text}</Fragment>
        )
      )}
    </>
  );
}

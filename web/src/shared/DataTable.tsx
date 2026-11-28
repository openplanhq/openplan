import type { ReactNode } from "react";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";

// 8px either side of a column boundary makes the 16px between columns, and
// 16px at the row's ends, so a column's width is its content's plus 16px.
export const tableCellClass = "px-2 py-2 first:pl-4 last:pr-4";

// openplan UI's table: fixed columns, so a cell that changes length never
// moves the ones after it, in a bordered frame that scrolls sideways when the
// panel is narrower than the table. The header row, when it has one, names
// each column in caption text, padded as cellClass pads the body's cells.
export default function DataTable({
  columns,
  headings,
  cellClass = tableCellClass,
  className,
  children
}: {
  /** Each column's width class; undefined takes the width left over. */
  columns: Array<string | undefined>;
  headings?: string[];
  cellClass?: string;
  /** The table's own classes, such as the width it scrolls below. */
  className?: string;
  /** The body's rows. */
  children: ReactNode;
}) {
  return (
    <div className="overflow-x-auto rounded-lg border">
      <Table className={className}>
        <colgroup>
          {columns.map((width, index) => (
            <col key={index} className={width} />
          ))}
        </colgroup>
        {headings && (
          <TableHeader>
            <TableRow className="border-divider hover:bg-transparent">
              {headings.map((heading) => (
                <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-caption font-medium text-muted-foreground")}>
                  {heading}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
        )}
        <TableBody>{children}</TableBody>
      </Table>
    </div>
  );
}

import { useTemplateRevisionVariablesQuery } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import LoadError from "../../shared/LoadError";
import Tag from "../../shared/Tag";
import LoadingLine from "../../shared/LoadingLine";
import DataTable, { tableCellClass } from "../../shared/DataTable";
import { TableCell, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { useTemplate } from "./templateContext";
import { Breakable, piecesAfter } from "../../shared/breakAfter";

// Name 200, type 120 and value 90, plus the table's padding. Baseline, not
// the table's centring: a row shares one baseline, so a value stays beside
// the first line of a name that wraps or carries a tag.
const cellClass = cn(tableCellClass, "align-baseline");

// A type expression wraps only at its structure: after an opening bracket, a
// comma or a space (which the browser already breaks at), never inside a
// word, so optional(map(string)) reads "optional(" then "map(string))". Only
// a single word wider than the column breaks where it must.
function typePieces(typeExpression: string): string[] {
  return piecesAfter(typeExpression, "({[,");
}

// /templates/:id/variables: what the template asks for, read from its newest
// revision. The API says whether a variable has a default, not what it is, so
// a variable is required or optional.
export default function VariablesTab() {
  const tenantID = useTenantID();
  const { latest } = useTemplate();
  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, latest.id);
  const boundary = useQueryErrorBoundary(variablesQuery.error);

  // The query keeps the previous revision's variables while the newest load;
  // shown here, they would read as the newest revision's.
  if (variablesQuery.status === "pending" || variablesQuery.isPlaceholderData) {
    return (
      <LoadingLine>Loading variables…</LoadingLine>
    );
  }

  if (variablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <LoadError error={variablesQuery.error} onRetry={() => void variablesQuery.refetch()}>
        Something went wrong while loading the template's variables.
      </LoadError>
    );
  }

  const variables = variablesQuery.data;
  if (variables.length === 0) {
    return (
      <p className="text-meta text-muted-foreground">
        This template takes no variables.
      </p>
    );
  }

  return (
    <section className="flex min-w-0 flex-col gap-3">
      <DataTable columns={["w-56", "w-34", "w-26", undefined]} headings={["Name", "Type", "Value", "Description"]} cellClass={cellClass} className="min-w-2xl">
        {variables.map((templateVariable) => (
          <TableRow key={templateVariable.name} className="h-12 border-divider hover:bg-transparent">
            <TableCell className={cn(cellClass, "font-mono text-meta whitespace-normal wrap-anywhere")}>
              {/* Sensitive is a fact about the variable, not about whether it
                  needs a value, so it rides with the name in the Tag look
                  "latest" uses, and wraps under a long name. */}
              {templateVariable.sensitive ? (
                <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                  <span>{templateVariable.name}</span>
                  <Tag>sensitive</Tag>
                </span>
              ) : (
                templateVariable.name
              )}
            </TableCell>
            <TableCell className={cn(cellClass, "font-mono text-caption whitespace-normal text-muted-foreground wrap-anywhere")}>
              <Breakable pieces={typePieces(templateVariable.type_expression || "any")} />
            </TableCell>
            <TableCell className={cn(cellClass, "text-meta", templateVariable.required ? "font-medium text-foreground" : "text-muted-foreground")}>
              {templateVariable.required ? "required" : "optional"}
            </TableCell>
            <TableCell className={cn(cellClass, "text-meta whitespace-normal text-muted-foreground")}>{templateVariable.description}</TableCell>
          </TableRow>
        ))}
      </DataTable>
    </section>
  );
}

import { Loader2, RefreshCw } from "lucide-react";
import { useTemplateRevisionVariablesQuery } from "../../api/queries";
import { tenantID } from "../../config";
import { buttonClass } from "../../shared/buttonClass";
import ErrorLine from "../../shared/ErrorLine";
import { useQueryErrorBoundary } from "../../shared/queryErrorBoundary";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { useTemplate } from "./templateContext";
import { shortCommitSHA } from "./templateWorkflow";

// 8px either side of a column boundary makes the 16px between columns, and
// 16px at the row's ends. Name 200, type 120 and value 90, plus that padding.
const cellClass = "px-2 py-2 first:pl-4 last:pr-4";

// /templates/:id/variables: what the template asks for, read from its newest
// revision. The API says whether a variable has a default, not what it is, so
// a variable is required or optional.
export default function VariablesTab() {
  const { latest } = useTemplate();
  const variablesQuery = useTemplateRevisionVariablesQuery(tenantID, latest.id);
  const boundary = useQueryErrorBoundary(variablesQuery.error);

  if (variablesQuery.status === "pending") {
    return (
      <p className="flex items-center gap-2 text-meta text-muted-foreground" data-testid="template-variables-loading">
        <Loader2 aria-hidden="true" className="size-3.5 animate-spin" /> Loading variables…
      </p>
    );
  }

  if (variablesQuery.status === "error") {
    if (boundary !== null) {
      return <>{boundary}</>;
    }
    return (
      <div className="flex flex-col items-start gap-3" data-testid="template-variables-error">
        <ErrorLine live={false}>Something went wrong while loading the template's variables.</ErrorLine>
        <button
          type="button"
          className={cn(buttonClass("outline"), "pointer-coarse:h-11")}
          data-testid="template-variables-retry"
          onClick={() => void variablesQuery.refetch()}
        >
          <RefreshCw data-icon="inline-start" aria-hidden="true" />
          Retry
        </button>
      </div>
    );
  }

  const variables = variablesQuery.data;
  if (variables.length === 0) {
    return (
      <p className="text-meta text-muted-foreground" data-testid="template-variables-none">
        This template takes no variables.
      </p>
    );
  }

  return (
    <section className="flex min-w-0 flex-col gap-3" data-testid="template-variables">
      <p className="text-meta text-muted-foreground">
        From the latest revision, <span className="font-mono text-code-foreground">{shortCommitSHA(latest.resolved_commit_sha)}</span>. A required
        variable needs a value when the template is added to a stack.
      </p>
      {/* Fixed columns in a frame that scrolls sideways on a phone. */}
      <div className="overflow-x-auto rounded-lg border">
        <Table className="min-w-2xl">
          <colgroup>
            <col className="w-56" />
            <col className="w-34" />
            <col className="w-26" />
            <col />
          </colgroup>
          <TableHeader>
            <TableRow className="border-divider hover:bg-transparent">
              {["Name", "Type", "Value", "Description"].map((heading) => (
                <TableHead key={heading} scope="col" className={cn(cellClass, "h-10 text-xs font-medium text-muted-foreground")}>
                  {heading}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {variables.map((templateVariable) => (
              <TableRow key={templateVariable.name} className="h-12 border-divider hover:bg-transparent" data-testid={`template-variable-${templateVariable.name}`}>
                <TableCell className={cn(cellClass, "font-mono text-meta whitespace-normal wrap-anywhere")}>{templateVariable.name}</TableCell>
                <TableCell className={cn(cellClass, "font-mono text-xs whitespace-normal text-muted-foreground wrap-anywhere")}>
                  {templateVariable.type_expression || "any"}
                </TableCell>
                <TableCell className={cn(cellClass, "text-meta", templateVariable.required ? "font-medium text-foreground" : "text-muted-foreground")}>
                  {templateVariable.required ? "required" : "optional"}
                </TableCell>
                <TableCell className={cn(cellClass, "text-meta whitespace-normal text-muted-foreground")}>{templateVariable.description}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  );
}

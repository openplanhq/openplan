import { useOutletContext } from "react-router-dom";

/** What the templates page hands the routes drawn into its panel. */
export interface TemplatesPageOutletContext {
  /** The template /templates itself shows: the first in the list, or none. */
  indexTemplateId: string | null;
}

export function useIndexTemplateId(): string | null {
  return useOutletContext<TemplatesPageOutletContext | undefined>()?.indexTemplateId ?? null;
}

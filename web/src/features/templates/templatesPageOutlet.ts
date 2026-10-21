import { useOutletContext } from "react-router-dom";

/** What the templates page hands the routes drawn into its panel. */
export interface TemplatesPageOutletContext {
  /** The template /templates itself shows: the first in the list, or none. */
  indexTemplateId: string | null;
}

export function useIndexTemplateId(): string | null {
  return useOutletContext<TemplatesPageOutletContext | undefined>()?.indexTemplateId ?? null;
}

/**
 * What a move to /templates carries: after a delete, the name of the
 * template that is gone, so the page can say so; the panel that asked for it
 * is gone with it.
 */
export interface TemplatesLocationState {
  deletedTemplate?: string;
}

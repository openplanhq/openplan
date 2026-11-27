import { useOutletContext } from "react-router-dom";

/** What a list page (stacks, templates) hands the routes drawn into its panel. */
export interface IndexOutletContext {
  /** The item the page's own path shows: picked when it opened, or the first
      in the list, or none. */
  indexTemplateId: string | null;
}

export function useIndexTemplateId(): string | null {
  return useOutletContext<IndexOutletContext | undefined>()?.indexTemplateId ?? null;
}

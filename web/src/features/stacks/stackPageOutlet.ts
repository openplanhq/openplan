import { useOutletContext } from "react-router-dom";

/** What the stack's page hands the routes drawn into its panel. */
export interface StackPageOutletContext {
  /** The template the stack's own path shows, picked when it opened. */
  indexTemplateId: string | null;
}

export function useIndexTemplateId(): string | null {
  return useOutletContext<StackPageOutletContext | undefined>()?.indexTemplateId ?? null;
}

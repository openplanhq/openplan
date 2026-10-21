import { createContext, useContext } from "react";
import type { Template, TemplateRevision } from "../../api/types";

export interface TemplateContextValue {
  template: Template;
  /** Every revision of the template, newest first. */
  revisions: TemplateRevision[];
  /** The template's latest revision, which names and describes it. */
  latest: TemplateRevision;
}

// The template a panel is about, for the tabs and controls inside it. A React
// context rather than the outlet's: /templates itself draws the first
// template's Variables tab without a route of its own.
export const TemplateContext = createContext<TemplateContextValue | null>(null);

export function useTemplate(): TemplateContextValue {
  const value = useContext(TemplateContext);
  if (value === null) {
    throw new Error("useTemplate must be used inside a TemplatePanel");
  }
  return value;
}

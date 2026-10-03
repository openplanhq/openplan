import { createContext, useContext } from "react";
import type { StackTemplate } from "../../api/types";

export interface StackTemplateContextValue {
  stackId: string;
  stackTemplate: StackTemplate;
}

// The template a panel is about, for the tabs inside it. A React context
// rather than the outlet's: the stack's own index draws the default
// template's Runs tab without a route of its own, so there is no outlet there
// to carry it.
export const StackTemplateContext = createContext<StackTemplateContextValue | null>(null);

export function useStackTemplate(): StackTemplateContextValue {
  const value = useContext(StackTemplateContext);
  if (value === null) {
    throw new Error("useStackTemplate must be used inside a TemplatePanel");
  }
  return value;
}

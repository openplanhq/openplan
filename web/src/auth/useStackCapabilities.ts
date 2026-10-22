import { useStackQuery } from "../api/queries";
import { useTenantID } from "./useTenantID";
import type { StackCapabilities } from "./types";

export function useStackCapabilities(stackId: string): StackCapabilities | undefined {
  const tenantID = useTenantID();
  return useStackQuery(tenantID, stackId).data?.stack.effective_capabilities;
}

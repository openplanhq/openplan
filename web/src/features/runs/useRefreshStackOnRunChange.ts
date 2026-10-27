import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { isTerminalRunStatus } from "../../api/polling";
import { queryKeys } from "../../api/queryKeys";
import { useTemplateRunsQuery } from "../../api/queries";
import { useTenantID } from "../../auth/useTenantID";

// plan_state, live_state and the pending plan live on the stack template, but
// what changes them is a run finishing or starting to wait, and only the runs
// query polls. Without this the stack's page would keep the states it loaded:
// the panel's header and the list's chips. The panel calls it, so it runs on
// every tab.
//
// The attention list goes with it: it polls only every 30 s, so a plan that
// just failed or started waiting would otherwise reach the template's status
// and Needs attention that much later than the run itself.
export function useRefreshStackOnRunChange(stackId: string, stackTemplateId: string): void {
  const tenantID = useTenantID();
  const queryClient = useQueryClient();
  const runsQuery = useTemplateRunsQuery(tenantID, stackTemplateId);
  const latestRun = runsQuery.status === "success" ? runsQuery.data?.[0] ?? null : null;
  const settledRun = latestRun && isTerminalRunStatus(latestRun.status) ? `${latestRun.id}:${latestRun.status}` : "";
  const waitingRun = latestRun?.status === "waiting_approval" ? latestRun.id : "";

  useEffect(() => {
    if (settledRun === "" && waitingRun === "") {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackId) });
    void queryClient.invalidateQueries({ queryKey: queryKeys.attention(tenantID) });
  }, [settledRun, waitingRun, stackId, tenantID, queryClient]);
}

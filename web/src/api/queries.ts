import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as client from "./client";
import { queryKeys } from "./queryKeys";
import { isTerminalRegistrationStatus, isTerminalRunStatus } from "./polling";
import type { StackView, TemplateRegistration, TemplateRegistrationStatus, TemplateRun, TemplateRunStatus } from "./types";

export function useStacksQuery(tenantID: string) {
  return useQuery({
    queryKey: queryKeys.stacks(tenantID),
    queryFn: () => client.listStacks(tenantID)
  });
}

// A plan reaches waiting_approval, or a destroy fails or resumes, in the
// executor, with nothing here to invalidate the list, so it is polled; slowly,
// since it looks across every stack.
export const ATTENTION_POLL_INTERVAL_MS = 30_000;

export function useAttentionQuery(tenantID: string) {
  return useQuery({
    queryKey: queryKeys.attention(tenantID),
    queryFn: () => client.listAttention(tenantID),
    refetchInterval: ATTENTION_POLL_INTERVAL_MS
  });
}

export function useTemplateRevisionsQuery(tenantID: string) {
  return useQuery({
    queryKey: queryKeys.templateRevisions(tenantID),
    queryFn: () => client.listTemplateRevisions(tenantID)
  });
}

export function useStackQuery(tenantID: string, stackID: string) {
  return useQuery({
    queryKey: queryKeys.stack(tenantID, stackID),
    queryFn: () => client.getStack(tenantID, stackID),
    enabled: stackID !== ""
  });
}

/** Loads write-only metadata for credentials owned by a Stack. */
export function useStackCredentialsQuery(tenantID: string, stackID: string) {
  return useQuery({ queryKey: queryKeys.stackCredentials(tenantID, stackID), queryFn: () => client.listStackCredentials(tenantID, stackID), enabled: stackID !== "" });
}

/** Loads write-only metadata for credentials owned by a StackTemplate. */
export function useStackTemplateCredentialsQuery(tenantID: string, stackTemplateID: string) {
  return useQuery({ queryKey: queryKeys.stackTemplateCredentials(tenantID, stackTemplateID), queryFn: () => client.listStackTemplateCredentials(tenantID, stackTemplateID), enabled: stackTemplateID !== "" });
}

/** Creates a Stack credential and refreshes its metadata list. */
export function useCreateStackCredentialMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: (body: Parameters<typeof client.createStackCredential>[2]) => client.createStackCredential(tenantID, stackID, body), onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.stackCredentials(tenantID, stackID) }) });
}

/** Deletes a Stack credential and refreshes its metadata list. */
export function useDeleteStackCredentialMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: (credentialID: string) => client.deleteStackCredential(tenantID, stackID, credentialID), onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.stackCredentials(tenantID, stackID) }) });
}

/** Creates a StackTemplate credential and refreshes its metadata list. */
export function useCreateStackTemplateCredentialMutation(tenantID: string, stackTemplateID: string) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: (body: Parameters<typeof client.createStackTemplateCredential>[2]) => client.createStackTemplateCredential(tenantID, stackTemplateID, body), onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.stackTemplateCredentials(tenantID, stackTemplateID) }) });
}

/** Deletes a StackTemplate credential and refreshes its metadata list. */
export function useDeleteStackTemplateCredentialMutation(tenantID: string, stackTemplateID: string) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: (credentialID: string) => client.deleteStackTemplateCredential(tenantID, stackTemplateID, credentialID), onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.stackTemplateCredentials(tenantID, stackTemplateID) }) });
}

export function useTemplateRevisionVariablesQuery(tenantID: string, templateRevisionID: string) {
  return useQuery({
    queryKey: queryKeys.templateRevisionVariables(tenantID, templateRevisionID),
    queryFn: () => client.getTemplateRevisionVariables(tenantID, templateRevisionID),
    enabled: templateRevisionID !== "",
    placeholderData: keepPreviousData
  });
}

export const POLL_INTERVAL_MS = 1500;

export function registrationRefetchInterval(status: TemplateRegistrationStatus | undefined): number | false {
  return status && isTerminalRegistrationStatus(status) ? false : POLL_INTERVAL_MS;
}

export function runRefetchInterval(status: TemplateRunStatus | undefined, poll: boolean): number | false {
  if (!poll) {
    return false;
  }
  return status && isTerminalRunStatus(status) ? false : POLL_INTERVAL_MS;
}

export function useTemplateRegistrationQuery(tenantID: string, registrationID: string) {
  return useQuery({
    queryKey: queryKeys.templateRegistration(tenantID, registrationID),
    queryFn: () => client.getTemplateRegistration(tenantID, registrationID),
    enabled: registrationID !== "",
    refetchInterval: (query: { state: { data?: TemplateRegistration } }) =>
      registrationRefetchInterval(query.state.data?.status)
  });
}

export function useTemplateRunQuery(tenantID: string, runID: string, options: { poll: boolean }) {
  return useQuery({
    queryKey: queryKeys.templateRun(tenantID, runID),
    queryFn: () => client.getTemplateRun(tenantID, runID),
    enabled: runID !== "",
    refetchInterval: (query: { state: { data?: TemplateRun } }) =>
      runRefetchInterval(query.state.data?.status, options.poll)
  });
}

export function useTemplateRunsQuery(tenantID: string, stackTemplateID: string) {
  return useQuery({
    queryKey: queryKeys.templateRuns(tenantID, stackTemplateID),
    queryFn: () => client.listTemplateRuns(tenantID, stackTemplateID),
    enabled: stackTemplateID !== "",
    refetchInterval: POLL_INTERVAL_MS
  });
}

// A run's log list refetches whenever the run moves. The previous list stays
// on screen while it does, but only for the same run: another run's phases
// would be requested under this run's id.
export function useTemplateRunLogsQuery(tenantID: string, runID: string, statusTag: string) {
  return useQuery({
    queryKey: queryKeys.templateRunLogs(tenantID, runID, statusTag),
    queryFn: () => client.listTemplateRunLogs(tenantID, runID),
    enabled: runID !== "",
    placeholderData: (previous, previousQuery) => (previousQuery?.queryKey[2] === runID ? previous : undefined)
  });
}

// A phase's log is recorded once its command exits and is replaced only by a
// new upload, which moves uploadedAt. So one upload's body never changes, and
// is fetched once.
export function useTemplateRunLogQuery(tenantID: string, runID: string, phase: string, uploadedAt: string) {
  return useQuery({
    queryKey: queryKeys.templateRunLog(tenantID, runID, phase, uploadedAt),
    queryFn: () => client.getTemplateRunLog(tenantID, runID, phase),
    enabled: runID !== "" && phase !== "",
    staleTime: Infinity,
    placeholderData: keepPreviousData
  });
}

export function useRegisterTemplateMutation(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Parameters<typeof client.registerTemplate>[1]) => client.registerTemplate(tenantID, body),
    onSuccess: (registration) => {
      queryClient.setQueryData(queryKeys.templateRegistration(tenantID, registration.id), registration);
    }
  });
}

export function useCreateStackMutation(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Parameters<typeof client.createStack>[1]) => client.createStack(tenantID, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.stacks(tenantID) });
    }
  });
}

export function useAddTemplateToStackMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Parameters<typeof client.addTemplateToStack>[2]) => client.addTemplateToStack(tenantID, stackID, body),
    onSuccess: (installed) => {
      // The new template's panel opens as soon as this resolves, before the
      // refetch below lands. Without the template in the cached stack, the
      // panel would say it is not installed until then.
      queryClient.setQueryData<StackView>(queryKeys.stack(tenantID, stackID), (view) =>
        view && !view.templates.some((template) => template.id === installed.id) ? { ...view, templates: [...view.templates, installed] } : view
      );
      queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackID) });
      // The stacks index counts each stack's templates.
      queryClient.invalidateQueries({ queryKey: queryKeys.stacks(tenantID) });
    }
  });
}

export function useUpdateStackTemplateConfigMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (variables: { stackTemplateID: string; body: Parameters<typeof client.updateStackTemplateConfig>[2] }) =>
      client.updateStackTemplateConfig(tenantID, variables.stackTemplateID, variables.body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackID) });
    }
  });
}

export function useUpgradeStackTemplateMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (variables: { stackTemplateID: string; body: Parameters<typeof client.upgradeStackTemplate>[2] }) =>
      client.upgradeStackTemplate(tenantID, variables.stackTemplateID, variables.body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackID) });
    }
  });
}

export function useStartTemplateRunMutation(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (variables: { stackTemplateID: string; body: Parameters<typeof client.startTemplateRun>[2] }) =>
      client.startTemplateRun(tenantID, variables.stackTemplateID, variables.body),
    onSuccess: (run) => {
      queryClient.setQueryData(queryKeys.templateRun(tenantID, run.id), run);
    }
  });
}

export function useApproveRunMutation(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (runID: string) => client.approveRun(tenantID, runID),
    onSuccess: (_data, runID) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.templateRun(tenantID, runID) });
      queryClient.invalidateQueries({ queryKey: queryKeys.attention(tenantID) });
    }
  });
}

export function useDiscardRunMutation(tenantID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (variables: { runID: string; body: Parameters<typeof client.discardRun>[2] }) =>
      client.discardRun(tenantID, variables.runID, variables.body),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.templateRun(tenantID, variables.runID) });
      queryClient.invalidateQueries({ queryKey: queryKeys.attention(tenantID) });
    }
  });
}

export function useStackGrantsQuery(tenantID: string, stackID: string) {
  return useQuery({
    queryKey: queryKeys.stackGrants(tenantID, stackID),
    queryFn: () => client.listStackGrants(tenantID, stackID),
    enabled: stackID !== ""
  });
}

export function useSearchUsersQuery(tenantID: string, query: string) {
  return useQuery({
    queryKey: queryKeys.userSearch(tenantID, query),
    queryFn: () => client.searchUsers(tenantID, query, 0, 20),
    enabled: query.length >= 2,
    // The last answer stays while the next query loads, so the stack-access
    // search list doesn't close, and blink, on every keystroke. Only for a
    // query that narrows it: an unrelated one would show the wrong people.
    placeholderData: (previous, previousQuery) => {
      const previousSearch = previousQuery?.queryKey[2];
      return previousSearch !== undefined && query.startsWith(previousSearch) ? previous : undefined;
    }
  });
}

export function useAssignStackRoleMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: Parameters<typeof client.assignStackRole>[2]) => client.assignStackRole(tenantID, stackID, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.stackGrants(tenantID, stackID) });
    }
  });
}

export function useRevokeStackRoleMutation(tenantID: string, stackID: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userSub: string) => client.revokeStackRole(tenantID, stackID, userSub),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.stackGrants(tenantID, stackID) });
    }
  });
}

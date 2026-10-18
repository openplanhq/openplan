import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { InfiniteData, QueryClient } from "@tanstack/react-query";
import * as client from "./client";
import { queryKeys } from "./queryKeys";
import { isTerminalRegistrationStatus, isTerminalRunStatus } from "./polling";
import type { ListPage, TemplateRegistration, TemplateRegistrationStatus, TemplateRun, TemplateRunStatus } from "./types";

// What getNextPageParam gives back: the next page's cursor, or undefined on
// the last page, which is how TanStack Query knows there is no next page.
function nextCursor<T>(page: ListPage<T>): string | undefined {
  return page.next_cursor ?? undefined;
}

/**
 * The stacks the caller can view, a page at a time, newest first. q filters
 * on the server, so a stack beyond the loaded pages is still found. Each q is
 * its own list; the previous one stays on screen while the next loads.
 */
export function useStacksQuery(tenantID: string, q = "") {
  return useInfiniteQuery({
    queryKey: queryKeys.stackList(tenantID, q),
    queryFn: ({ pageParam }) => client.listStacks(tenantID, { cursor: pageParam, q }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: nextCursor,
    select: (data) => data.pages.flatMap((page) => page.items),
    placeholderData: keepPreviousData
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

// Once every run in a list has settled, nothing on screen can move. Runs this
// page starts invalidate the list themselves, so the slow poll only has to
// catch a run started from another tab or by someone else.
export const SETTLED_RUNS_POLL_INTERVAL_MS = 30_000;

// A runs list polls fast while a run in it is moving, and slowly once all of
// them have settled. It stops while its first load is failing: a poll starts a
// fresh fetch, and a fresh fetch of a query with no data puts it back to
// pending, so a list that cannot load would swing between "loading" and
// "failed" for as long as the page is open. Once it has loaded, a failed poll
// keeps polling under the runs already on screen.
export function runsRefetchInterval(state: { data?: TemplateRun[]; fetchFailureCount: number }): number | false {
  // (state.data is every run loaded so far, across the pages read.)
  if (state.data === undefined) {
    return state.fetchFailureCount > 0 ? false : POLL_INTERVAL_MS;
  }
  return state.data.some(isMovingRun) ? POLL_INTERVAL_MS : SETTLED_RUNS_POLL_INTERVAL_MS;
}

// A plan waiting for approval counts as settled here: it can wait for days,
// and moves only when someone approves or discards it on the run's own page.
function isMovingRun(run: TemplateRun): boolean {
  return run.status !== "waiting_approval" && !isTerminalRunStatus(run.status);
}

type RunPages = InfiniteData<ListPage<TemplateRun>, string | undefined>;

function runsOf(data: RunPages | undefined): TemplateRun[] | undefined {
  return data?.pages.flatMap((page) => page.items);
}

/**
 * A template's runs, newest first: the first page, and older pages as the
 * history asks for them. Every reader shares the one list. Lock state,
 * notices and refresh need only the first page: at most one run is in flight
 * per template, and it is always the newest.
 *
 * A poll refetches each loaded page in turn from the first, so a run started
 * since shifts the pages rather than leaving a gap between them.
 */
export function useTemplateRunsQuery(tenantID: string, stackTemplateID: string) {
  return useInfiniteQuery({
    queryKey: queryKeys.templateRuns(tenantID, stackTemplateID),
    queryFn: ({ pageParam }) => client.listTemplateRuns(tenantID, stackTemplateID, { cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: nextCursor,
    select: runsOf,
    enabled: stackTemplateID !== "",
    refetchInterval: (query) => runsRefetchInterval({ data: runsOf(query.state.data), fetchFailureCount: query.state.fetchFailureCount })
  });
}

/** The runs in the cache right now, for a check made at click time. */
export function cachedTemplateRuns(queryClient: QueryClient, tenantID: string, stackTemplateID: string): TemplateRun[] | undefined {
  return runsOf(queryClient.getQueryData<RunPages>(queryKeys.templateRuns(tenantID, stackTemplateID)));
}

/**
 * One run by its number, for a run's page when the loaded runs don't hold it.
 * Only its id is used from here on: a number names one run for good, so this
 * is read once, and the run itself is followed by id.
 */
export function useTemplateRunByNumberQuery(tenantID: string, stackTemplateID: string, runNumber: string, options: { enabled: boolean }) {
  return useQuery({
    queryKey: queryKeys.templateRunByNumber(tenantID, stackTemplateID, runNumber),
    queryFn: () => client.getTemplateRunByNumber(tenantID, stackTemplateID, runNumber),
    enabled: options.enabled && stackTemplateID !== "" && runNumber !== "",
    staleTime: Infinity
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

// A log is recorded once its command exits and is replaced only by a new
// upload, which moves uploadedAt. So one upload's body never changes, and is
// fetched once.
export function useTemplateRunLogQuery(tenantID: string, runID: string, name: string, uploadedAt: string) {
  return useQuery({
    queryKey: queryKeys.templateRunLog(tenantID, runID, name, uploadedAt),
    queryFn: () => client.getTemplateRunLog(tenantID, runID, name),
    enabled: runID !== "" && name !== "",
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
    onSuccess: async () => {
      // The stacks index counts each stack's templates.
      void queryClient.invalidateQueries({ queryKey: queryKeys.stacks(tenantID) });
      // The new template's panel opens as soon as this resolves, so wait for
      // the stack to reload with the template in it. Without it the panel
      // would say the template is not installed. The add's own response
      // cannot stand in: it has no display name or ref, so the panel would
      // show the workspace name until the reload landed.
      await queryClient.invalidateQueries({ queryKey: queryKeys.stack(tenantID, stackID) });
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
    onSuccess: (run) => {
      // The approval answers with the run, so the screen moves off "waiting
      // for approval" without reading it again.
      queryClient.setQueryData(queryKeys.templateRun(tenantID, run.id), run);
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

export function useSearchUsersQuery(tenantID: string, stackID: string, query: string) {
  return useQuery({
    queryKey: queryKeys.userSearch(tenantID, stackID, query),
    queryFn: async () => (await client.searchUsers(tenantID, stackID, query, { limit: 20 })).items,
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
    mutationFn: ({ userSub, role }: { userSub: string; role: string }) => client.assignStackRole(tenantID, stackID, userSub, { role }),
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

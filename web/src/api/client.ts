import { loginLoopDetected } from "../auth/loginAttempts";
import type { Me } from "../auth/types";
import type {
  ApiProblem,
  AttentionItem,
  CredentialMetadata,
  GrantView,
  ListPage,
  Operation,
  PlanResourceChange,
  ProblemFieldError,
  Stack,
  StackListItem,
  StackTemplate,
  StackView,
  Template,
  TemplateRevision,
  TemplateRun,
  TemplateRunLog,
  TemplateVariable,
  UserProfile
} from "./types";

// A failed request, carrying the server's problem document. message is the
// problem's detail, so a screen that only shows text still says something
// specific; a screen that acts on the failure reads type instead.
export class ApiRequestError extends Error {
  readonly type: string;
  readonly status: number;
  readonly title: string;
  readonly detail: string;
  readonly errors: ProblemFieldError[];
  readonly requestId: string;

  constructor(problem: ApiProblem) {
    super(problem.detail || problem.title);
    this.name = "ApiRequestError";
    this.type = problem.type;
    this.status = problem.status;
    this.title = problem.title;
    this.detail = problem.detail ?? "";
    this.errors = problem.errors ?? [];
    this.requestId = problem.request_id ?? "";
  }
}

interface RegisterTemplateRequest {
  repo_owner: string;
  repo_name: string;
  source_ref: string;
  root_path: string;
}

interface CreateStackRequest {
  name: string;
  slug: string;
  tags: Record<string, string>;
  default_credential_ids: string[];
}

interface AddTemplateToStackRequest {
  template_revision_id: string;
  component_key?: string;
  config: Record<string, unknown>;
}

interface UpdateStackTemplateConfigRequest {
  config: Record<string, unknown>;
}

interface UpgradeStackTemplateRequest {
  target_template_revision_id: string;
  config?: Record<string, unknown>;
}

interface StartRunRequest {
  operation: Operation;
  // Applies straight away, with no saved plan. Only an apply run takes it,
  // and it needs approve access on the stack.
  auto_approve?: boolean;
}

interface CredentialRequest {
  name: string;
  value: string;
}

interface DiscardRunRequest {
  reason: string;
}

export function getMe(): Promise<Me> {
  return requestJSON("/v1/me");
}

function templatePath(tenantID: string, templateID: string): string {
  return `/v1/tenants/${encodeURIComponent(tenantID)}/templates/${encodeURIComponent(templateID)}`;
}

// Registering answers with the template, whose latest_sync is the sync it
// queued. A source registered already answers with its template, syncing
// again.
export function registerTemplate(tenantID: string, body: RegisterTemplateRequest): Promise<Template> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/templates`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

export function getTemplate(tenantID: string, templateID: string): Promise<Template> {
  return requestJSON(templatePath(tenantID, templateID));
}

// Answers with the template, whose latest_sync is the sync just queued.
export function syncTemplate(tenantID: string, templateID: string): Promise<Template> {
  return requestJSON(`${templatePath(tenantID, templateID)}/syncs`, {
    method: "POST",
    body: JSON.stringify({})
  });
}

// Refused with template-in-use while any stack uses the template.
export function deleteTemplate(tenantID: string, templateID: string): Promise<void> {
  return requestNoContent(templatePath(tenantID, templateID), { method: "DELETE" });
}

export async function getTemplateRevisionVariables(tenantID: string, templateRevisionID: string): Promise<TemplateVariable[]> {
  return listItems<TemplateVariable>(
    `/v1/tenants/${encodeURIComponent(tenantID)}/template-revisions/${encodeURIComponent(templateRevisionID)}/variables`
  );
}

// Where a list starts and how much of it to read. cursor is the previous
// page's next_cursor; without one, the first page.
export interface PageOptions {
  cursor?: string;
  limit?: number;
}

function pageQuery(options: PageOptions & { q?: string }): string {
  const params = new URLSearchParams();
  if (options.q) {
    params.set("q", options.q);
  }
  if (options.limit !== undefined) {
    params.set("limit", String(options.limit));
  }
  if (options.cursor) {
    params.set("cursor", options.cursor);
  }
  const query = params.toString();
  return query === "" ? "" : `?${query}`;
}

// The largest page the API serves, for a caller that reads every page.
const MAX_PAGE_LIMIT = 100;

/** Follows next_cursor from the first page to the last and returns every item. */
export async function listAll<T>(fetchPage: (cursor: string | undefined) => Promise<ListPage<T>>): Promise<T[]> {
  const items: T[] = [];
  let cursor: string | undefined;
  do {
    const page = await fetchPage(cursor);
    items.push(...page.items);
    cursor = page.next_cursor ?? undefined;
  } while (cursor !== undefined);
  return items;
}

// A list that is always one page: its items, never null.
async function listItems<T>(path: string): Promise<T[]> {
  const page = await requestJSON<ListPage<T>>(path);
  return page.items ?? [];
}

// Every template, read a page at a time: the registry sorts them by name
// and filters them as someone types, which needs the whole set.
export function listTemplates(tenantID: string): Promise<Template[]> {
  return listAll((cursor) =>
    requestJSON<ListPage<Template>>(`/v1/tenants/${encodeURIComponent(tenantID)}/templates${pageQuery({ cursor, limit: MAX_PAGE_LIMIT })}`)
  );
}

// Every revision of one template, newest first, read a page at a time.
export function listTemplateRevisions(tenantID: string, templateID: string): Promise<TemplateRevision[]> {
  return listAll((cursor) =>
    requestJSON<ListPage<TemplateRevision>>(`${templatePath(tenantID, templateID)}/revisions${pageQuery({ cursor, limit: MAX_PAGE_LIMIT })}`)
  );
}

export function createStack(tenantID: string, body: CreateStackRequest): Promise<Stack> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

/** One page of the stacks the caller can view, newest first; q matches name or slug. */
export function listStacks(tenantID: string, options: PageOptions & { q?: string } = {}): Promise<ListPage<StackListItem>> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks${pageQuery(options)}`);
}

/** Lists plans waiting for approval and failed destroys on every visible stack. */
export function listAttention(tenantID: string): Promise<AttentionItem[]> {
  return listItems(`/v1/tenants/${encodeURIComponent(tenantID)}/attention`);
}

export function getStack(tenantID: string, stackID: string): Promise<StackView> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}`);
}

/** Lists Stack credential names and metadata; secret values are never returned. */
export function listStackCredentials(tenantID: string, stackID: string): Promise<CredentialMetadata[]> {
  return listItems(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/credentials`);
}

/** Stores one Stack-scoped credential and returns write-only metadata. */
export function createStackCredential(tenantID: string, stackID: string, body: CredentialRequest): Promise<CredentialMetadata> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/credentials`, { method: "POST", body: JSON.stringify(body) });
}

/** Deletes one Stack-scoped credential. */
export function deleteStackCredential(tenantID: string, stackID: string, credentialID: string): Promise<void> {
  return requestNoContent(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/credentials/${encodeURIComponent(credentialID)}`, { method: "DELETE" });
}

/** Lists StackTemplate credential names and metadata without secret values. */
export function listStackTemplateCredentials(tenantID: string, stackTemplateID: string): Promise<CredentialMetadata[]> {
  return listItems(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/credentials`);
}

/** Stores one StackTemplate-scoped credential and returns write-only metadata. */
export function createStackTemplateCredential(tenantID: string, stackTemplateID: string, body: CredentialRequest): Promise<CredentialMetadata> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/credentials`, { method: "POST", body: JSON.stringify(body) });
}

/** Deletes one StackTemplate-scoped credential. */
export function deleteStackTemplateCredential(tenantID: string, stackTemplateID: string, credentialID: string): Promise<void> {
  return requestNoContent(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/credentials/${encodeURIComponent(credentialID)}`, { method: "DELETE" });
}

export function addTemplateToStack(tenantID: string, stackID: string, body: AddTemplateToStackRequest): Promise<StackTemplate> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/templates`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

export function updateStackTemplateConfig(tenantID: string, stackTemplateID: string, body: UpdateStackTemplateConfigRequest): Promise<StackTemplate> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/config`, {
    method: "PUT",
    body: JSON.stringify(body)
  });
}

export function upgradeStackTemplate(tenantID: string, stackTemplateID: string, body: UpgradeStackTemplateRequest): Promise<StackTemplate> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/upgrade`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

export function startTemplateRun(tenantID: string, stackTemplateID: string, body: StartRunRequest): Promise<TemplateRun> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/runs`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

export function getTemplateRun(tenantID: string, runID: string): Promise<TemplateRun> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}`);
}

/** One page of a template's runs, newest first. */
export function listTemplateRuns(tenantID: string, stackTemplateID: string, options: PageOptions = {}): Promise<ListPage<TemplateRun>> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/runs${pageQuery(options)}`);
}

// A run's page is addressed by its number, which the runs list may not reach
// on its first page, so the run is read by number rather than found in it.
export function getTemplateRunByNumber(tenantID: string, stackTemplateID: string, runNumber: string): Promise<TemplateRun> {
  return requestJSON(
    `/v1/tenants/${encodeURIComponent(tenantID)}/stack-templates/${encodeURIComponent(stackTemplateID)}/runs/${encodeURIComponent(runNumber)}`
  );
}

// A run's plan's resource changes, a page at a time, removals first. One
// run lists at most 1,000, so a caller can read them all with listAll.
export function listTemplateRunChanges(tenantID: string, runID: string, options: PageOptions = {}): Promise<ListPage<PlanResourceChange>> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}/changes${pageQuery(options)}`);
}

export function listTemplateRunLogs(tenantID: string, runID: string): Promise<TemplateRunLog[]> {
  return listItems(`/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}/logs`);
}

// name is the log's name from the run's log list, such as "plan-1", not a
// run phase: one phase can write several logs.
export function getTemplateRunLog(tenantID: string, runID: string, name: string): Promise<string> {
  return requestText(`/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}/logs/${encodeURIComponent(name)}`);
}

// A live log's server-sent events, for an EventSource rather than fetch: the
// browser holds the stream open and reconnects it on its own.
export function templateRunLogEventsURL(tenantID: string, runID: string, name: string): string {
  return `/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}/logs/${encodeURIComponent(name)}/events`;
}

// approveRun answers with the run, already approved, while its apply waits for
// an executor.
export function approveRun(tenantID: string, runID: string): Promise<TemplateRun> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}/approve`, {
    method: "POST",
    body: JSON.stringify({})
  });
}

// discardRun throws away a plan waiting for approval. A run that is planning
// or applying cannot be stopped.
export function discardRun(tenantID: string, runID: string, body: DiscardRunRequest): Promise<void> {
  return requestNoContent(`/v1/tenants/${encodeURIComponent(tenantID)}/template-runs/${encodeURIComponent(runID)}/discard`, {
    method: "POST",
    body: JSON.stringify(body)
  });
}

interface AssignStackRoleBody {
  role: string;
}

export function listStackGrants(tenantID: string, stackID: string): Promise<GrantView[]> {
  return listItems(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/grants`);
}

// assignStackRole sets userSub's role on the stack, creating the grant or
// replacing the role they hold.
export function assignStackRole(tenantID: string, stackID: string, userSub: string, body: AssignStackRoleBody): Promise<GrantView> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/grants/${encodeURIComponent(userSub)}`, {
    method: "PUT",
    body: JSON.stringify(body)
  });
}

export function revokeStackRole(tenantID: string, stackID: string, userSub: string): Promise<void> {
  return requestNoContent(`/v1/tenants/${encodeURIComponent(tenantID)}/stacks/${encodeURIComponent(stackID)}/grants/${encodeURIComponent(userSub)}`, {
    method: "DELETE"
  });
}

// Who holds a platform role. Root isn't among them: the server sets it from
// OPENPLAN_ROOT_SUBJECT and never lists it as a grant.
export function listPlatformGrants(tenantID: string): Promise<GrantView[]> {
  return listItems(`/v1/tenants/${encodeURIComponent(tenantID)}/platform/grants`);
}

// assignPlatformRole sets userSub's platform role, creating the grant or
// replacing the role they hold, as assignStackRole does for a stack.
export function assignPlatformRole(tenantID: string, userSub: string, body: AssignStackRoleBody): Promise<GrantView> {
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/platform/grants/${encodeURIComponent(userSub)}`, {
    method: "PUT",
    body: JSON.stringify(body)
  });
}

export function revokePlatformRole(tenantID: string, userSub: string): Promise<void> {
  return requestNoContent(`/v1/tenants/${encodeURIComponent(tenantID)}/platform/grants/${encodeURIComponent(userSub)}`, {
    method: "DELETE"
  });
}

/**
 * One page of the people whose name or email matches q, ordered by name.
 * With a stack, the search is for that stack: naming it is what lets an
 * operator or above, not only a platform administrator, search. Without one
 * it searches the platform, which only people who manage platform access may.
 */
export function searchUsers(tenantID: string, stackID: string, q: string, options: PageOptions = {}): Promise<ListPage<UserProfile>> {
  const query = pageQuery({ ...options, q });
  const stack = stackID === "" ? "" : `${query === "" ? "?" : "&"}stack_id=${encodeURIComponent(stackID)}`;
  return requestJSON(`/v1/tenants/${encodeURIComponent(tenantID)}/users/search${query}${stack}`);
}

// The app's own sign-in screen. Not a server route: it is a page with one
// button that starts the OIDC flow, so a lost session lands somewhere the user
// chooses to leave rather than bouncing straight to the identity provider.
export const signInPath = "/signin";

export function loginURL(): string {
  const returnTo = `${globalThis.location.pathname}${globalThis.location.search}`;
  return `${signInPath}?return_to=${encodeURIComponent(returnTo)}`;
}

// The OIDC authorization request, reached by a full navigation from the
// sign-in button. Never fetch: it redirects to the provider's origin, where an
// XHR dies on CORS.
export function ssoLoginURL(returnTo: string): string {
  return `/v1/auth/login?return_to=${encodeURIComponent(returnTo)}`;
}

// Every request this module makes carries the session cookie, and the server
// slides the session's idle bound on any request that arrives more than a touch
// interval after the last one. SessionProvider counts them so it can tell "the
// bound may have moved since our last snapshot" from "this tab has said
// nothing at all", which is what keeps its expiry timer from renewing an
// unattended tab forever.
let requestsMade = 0;

export function apiRequestsMade(): number {
  return requestsMade;
}

async function fetchWithAuth(path: string, init: RequestInit): Promise<Response> {
  const headers = new Headers(init.headers);
  if (!headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }

  // The session cookie is httpOnly: the browser attaches it and this code
  // cannot read it. There is no bearer header and nothing to renew.
  const response = await fetch(path, { method: "GET", ...init, headers, credentials: "same-origin" });
  // Counted once the server has answered, so the count only ever covers
  // requests that actually reached it and could have slid the bound.
  requestsMade += 1;

  if (
    response.status === 401 &&
    !loginLoopDetected() &&
    // Already there. Navigating again would only reload the screen the user
    // is looking at.
    globalThis.location.pathname !== signInPath
  ) {
    // A full navigation rather than a router push: this runs outside React, and
    // a reload is also what re-reads the session state from scratch. Once the
    // loop guard has tripped the navigation is skipped and the 401 surfaces as
    // an error instead, so the browser stops bouncing and SessionProvider can
    // explain what went wrong.
    globalThis.location.assign(loginURL());
  }

  return response;
}

export function logout(): void {
  // A real form POST, never fetch. The response is a 303 whose Location carries
  // the ID token as id_token_hint; a navigation lets the browser follow it
  // without script ever reading that header. POST rather than a link so a
  // cross-site image tag cannot log the user out.
  const form = document.createElement("form");
  form.method = "POST";
  form.action = "/v1/auth/logout";
  document.body.appendChild(form);
  form.submit();
}

async function requestJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetchWithAuth(path, init);
  await throwForError(response);
  return response.json() as Promise<T>;
}

async function requestText(path: string, init: RequestInit = {}): Promise<string> {
  const response = await fetchWithAuth(path, init);
  await throwForError(response);
  return response.text();
}

async function requestNoContent(path: string, init: RequestInit = {}): Promise<void> {
  const response = await fetchWithAuth(path, init);
  await throwForError(response);
}

async function throwForError(response: Response): Promise<void> {
  if (response.ok) {
    return;
  }

  throw new ApiRequestError(await readProblem(response));
}

// readProblem accepts application/problem+json, which is what the API sends,
// and plain JSON. Anything else, such as a proxy's HTML 502, or a body that
// isn't a problem, becomes an about:blank problem for the status, so the
// caller always gets an ApiRequestError rather than a parse error.
async function readProblem(response: Response): Promise<ApiProblem> {
  const fallback: ApiProblem = { type: "about:blank", title: response.statusText || "Request failed", status: response.status };
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("application/problem+json") && !contentType.includes("application/json")) {
    return fallback;
  }
  try {
    const body = (await response.json()) as Partial<ApiProblem> | null;
    if (body === null || typeof body.type !== "string") {
      return fallback;
    }
    // The response's status wins: it is what actually happened.
    return { ...fallback, ...body, status: response.status } as ApiProblem;
  } catch {
    return fallback;
  }
}

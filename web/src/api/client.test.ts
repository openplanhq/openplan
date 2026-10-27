// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  addTemplateToStack,
  ApiRequestError,
  approveRun,
  assignPlatformRole,
  assignStackRole,
  discardRun,
  createStack,
  createStackCredential,
  createStackTemplateCredential,
  deleteTemplate,
  getTemplate,
  getTemplateRevisionVariables,
  getTemplateRunByNumber,
  getTemplateRunLog,
  listAll,
  listPlatformGrants,
  listStackCredentials,
  listStacks,
  listTemplateRevisions,
  listTemplateRuns,
  listTemplates,
  logout,
  registerTemplate,
  revokePlatformRole,
  searchUsers,
  ssoLoginURL,
  startTemplateRun,
  syncTemplate,
  templateRunLogEventsURL,
  updateStackTemplateConfig,
  changeStackTemplateRevision
} from "./client";
import { isRunInFlightError } from "../features/runs/runErrors";
import { problemTypes } from "./types";

describe("api client", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("registers a template on the tenant's templates", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "tpl_1" }, 202));

    await registerTemplate("tenant_123", {
      repo_owner: "acme",
      repo_name: "infra",
      source_ref: "main",
      root_path: "."
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/templates",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          repo_owner: "acme",
          repo_name: "infra",
          source_ref: "main",
          root_path: "."
        })
      })
    );
  });

  it("posts stack creation with tags", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "stack_123" }));

    await createStack("tenant_123", {
      name: "Acme Prod",
      slug: "",
      tags: { env: "prod" },
      default_credential_ids: []
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stacks",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          name: "Acme Prod",
          slug: "",
          tags: { env: "prod" },
          default_credential_ids: []
        })
      })
    );
  });

  it("lists a page of stacks, asking for the next with the cursor and filter", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [{ id: "stack_1" }], next_cursor: "c2" }))
      .mockResolvedValueOnce(jsonResponse({ items: [{ id: "stack_2" }], next_cursor: null }));

    const first = await listStacks("tenant_123");
    const next = await listStacks("tenant_123", { cursor: "c2", q: "pay ments", limit: 20 });

    expect(first).toEqual({ items: [{ id: "stack_1" }], next_cursor: "c2" });
    expect(next.next_cursor).toBeNull();
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/v1/tenants/tenant_123/stacks", expect.objectContaining({ method: "GET" }));
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/v1/tenants/tenant_123/stacks?q=pay+ments&limit=20&cursor=c2",
      expect.objectContaining({ method: "GET" })
    );
  });

  // The registry sorts and filters every template, and a template's screens
  // read every revision, so the client reads every page, at the largest size
  // the API serves.
  it("reads every page of templates", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [{ id: "tpl_1" }], next_cursor: "c2" }))
      .mockResolvedValueOnce(jsonResponse({ items: [{ id: "tpl_2" }], next_cursor: null }));

    const templates = await listTemplates("tenant_123");

    expect(templates.map((template) => template.id)).toEqual(["tpl_1", "tpl_2"]);
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/v1/tenants/tenant_123/templates?limit=100", expect.anything());
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/v1/tenants/tenant_123/templates?limit=100&cursor=c2", expect.anything());
  });

  it("reads every page of one template's revisions", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ items: [{ id: "rev_1", tags: [] }], next_cursor: "c2" }))
      .mockResolvedValueOnce(jsonResponse({ items: [{ id: "rev_2", tags: [] }], next_cursor: null }));

    const templateRevisions = await listTemplateRevisions("tenant_123", "tpl 1");

    expect(templateRevisions.map((revision) => revision.id)).toEqual(["rev_1", "rev_2"]);
    expect(fetchMock).toHaveBeenNthCalledWith(1, "/v1/tenants/tenant_123/templates/tpl%201/revisions?limit=100", expect.anything());
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/v1/tenants/tenant_123/templates/tpl%201/revisions?limit=100&cursor=c2", expect.anything());
  });

  it("reads, syncs and deletes one template", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ id: "tpl_1" }))
      .mockResolvedValueOnce(jsonResponse({ id: "tpl_1" }, 202))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));

    await getTemplate("tenant_123", "tpl_1");
    await syncTemplate("tenant_123", "tpl_1");
    await deleteTemplate("tenant_123", "tpl_1");

    expect(fetchMock).toHaveBeenNthCalledWith(1, "/v1/tenants/tenant_123/templates/tpl_1", expect.objectContaining({ method: "GET" }));
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/v1/tenants/tenant_123/templates/tpl_1/syncs", expect.objectContaining({ method: "POST", body: "{}" }));
    expect(fetchMock).toHaveBeenNthCalledWith(3, "/v1/tenants/tenant_123/templates/tpl_1", expect.objectContaining({ method: "DELETE" }));
  });

  it("follows next_cursor to the last page", async () => {
    const pages: Record<string, { items: number[]; next_cursor: string | null }> = {
      start: { items: [1, 2], next_cursor: "b" },
      b: { items: [3], next_cursor: "c" },
      c: { items: [], next_cursor: null }
    };
    const asked: (string | undefined)[] = [];

    const items = await listAll(async (cursor) => {
      asked.push(cursor);
      return pages[cursor ?? "start"];
    });

    expect(items).toEqual([1, 2, 3]);
    expect(asked).toEqual([undefined, "b", "c"]);
  });

  // The small lists are one page; the client hands screens their items.
  it("reads a one-page list's items", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ items: [{ name: "region" }], next_cursor: null }));

    expect(await getTemplateRevisionVariables("tenant_123", "rev_1")).toEqual([{ name: "region" }]);
    expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/template-revisions/rev_1/variables", expect.objectContaining({ method: "GET" }));
  });

  it("lists runs for a stack template a page at a time", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse({ items: [{ id: "run_1", status: "waiting_approval" }], next_cursor: null }));

    const runs = await listTemplateRuns("tenant_123", "stpl_1", { cursor: "c2" });

    expect(runs.items).toEqual([{ id: "run_1", status: "waiting_approval" }]);
    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stack-templates/stpl_1/runs?cursor=c2",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("reads a run by its number within the template", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "run_7", run_number: 7 }));

    const run = await getTemplateRunByNumber("tenant_123", "stpl_1", "7");

    expect(run.id).toBe("run_7");
    expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/stack-templates/stpl_1/runs/7", expect.objectContaining({ method: "GET" }));
  });

  it("posts template install config", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ id: "stack_template_123" }));

    await addTemplateToStack("tenant_123", "stack_123", {
      template_revision_id: "template_123",
      config: { region: "us-east-1" }
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stacks/stack_123/templates",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({
          template_revision_id: "template_123",
          config: { region: "us-east-1" }
        })
      })
    );
  });

  it("edits installed stack templates and changes their revision", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValueOnce(jsonResponse({ id: "stack_template_123" }))
      .mockResolvedValueOnce(jsonResponse({ id: "stack_template_123" }));

    await updateStackTemplateConfig("tenant_123", "stack_template_123", {
      config: { region: "us-west-2" }
    });
    await changeStackTemplateRevision("tenant_123", "stack_template_123", {
      target_template_revision_id: "template_rev_2"
    });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/v1/tenants/tenant_123/stack-templates/stack_template_123/config",
      expect.objectContaining({
        // PUT: the config sent replaces the whole stored one.
        method: "PUT",
        body: JSON.stringify({ config: { region: "us-west-2" } })
      })
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/v1/tenants/tenant_123/stack-templates/stack_template_123/change-revision",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ target_template_revision_id: "template_rev_2" })
      })
    );
  });

  it("posts run operations, approvals, and discards", async () => {
    // A fresh response per call: the approval now reads its body too.
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => Promise.resolve(jsonResponse({ id: "run_123" })));

    await startTemplateRun("tenant_123", "stack_template_123", { operation: "apply", auto_approve: true });
    // The approval answers with the run, now approved, for the caller to show.
    await expect(approveRun("tenant_123", "run_123")).resolves.toEqual({ id: "run_123" });
    await discardRun("tenant_123", "run_456", { reason: "manual stop" });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/v1/tenants/tenant_123/stack-templates/stack_template_123/runs",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ operation: "apply", auto_approve: true }) })
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/v1/tenants/tenant_123/template-runs/run_123/approve",
      expect.objectContaining({ method: "POST" })
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      "/v1/tenants/tenant_123/template-runs/run_456/discard",
      expect.objectContaining({ method: "POST" })
    );
    expect(JSON.parse(String(fetchMock.mock.calls[0][1]?.body))).toEqual({ operation: "apply", auto_approve: true });
    expect(JSON.parse(String(fetchMock.mock.calls[1][1]?.body))).toEqual({});
    expect(JSON.parse(String(fetchMock.mock.calls[2][1]?.body))).toEqual({ reason: "manual stop" });
  });

  it("reads log bodies as text", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(textResponse("plan output\n"));

    const body = await getTemplateRunLog("tenant_123", "run_123", "plan-1");

    expect(body).toBe("plan output\n");
    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/template-runs/run_123/logs/plan-1",
      expect.objectContaining({ method: "GET" })
    );
  });

  it("addresses a log's live events beside its body", () => {
    expect(templateRunLogEventsURL("tenant 1", "run/1", "plan-1")).toBe("/v1/tenants/tenant%201/template-runs/run%2F1/logs/plan-1/events");
  });

  // The person is named in the path, so the body carries only the role.
  it("puts a stack role at the person's grant path", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(jsonResponse({ user_sub: "u/3", role: "viewer", display_name: "", email: "" }, 201));

    await assignStackRole("tenant_123", "stack_123", "u/3", { role: "viewer" });

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stacks/stack_123/grants/u%2F3",
      expect.objectContaining({ method: "PUT", body: JSON.stringify({ role: "viewer" }) })
    );
  });

  it("lists, sets and revokes platform roles at the platform's grant paths", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    fetchMock.mockResolvedValueOnce(jsonResponse({ items: [{ user_sub: "u/3", role: "admin", display_name: "Sam", email: "" }], next_cursor: null }));
    fetchMock.mockResolvedValueOnce(jsonResponse({ user_sub: "u/3", role: "editor", display_name: "", email: "" }));
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));

    await expect(listPlatformGrants("tenant_123")).resolves.toEqual([{ user_sub: "u/3", role: "admin", display_name: "Sam", email: "" }]);
    await assignPlatformRole("tenant_123", "u/3", { role: "editor" });
    await revokePlatformRole("tenant_123", "u/3");

    expect(fetchMock.mock.calls[0][0]).toBe("/v1/tenants/tenant_123/platform/grants");
    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/platform/grants/u%2F3",
      expect.objectContaining({ method: "PUT", body: JSON.stringify({ role: "editor" }) })
    );
    expect(fetchMock).toHaveBeenCalledWith("/v1/tenants/tenant_123/platform/grants/u%2F3", expect.objectContaining({ method: "DELETE" }));
  });

  // No stack means the platform search, so no empty stack_id rides along.
  it("searches the platform when no stack is named", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async () => jsonResponse({ items: [], next_cursor: null }));

    await searchUsers("tenant_123", "", "sam", { limit: 20 });
    await searchUsers("tenant_123", "stack_1", "sam", { limit: 20 });

    expect(fetchMock.mock.calls[0][0]).toBe("/v1/tenants/tenant_123/users/search?q=sam&limit=20");
    expect(fetchMock.mock.calls[1][0]).toBe("/v1/tenants/tenant_123/users/search?q=sam&limit=20&stack_id=stack_1");
  });

  // The API serves every error as application/problem+json, which a check
  // for application/json alone does not match.
  it("throws ApiRequestError carrying the problem document", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      problemResponse({
        type: "https://openplan.run/problems/validation-failed",
        title: "The request has invalid fields",
        status: 422,
        detail: "name is required",
        request_id: "req-1",
        errors: [{ pointer: "#/name", issue: "required", detail: "name is required" }]
      })
    );

    const error = await createStack("tenant_123", { name: "", slug: "", tags: {}, default_credential_ids: [] }).catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect(error).toMatchObject({
      type: problemTypes.validationFailed,
      status: 422,
      title: "The request has invalid fields",
      message: "name is required",
      requestId: "req-1",
      errors: [{ pointer: "#/name", issue: "required", detail: "name is required" }]
    });
  });

  it("reads a run-in-flight conflict by its type", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      problemResponse({
        type: "https://openplan.run/problems/run-in-flight",
        title: "Another run is in progress",
        status: 409,
        detail: "Another run on this template is still in progress.",
        request_id: "req-2"
      })
    );

    const error = await listStacks("tenant_123").catch((caught: unknown) => caught);

    expect(isRunInFlightError(error)).toBe(true);
    expect(error).toMatchObject({ status: 409, requestId: "req-2", errors: [] });
  });

  // A proxy in front of the API can answer with its own page; the caller
  // still gets an ApiRequestError for the status, not a JSON parse error.
  it("turns a non-problem error body into an about:blank problem", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("<html>bad gateway</html>", { status: 502, statusText: "Bad Gateway", headers: { "content-type": "text/html" } })
    );

    const error = await listStacks("tenant_123").catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiRequestError);
    expect(error).toMatchObject({ type: "about:blank", status: 502, message: "Bad Gateway", requestId: "" });
  });

  it("turns a JSON error that isn't a problem into an about:blank problem", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ error: "old_shape", message: "old" }, 400));

    const error = await listStacks("tenant_123").catch((caught: unknown) => caught);

    expect(error).toMatchObject({ type: "about:blank", status: 400 });
  });

  // The server reads a missing `sensitive` as true, so the field is always
  // sent: a Variables entry that dropped it would be stored as a secret.
  it("sends sensitive with every new credential, on both scopes", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => Promise.resolve(jsonResponse({ id: "c1" }, 201)));

    await createStackCredential("tenant_123", "stack_1", { name: "AWS_REGION", value: "us-east-1", sensitive: false });
    await createStackTemplateCredential("tenant_123", "st_1", { name: "TOKEN", value: "s3cret", sensitive: true });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      "/v1/tenants/tenant_123/stacks/stack_1/credentials",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "AWS_REGION", value: "us-east-1", sensitive: false }) })
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      "/v1/tenants/tenant_123/stack-templates/st_1/credentials",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ name: "TOKEN", value: "s3cret", sensitive: true }) })
    );
  });

  it("lists credentials with the value of each one that is not sensitive", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({
        items: [
          { id: "c1", name: "AWS_REGION", scope: "stack", stack_id: "stack_1", sensitive: false, value: "us-east-1", created_at: "2026-10-23T00:00:00Z" },
          { id: "c2", name: "TOKEN", scope: "stack", stack_id: "stack_1", sensitive: true, created_at: "2026-10-23T00:00:00Z" }
        ],
        next_cursor: null
      })
    );

    const credentials = await listStackCredentials("tenant_123", "stack_1");

    expect(credentials.map(({ name, sensitive, value }) => ({ name, sensitive, value }))).toEqual([
      { name: "AWS_REGION", sensitive: false, value: "us-east-1" },
      { name: "TOKEN", sensitive: true, value: undefined }
    ]);
  });

  it("never includes actor identity fields in request bodies", async () => {
    const forbiddenFields = ["actor", "actor_id", "sub", "user_id", "on_behalf_of"];

    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(() => Promise.resolve(jsonResponse({ id: "ok" })));

    await registerTemplate("tenant_123", { repo_owner: "a", repo_name: "b", source_ref: "main", root_path: "." });
    await createStack("tenant_123", { name: "s", slug: "", tags: {}, default_credential_ids: [] });
    await addTemplateToStack("tenant_123", "stack_1", { template_revision_id: "rev_1", config: {} });
    await updateStackTemplateConfig("tenant_123", "st_1", { config: {} });
    await changeStackTemplateRevision("tenant_123", "st_1", { target_template_revision_id: "rev_2" });
    await startTemplateRun("tenant_123", "st_1", { operation: "plan" });
    await approveRun("tenant_123", "run_1");
    await discardRun("tenant_123", "run_2", { reason: "stop" });

    for (const call of fetchMock.mock.calls) {
      const init = call[1] as RequestInit | undefined;
      if (init?.body && typeof init.body === "string") {
        const parsed = JSON.parse(init.body);
        for (const field of forbiddenFields) {
          const hasField = Object.prototype.hasOwnProperty.call(parsed, field);
          expect(hasField, `request body contains forbidden field "${field}": ${JSON.stringify(parsed)}`).toBe(false);
        }
      }
    }
  });
});

describe("api client — session cookie auth", () => {
  const assign = vi.fn();

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    // restoreAllMocks restores spies; a standalone vi.fn() keeps its calls, and
    // the tests below assert that it was NOT called.
    assign.mockClear();
  });

  it("sends requests with same-origin credentials and no bearer header", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse([{ id: "stack_1" }]));

    await listStacks("tenant_123");

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/tenants/tenant_123/stacks",
      expect.objectContaining({ credentials: "same-origin" })
    );
    const callHeaders = fetchMock.mock.calls[0][1]?.headers as Headers;
    expect(callHeaders.get("authorization")).toBeNull();
  });

  it("navigates to the login route on a 401, never fetches it", async () => {
    vi.stubGlobal("location", { assign, pathname: "/stacks", search: "?selected=st_1" });
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ type: "about:blank", title: "Unauthorized", status: 401, detail: "Sign in to continue." }), {
        status: 401,
        headers: { "content-type": "application/problem+json" }
      })
    );

    try {
      await listStacks("tenant_123");
    } catch {
      // A 401 still throws ApiRequestError for the caller; the navigation
      // above is what actually recovers the session.
    }

    expect(assign).toHaveBeenCalledWith("/signin?return_to=%2Fstacks%3Fselected%3Dst_1");
  });

  // Already on the sign-in screen: a 401 there must not reload it.
  it("does not navigate away from the sign-in screen on any 401", async () => {
    vi.stubGlobal("location", { assign, pathname: "/signin", search: "" });
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ type: "about:blank", title: "Unauthorized", status: 401, detail: "Sign in to continue." }), {
        status: 401,
        headers: { "content-type": "application/problem+json" }
      })
    );

    await expect(listStacks("tenant_123")).rejects.toBeInstanceOf(ApiRequestError);
    expect(assign).not.toHaveBeenCalled();
  });

  it("builds the OIDC sign-in route with the return path encoded", () => {
    expect(ssoLoginURL("/stacks?selected=st_1")).toBe("/v1/auth/login?return_to=%2Fstacks%3Fselected%3Dst_1");
  });
});

describe("api client — logout()", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  // Regression guard for the fix two tasks prior: the /v1/auth/logout 303's
  // Location header carries the raw ID token as id_token_hint. A Location
  // header is not script-readable, but a fetch response body is — so
  // logout() must drive a real browser navigation via a form POST, never
  // fetch(), or a future edit reintroduces that credential leak silently.
  it("logs out via a real form POST, never fetch, so the id_token_hint in the 303 Location never reaches script", () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    // jsdom does not implement real form submission and logs a "not
    // implemented" error if left unstubbed.
    const submitMock = vi.spyOn(HTMLFormElement.prototype, "submit").mockImplementation(() => {});

    logout();

    expect(fetchMock).not.toHaveBeenCalled();
    const form = document.body.querySelector("form");
    expect(form).not.toBeNull();
    expect(form?.method).toBe("post");
    expect(form?.getAttribute("action")).toBe("/v1/auth/logout");
    expect(submitMock).toHaveBeenCalledTimes(1);
  });
});

function problemResponse(problem: { status: number } & Record<string, unknown>): Response {
  return new Response(JSON.stringify(problem), {
    status: problem.status,
    headers: { "content-type": "application/problem+json" }
  });
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" }
  });
}

function textResponse(body: string, status = 200): Response {
  return new Response(body, {
    status,
    headers: { "content-type": "text/plain; charset=utf-8" }
  });
}

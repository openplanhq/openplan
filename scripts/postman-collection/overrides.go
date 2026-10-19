package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// override is what a collection run needs from one operation beyond what the
// spec says about it.
type override struct {
	// jsonBody, formBody and noBody replace the spec's example body, mostly
	// so a request uses an id an earlier one saved instead of a sample.
	jsonBody string
	formBody [][2]string
	noBody   bool
	// query sets query parameter values, and includes optional ones.
	query map[string]string
	// pathVars renames a path parameter's variable, for a request that must
	// not reuse the one an earlier request saved under the parameter's name.
	pathVars map[string]string
	// expect replaces the spec's success status, for a request a run cannot
	// make succeed (the sign-in callback) or one that may legitimately lose a
	// race (approving a plan someone else already acted on).
	expect []int
	// test is extra test script: saving ids, and polling.
	test string
	// preRequest is extra pre-request script.
	preRequest string
	// noRedirect stops Postman following a redirect, so a sign-in route's
	// own 302 or 303 is what the test sees.
	noRedirect bool
	// note is appended to the spec's description.
	note string
}

// dropSession removes the session cookie the collection script adds, for the
// routes a browser reaches before it has one.
const dropSession = "// Sent without a session, as a browser reaches this route.\npm.request.headers.remove('Cookie');"

// save stores a field of a successful response for later requests.
func save(variable, expression string) string {
	return fmt.Sprintf("if (pm.response.code < 300) { pm.environment.set('%s', %s); }", variable, expression)
}

// poll re-sends the current request in a collection run until done holds, at
// most 30 times, two seconds apart. The run, the registration and the plan
// are asynchronous, so a run would otherwise race ahead of them.
func poll(done string) string {
	return `const tries = Number(pm.environment.get('poll_tries') || 0);
if (!(` + done + `) && tries < 30) {
  pm.environment.set('poll_tries', String(tries + 1));
  setTimeout(() => {}, 2000);
  pm.execution.setNextRequest(pm.info.requestName);
} else {
  pm.environment.unset('poll_tries');
}`
}

var overrides = map[string]override{
	"startOIDCLogin": {
		query:      map[string]string{"return_to": "/stacks"},
		noRedirect: true,
		preRequest: dropSession,
		test:       "pm.test('redirects to the identity provider', () => pm.expect(pm.response.headers.get('Location')).to.be.a('string').and.not.empty);",
	},
	"completeOIDCLogin": {
		query:      map[string]string{"code": "not-a-real-code", "state": "not-a-real-state"},
		noRedirect: true,
		preRequest: dropSession,
		expect:     []int{401},
		test:       "pm.test('failure is the HTML sign-in page', () => pm.expect(pm.response.headers.get('Content-Type')).to.include('text/html'));",
		note: "Only the identity provider can complete a sign-in, with a real code and the " +
			"transaction cookie. Sent from here it shows the one failure page every cause gets.",
	},
	"logout": {
		noRedirect: true,
		note:       "Revokes the session `session_cookie` holds, so it runs last.",
	},
	"backchannelLogout": {
		formBody:   [][2]string{{"logout_token", "not.a.jwt"}},
		preRequest: dropSession,
		expect:     []int{400, 503},
		note:       "Only the identity provider can send a valid logout token. A forged one is refused.",
	},
	"getMe": {
		test: save("my_sub", "pm.response.json().sub") + "\n" +
			"pm.test('tenant matches the environment', () => pm.expect(pm.response.json().tenant_id).to.eql(pm.environment.get('tenant_id')));",
	},
	"createTemplateRegistration": {
		jsonBody: jsonObject([][2]string{
			{"repo_owner", "{{template_repo_owner}}"}, {"repo_name", "{{template_repo_name}}"},
			{"source_ref", "{{template_source_ref}}"}, {"root_path", "{{template_root_path}}"},
		}),
		test: save("registration_id", "pm.response.json().id"),
	},
	"getTemplateRegistration": {
		test: "const registration = pm.response.json();\n" +
			"if (registration.template_revision_id) { pm.environment.set('template_revision_id', registration.template_revision_id); }\n" +
			poll("!['pending', 'running'].includes(registration.status)"),
		note: "In a collection run this re-sends itself until the registration ends, and saves its revision.",
	},
	"listTemplateRevisions": {
		test: "const revisions = pm.response.json().items;\n" +
			"if (!pm.environment.get('template_revision_id') && revisions.length) { pm.environment.set('template_revision_id', revisions[0].id); }",
	},
	"createStack": {
		jsonBody: `{
  "name": "Postman {{$timestamp}}",
  "tags": { "team": "platform" },
  "default_credential_ids": []
}`,
		test: save("stack_id", "pm.response.json().id"),
	},
	"addTemplateToStack": {
		jsonBody: `{
  "template_revision_id": "{{template_revision_id}}",
  "component_key": "postman-{{$timestamp}}",
  "config": {}
}`,
		test: save("stack_template_id", "pm.response.json().id"),
		note: "The config is empty, which suits a template whose variables all have defaults, " +
			"as the demo template does. Fill it in for one that has required variables.",
	},
	"updateStackTemplateConfig": {
		jsonBody: `{ "config": {} }`,
	},
	"upgradeStackTemplate": {
		jsonBody: `{ "target_template_revision_id": "{{template_revision_id}}" }`,
	},
	"createStackCredential": {
		jsonBody: `{ "name": "TF_VAR_postman_{{$timestamp}}", "value": "example-secret" }`,
		test:     save("credential_id", "pm.response.json().id"),
	},
	"createStackTemplateCredential": {
		jsonBody: `{ "name": "TF_VAR_postman_{{$timestamp}}", "value": "example-secret" }`,
		test:     save("credential_id", "pm.response.json().id"),
	},
	"startTemplateRun": {
		jsonBody: `{ "operation": "apply", "auto_approve": false }`,
		test:     save("run_id", "pm.response.json().id") + "\n" + save("run_number", "String(pm.response.json().run_number)"),
		note:     "An apply, so that its plan waits for approval and the approval request has something to approve.",
	},
	"getTemplateRun": {
		test: "const run = pm.response.json();\n" + poll("!['queued', 'running'].includes(run.status)"),
		note: "In a collection run this re-sends itself until the plan has finished.",
	},
	"listTemplateRunLogs": {
		test: "const logs = pm.response.json().items;\nif (logs.length) { pm.environment.set('name', logs[0].name); }",
		note: "Saves the first log's name, which the log-body request reads by.",
	},
	"getTemplateRunLog": {
		test: "pm.test('body is plain text', () => pm.expect(pm.response.headers.get('Content-Type')).to.include('text/plain'));",
	},
	"streamTemplateRunLog": {
		test: "pm.test('body is an event stream', () => pm.expect(pm.response.headers.get('Content-Type')).to.include('text/event-stream'));",
		note: "In a collection run the plan has finished, so the stream is one `log_end` and closes.",
	},
	"approveRun": {
		noBody: true,
		expect: []int{202, 409},
		note:   "`409` when the plan had no changes, so there is nothing to approve.",
	},
	"discardRun": {
		jsonBody: `{ "reason": "Discarded from Postman" }`,
		expect:   []int{204, 409},
		note:     "Discards the approved plan if its apply has not started yet; `409` once it has.",
	},
	"searchUsers": {
		query: map[string]string{"q": "{{user_search}}"},
		test: "const others = pm.response.json().items.filter((user) => user.sub !== pm.environment.get('my_sub'));\n" +
			"if (others.length) { pm.environment.set('user_sub', others[0].sub); }",
		note: "Saves the first match who is not you, for the grant requests.",
	},
	"assignStackRole": {
		jsonBody: `{ "role": "viewer" }`,
		// 201 the first time the person is granted, 200 on every re-run.
		expect: []int{201, 200},
		note:   "Grants a role to the person the search saved. Needs a second person who has signed in once; without one there is nobody to grant a role to.",
	},
	// A stack grant only touches the stack the run created. A platform grant
	// changes what a person may do everywhere, so a run must never aim one at
	// the colleague the user search happened to find: both requests target
	// platform_grant_sub instead, which nothing sets, and skip while it is
	// empty.
	"assignPlatformRole": {
		pathVars:   map[string]string{userSubVariable: platformGrantSubVariable},
		preRequest: skipUnlessPlatformGrantSub,
		jsonBody:   `{ "role": "viewer" }`,
		// 201 the first time the person is granted, 200 on every re-run.
		expect: []int{201, 200},
		note:   "Changes a real person's platform role: skipped unless you set platform_grant_sub to the sub of someone it is safe to make a platform viewer.",
	},
	"revokePlatformRole": {
		pathVars:   map[string]string{userSubVariable: platformGrantSubVariable},
		preRequest: skipUnlessPlatformGrantSub,
		note:       "Removes every platform role platform_grant_sub holds: skipped unless you set it.",
	},
}

// userSubVariable holds the person the user search found, for the stack grant
// requests; platformGrantSubVariable holds the person a platform grant request
// may change, and nothing sets it.
const (
	userSubVariable          = "user_sub"
	platformGrantSubVariable = "platform_grant_sub"
)

// skipUnlessPlatformGrantSub keeps a collection run from changing anyone's
// platform role unless the person running it named who.
const skipUnlessPlatformGrantSub = "if (!pm.environment.get('" + platformGrantSubVariable + "')) { pm.execution.skipRequest(); }"

// requestTest asserts the expected status, then runs the override's script.
func requestTest(expected []int, extra string) string {
	var check string
	if len(expected) == 1 {
		check = fmt.Sprintf("pm.test('status is %d', () => pm.response.to.have.status(%d));", expected[0], expected[0])
	} else {
		codes := make([]string, len(expected))
		for i, code := range expected {
			codes[i] = strconv.Itoa(code)
		}
		list := strings.Join(codes, ", ")
		check = fmt.Sprintf("pm.test('status is one of %s', () => pm.expect(pm.response.code).to.be.oneOf([%s]));", list, list)
	}
	if extra == "" {
		return check
	}
	return check + "\n" + extra
}

// jsonObject renders string fields as an indented JSON object, in order.
func jsonObject(fields [][2]string) string {
	lines := make([]string, len(fields))
	for i, field := range fields {
		lines[i] = fmt.Sprintf("  %q: %q", field[0], field[1])
	}
	return "{\n" + strings.Join(lines, ",\n") + "\n}"
}

const collectionDescription = `Every route in docs/openapi.yaml, generated by scripts/postman-collection.
Do not edit it in Postman: run ` + "`make postman-collection`" + ` after changing the spec.

Setup:
1. Select the "openplan local" environment.
2. Sign in at the web UI, then copy the openplan_session cookie's value into
   session_cookie. Keep it in the current value, which stays on your machine.
3. Run the collection top to bottom. Each create request saves the id the
   next one needs, and the asynchronous ones (a registration, a plan) are
   polled until they finish.

Every response is checked for a request id, and every error for an RFC 9457
problem document (application/problem+json with type, title, a status
matching the response, and request_id). "Sign out" revokes the session, so
it runs last.`

const collectionPreRequest = `// The API authenticates by session cookie alone; there is no bearer token.
const session = pm.environment.get('session_cookie');
if (session) {
  pm.request.headers.upsert({ key: 'Cookie', value: 'openplan_session=' + session });
}`

// nonProblemErrorPaths are the routes whose errors are not problem documents,
// by design: the sign-in routes answer a browser with HTML, and back-channel
// logout answers Dex in plain text. The collection test exempts them by
// request path, never by the response's content type, so an error that
// regresses to another media type fails instead of skipping the check.
// TestNonProblemErrorPathsMatchTheSpec keeps this list and the spec in step.
var nonProblemErrorPaths = []string{"/v1/auth/login", "/v1/auth/callback", "/v1/auth/backchannel-logout"}

var collectionTest = `// Checks every response gets.
pm.test('no server error', () => pm.expect(pm.response.code).to.be.below(500));
pm.test('carries a request id', () => pm.expect(pm.response.headers.get('X-Request-Id')).to.be.a('string').and.not.empty);
// Every other error must be a problem document. These routes are exempt by
// design: sign-in answers in HTML, back-channel logout in plain text.
const notProblemRoutes = ` + jsStringArray(nonProblemErrorPaths) + `;
if (pm.response.code >= 400 && !notProblemRoutes.includes(pm.request.url.getPath())) {
  pm.test('error body is an RFC 9457 problem document', () => {
    pm.expect(pm.response.headers.get('Content-Type') || '', 'Content-Type').to.include('application/problem+json');
    const body = pm.response.json();
    pm.expect(body.type, 'type').to.be.a('string').and.not.empty;
    pm.expect(body.title, 'title').to.be.a('string').and.not.empty;
    pm.expect(body.status, 'status').to.eql(pm.response.code);
    pm.expect(body.request_id, 'request_id').to.be.a('string').and.eql(pm.response.headers.get('X-Request-Id'));
  });
}`

// jsStringArray renders strings as a JavaScript array literal.
func jsStringArray(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = "'" + value + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// contractChecks are negative cases the spec documents but no operation's
// success path reaches: they pin down status codes and problem types.
func contractChecks() folder {
	tenant := "/v1/tenants/{{tenant_id}}"
	// expectProblem checks the status, the media type and the problem type;
	// a slug is under https://openplan.run/problems/, "" means about:blank.
	expectProblem := func(status int, slug string) string {
		problemType := "about:blank"
		if slug != "" {
			problemType = "https://openplan.run/problems/" + slug
		}
		return fmt.Sprintf("pm.test('status is %d', () => pm.response.to.have.status(%d));\n"+
			"pm.test('is a problem document', () => pm.expect(pm.response.headers.get('Content-Type')).to.include('application/problem+json'));\n"+
			"pm.test('type is %s', () => pm.expect(pm.response.json().type).to.eql('%s'));", status, status, problemType, problemType)
	}
	// firstError checks where the first errors entry points.
	firstError := func(member, value string) string {
		return fmt.Sprintf("\npm.test('first error is on %s', () => pm.expect(pm.response.json().errors[0].%s).to.eql('%s'));", value, member, value)
	}
	jsonBody := func(content string) *body { return &body{mode: bodyJSON, json: content} }
	return folder{
		name:        "Contract checks",
		description: "Negative cases that pin down status codes and problem types.",
		requests: []request{
			{name: "Unknown route is 404", method: http.MethodGet, path: "/v1/does-not-exist", test: expectProblem(404, "")},
			{
				name: "Wrong method is 405", method: http.MethodDelete, path: "/healthz", preRequest: dropSession,
				test: expectProblem(405, "") + "\npm.test('Allow names GET', () => pm.expect(pm.response.headers.get('Allow')).to.include('GET'));",
			},
			{name: "Another tenant is 404", method: http.MethodGet, path: "/v1/tenants/not-this-tenant/stacks", test: expectProblem(404, "")},
			{name: "No session is 401", method: http.MethodGet, path: "/v1/me", preRequest: dropSession, test: expectProblem(401, "")},
			{name: "Unknown body field is 400", method: http.MethodPost, path: tenant + "/stacks", body: jsonBody(`{ "unknown_field": true }`), test: expectProblem(400, "malformed-body")},
			{name: "Malformed JSON is 400", method: http.MethodPost, path: tenant + "/stacks", body: jsonBody(`{ "name": `), test: expectProblem(400, "malformed-body")},
			{
				name: "Missing required field is 422", method: http.MethodPost, path: tenant + "/stacks", body: jsonBody(`{ "name": "" }`),
				test: expectProblem(422, "validation-failed") + firstError("pointer", "#/name"),
			},
			{
				name: "Search without q is 422", method: http.MethodGet, path: tenant + "/users/search",
				test: expectProblem(422, "validation-failed") + firstError("parameter", "q"),
			},
			{
				name: "Non-integer max is 400", method: http.MethodGet, path: tenant + "/users/search", query: []queryParam{{key: "q", value: "a"}, {key: "max", value: "ten"}},
				test: expectProblem(400, "invalid-query") + firstError("parameter", "max"),
			},
			{name: "Unknown run is 404", method: http.MethodGet, path: tenant + "/template-runs/run_does_not_exist", test: expectProblem(404, "")},
			{
				name: "Config edit without config is 422", method: http.MethodPut, path: tenant + "/stack-templates/{{stack_template_id}}/config",
				body: jsonBody(`{}`), test: expectProblem(422, "validation-failed") + firstError("pointer", "#/config"),
			},
			{
				name: "A proxy's request id is kept", method: http.MethodGet, path: "/healthz",
				preRequest: "pm.request.headers.upsert({ key: 'X-Request-Id', value: 'postman-check-1' });",
				test:       "pm.test('the id comes back', () => pm.expect(pm.response.headers.get('X-Request-Id')).to.eql('postman-check-1'));",
			},
		},
	}
}

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/encryption"
	"github.com/vishu42/openplan/internal/queue"
)

type Server struct {
	service  *app.Service
	tenantID domain.TenantID
	queue    queue.Reader
	liveLogs LiveLogSubscriber
	// liveLogStates is the live streams' shared re-check of each log's state.
	liveLogStates *liveLogStateCache
	// liveStreamsClosing closes when the process is stopping, which ends
	// every open live stream: http.Server.Shutdown waits for handlers, and a
	// stream otherwise lasts as long as its viewer.
	liveStreamsClosing chan struct{}
	closeLiveStreams   sync.Once
	auth               AuthConfig
	mux                *http.ServeMux
	handler            http.Handler
	debug              bool
}

// ServerOption configures optional server dependencies.
type ServerOption func(*Server)

// WithQueueReader enables the queue read endpoint.
func WithQueueReader(reader queue.Reader) ServerOption {
	return func(server *Server) { server.queue = reader }
}

// WithLiveLogs enables following a running command's log over server-sent
// events.
func WithLiveLogs(subscriber LiveLogSubscriber) ServerOption {
	return func(server *Server) { server.liveLogs = subscriber }
}

// CloseLiveStreams ends every open live log stream as still live, so each
// viewer reconnects from its last event, to this process once it is back or
// to another replica. Call it before http.Server.Shutdown, which waits for
// handlers and would otherwise wait on every viewer. Safe to call more than
// once.
func (server *Server) CloseLiveStreams() {
	server.closeLiveStreams.Do(func() { close(server.liveStreamsClosing) })
}

// AuthFlow is the subset of the OIDC flow the handlers use. *authentication.Flow
// satisfies it; the interface exists so handler tests need no live IdP.
type AuthFlow interface {
	AuthorizationURL(state, nonce, codeVerifier string) (string, error)
	Exchange(ctx context.Context, code, codeVerifier string) (string, error)
	EndSessionURL(idTokenHint, postLogoutRedirectURI string) string
}

// AuthConfig carries what the browser login routes need.
type AuthConfig struct {
	Flow     AuthFlow
	Verifier authentication.Verifier
	Sealer   *encryption.Cipher
	// PublicURL is the origin the browser reaches, with no trailing slash. It
	// is configured rather than derived from Host or X-Forwarded-Proto, which
	// a caller can spoof.
	PublicURL     string
	SecureCookies bool
	// Sessions persists the app-owned session the callback creates. The
	// browser is handed a reference to it, never the ID token.
	Sessions           authentication.SessionStore
	SessionAbsoluteTTL time.Duration
	SessionIdleTTL     time.Duration
	// LogoutTokenVerifier authenticates back-channel logout notifications.
	// *authentication.OIDCVerifier satisfies it in production.
	LogoutTokenVerifier LogoutTokenVerifier
	// Clock is time.Now when nil. Tests set it.
	Clock func() time.Time
}

// WithAuth enables the browser login routes.
func WithAuth(cfg AuthConfig) ServerOption {
	return func(server *Server) { server.auth = cfg }
}

// requireCompleteAuthWiring panics if a WithAuth configuration is missing a
// dependency the browser login routes assume is present. Registration is
// gated on Flow alone, so nothing else stops a caller from wiring Flow
// without, say, Sessions — and the handlers dereference Sessions
// unconditionally on every request. Catching that gap here, once, at boot,
// turns a missing dependency into a deterministic startup panic instead of a
// nil-pointer panic (or, for the TTLs, a session that is born already
// expired) on the first real request.
//
// The service is checked alongside the AuthConfig because the callback
// projects the signed-in user through it, so it is now as load-bearing for
// the login routes as the session store is — including its user repository,
// which the callback writes the projection through on every sign-in.
func requireCompleteAuthWiring(service *app.Service, auth AuthConfig) {
	switch {
	case service == nil:
		panic("api: WithAuth configured without a Service")
	case service.Users == nil:
		panic("api: WithAuth configured without a Service user repository")
	case auth.Verifier == nil:
		panic("api: WithAuth configured with a Flow but without a Verifier")
	case auth.Sealer == nil:
		panic("api: WithAuth configured without a Sealer")
	case auth.Sessions == nil:
		panic("api: WithAuth configured without a Sessions store")
	case auth.SessionAbsoluteTTL <= 0:
		panic("api: WithAuth configured without a positive SessionAbsoluteTTL")
	case auth.SessionIdleTTL <= 0:
		panic("api: WithAuth configured without a positive SessionIdleTTL")
	}
}

func NewServer(service *app.Service, tenantID domain.TenantID, options ...ServerOption) *Server {
	server := &Server{
		service:            service,
		tenantID:           tenantID,
		liveLogStates:      newLiveLogStateCache(),
		liveStreamsClosing: make(chan struct{}),
		mux:                http.NewServeMux(),
	}
	for _, option := range options {
		option(server)
	}

	// Browser login routes. Registered only when auth is configured, so an
	// unauthenticated test server does not expose a half-wired flow. The
	// identity provider is the only way in, so its Flow is what configures
	// them.
	if server.auth.Flow != nil {
		requireCompleteAuthWiring(server.service, server.auth)
		server.mux.HandleFunc("GET /v1/auth/login", server.handleAuthLogin)
		server.mux.HandleFunc("GET /v1/auth/callback", server.handleAuthCallback)
		server.mux.HandleFunc("POST /v1/auth/logout", server.handleAuthLogout)
		server.mux.HandleFunc("POST /v1/auth/backchannel-logout", server.handleBackchannelLogout)
	}

	// Health routes.
	// Reports process liveness for probes and local smoke checks.
	server.mux.HandleFunc("GET /healthz", server.handleHealth)

	// Identity route.
	// Returns the authenticated principal's identity, global capabilities, and configured tenant.
	server.mux.HandleFunc("GET /v1/me", server.handleMe)

	// Template routes.
	// Registers a GitHub Terraform source as a template and starts syncing it.
	server.handleTenantRoute("POST", "/templates", server.handleCreateTemplate)
	// Lists the tenant's templates, newest first, a page at a time.
	server.handleTenantRoute("GET", "/templates", server.handleListTemplates)
	// Reads one template.
	server.handleTenantRoute("GET", "/templates/{template_id}", server.handleGetTemplate)
	// Deletes a template no stack has installed.
	server.handleTenantRoute("DELETE", "/templates/{template_id}", server.handleDeleteTemplate)
	// Syncs a template again, to pick up a new commit or retry a failed sync.
	server.handleTenantRoute("POST", "/templates/{template_id}/syncs", server.handleStartTemplateSync)
	// Lists one template's revisions, newest first, a page at a time.
	server.handleTenantRoute("GET", "/templates/{template_id}/revisions", server.handleListTemplateRevisionsForTemplate)

	// Template revision routes.
	// Lists variables inferred from an immutable registered template revision.
	server.handleTenantRoute("GET", "/template-revisions/{template_revision_id}/variables", server.handleGetTemplateRevisionVariables)

	// Stack routes.
	// Creates a logical infrastructure stack.
	server.handleTenantRoute("POST", "/stacks", server.handleCreateStack)
	// Lists tenant-owned stacks.
	server.handleTenantRoute("GET", "/stacks", server.handleListStacks)
	// Lists the plans waiting for approval and the failed destroys across
	// every stack the caller can view.
	server.handleTenantRoute("GET", "/attention", server.handleListAttention)
	// Reads one stack with installed templates.
	server.handleTenantRoute("GET", "/stacks/{stack_id}", server.handleGetStack)
	server.handleTenantRoute("POST", "/stacks/{stack_id}/credentials", server.handleCreateStackCredential)
	server.handleTenantRoute("GET", "/stacks/{stack_id}/credentials", server.handleListStackCredentials)
	server.handleTenantRoute("DELETE", "/stacks/{stack_id}/credentials/{credential_id}", server.handleDeleteStackCredential)
	// Installs a registered template into a stack.
	server.handleTenantRoute("POST", "/stacks/{stack_id}/templates", server.handleAddTemplateToStack)
	// Replaces the whole desired config for an installed stack template.
	server.handleTenantRoute("PUT", "/stack-templates/{stack_template_id}/config", server.handleUpdateStackTemplateConfig)
	// Stages an installed stack template to a newer template revision.
	server.handleTenantRoute("POST", "/stack-templates/{stack_template_id}/change-revision", server.handleChangeStackTemplateRevision)

	// Template run routes.
	// Starts a Terraform operation for an installed stack template.
	server.handleTenantRoute("POST", "/stack-templates/{stack_template_id}/runs", server.handleStartTemplateRun)
	server.handleTenantRoute("POST", "/stack-templates/{stack_template_id}/credentials", server.handleCreateStackTemplateCredential)
	server.handleTenantRoute("GET", "/stack-templates/{stack_template_id}/credentials", server.handleListStackTemplateCredentials)
	server.handleTenantRoute("DELETE", "/stack-templates/{stack_template_id}/credentials/{credential_id}", server.handleDeleteStackTemplateCredential)
	// Lists runs for an installed stack template, most recent first, a page at a time.
	server.handleTenantRoute("GET", "/stack-templates/{stack_template_id}/runs", server.handleListTemplateRuns)
	// Reads one run by its number within the stack template, which is how a
	// run's page is addressed.
	server.handleTenantRoute("GET", "/stack-templates/{stack_template_id}/runs/{run_number}", server.handleGetTemplateRunByNumber)
	// Reads the current state of a template run.
	server.handleTenantRoute("GET", "/template-runs/{run_id}", server.handleGetTemplateRun)
	// Lists the resources a run's plan changes, removals first, a page at a time.
	server.handleTenantRoute("GET", "/template-runs/{run_id}/changes", server.handleListTemplateRunResourceChanges)
	// Lists persisted log metadata for all phases of a template run.
	server.handleTenantRoute("GET", "/template-runs/{run_id}/logs", server.handleListTemplateRunLogs)
	// Reads one persisted log body by the log's name, such as plan-1.
	server.handleTenantRoute("GET", "/template-runs/{run_id}/logs/{name}", server.handleGetTemplateRunLog)
	// Follows one log live, as server-sent events, while its command runs.
	server.handleTenantRoute("GET", "/template-runs/{run_id}/logs/{name}/events", server.handleStreamTemplateRunLog)

	// User search routes.
	// Searches signed-in users by display name or email.
	server.handleTenantRoute("GET", "/users/search", server.handleSearchUsers)

	// Queue route.
	// Lists the work items the caller queued.
	server.handleTenantRoute("GET", "/queue", server.handleListQueue)

	// Stack grant routes.
	// Lists grants for a stack.
	server.handleTenantRoute("GET", "/stacks/{stack_id}/grants", server.handleListStackGrants)
	// Sets one person's role on a stack, creating or replacing their grant.
	server.handleTenantRoute("PUT", "/stacks/{stack_id}/grants/{user_sub}", server.handleAssignStackRole)
	// Revokes a role from a stack.
	server.handleTenantRoute("DELETE", "/stacks/{stack_id}/grants/{user_sub}", server.handleRevokeStackRole)

	// Platform grant routes, the stack grant routes on the platform singleton.
	// Tenant-scoped like every resource route: an api serves one tenant.
	// Lists who holds a platform role.
	server.handleTenantRoute("GET", "/platform/grants", server.handleListPlatformGrants)
	// Sets one person's platform role, creating or replacing their grant.
	server.handleTenantRoute("PUT", "/platform/grants/{user_sub}", server.handleAssignPlatformRole)
	// Revokes a person's platform role.
	server.handleTenantRoute("DELETE", "/platform/grants/{user_sub}", server.handleRevokePlatformRole)

	// Template run decision routes.
	// Records approval for a waiting template run.
	server.handleTenantRoute("POST", "/template-runs/{run_id}/approve", server.handleApproveRun)
	// Discards a template run's plan waiting for approval.
	server.handleTenantRoute("POST", "/template-runs/{run_id}/discard", server.handleDiscardRun)
	server.handler = withErrorEnvelope(server.mux)
	return server
}

// withErrorEnvelope answers requests no route matches with a problem
// document. Left to itself the mux writes plain text: "404 page not found",
// or "Method Not Allowed" with an Allow header.
//
// Only unrouted requests are touched. mux.Handler reports the pattern a
// request would reach, and it is empty exactly when the mux would answer with
// one of those two errors; a redirect to a canonical path still has one.
func withErrorEnvelope(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, pattern := mux.Handler(request); pattern != "" {
			mux.ServeHTTP(response, request)
			return
		}
		// Let the mux decide 404 or 405 and set Allow, then replace only
		// its plain-text body.
		unrouted := &unroutedResponse{header: response.Header()}
		mux.ServeHTTP(unrouted, request)
		if unrouted.status == http.StatusMethodNotAllowed {
			writeProblem(response, blankProblem(http.StatusMethodNotAllowed, "This route doesn't accept this method; Allow lists the ones it does."))
			return
		}
		writeProblem(response, blankProblem(http.StatusNotFound, notFoundDetail))
	})
}

// unroutedResponse records the status the mux chose for an unrouted request
// and discards its body. Headers go straight to the real response, so Allow
// arrives with the envelope.
type unroutedResponse struct {
	header http.Header
	status int
}

func (response *unroutedResponse) Header() http.Header            { return response.header }
func (response *unroutedResponse) Write(body []byte) (int, error) { return len(body), nil }
func (response *unroutedResponse) WriteHeader(status int)         { response.status = status }

// NewAuthenticatedServer requires a session on every route except the health
// probe and the sign-in routes.
func NewAuthenticatedServer(service *app.Service, tenantID domain.TenantID, debug bool, options ...ServerOption) *Server {
	server := NewServer(service, tenantID, options...)
	server.debug = debug
	// No verifier here. The middleware authenticates against the session store
	// alone, so the only place a token is verified is the OIDC callback, which
	// reaches its verifier through server.auth. This used to take a verifier of
	// its own for the Bearer path, which had to be kept the same instance as
	// server.auth.Verifier by hand because nothing in the type system said so.
	server.handler = authentication.RequireAuthentication(
		server.auth.Sessions,
		server.auth.SessionIdleTTL,
		server.auth.Clock,
		// Sign-in has to be reachable before there is a session.
		"/healthz", "/v1/auth/login", "/v1/auth/callback", "/v1/auth/logout", "/v1/auth/backchannel-logout",
	)(server.handler)
	return server
}

// handleMe returns the authenticated principal's identity, global capabilities,
// and the configured tenant ID.
func (server *Server) handleMe(response http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.Subject == "" {
		writeAppError(response, app.ErrUnauthenticated)
		return
	}
	capabilities, err := server.service.PlatformCapabilities(request.Context())
	if err != nil {
		writeAppError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, meFromPrincipal(principal, server.tenantID, globalCapabilities{
		CanCreateStack:          capabilities.CanCreateStack,
		CanManageTemplates:      capabilities.CanManageTemplates,
		CanManagePlatformAccess: capabilities.CanManagePlatformAccess,
	}))
}

// tenantPrefix roots every tenant route. handleTenantRoute adds it, so no
// route can carry the prefix without the configured-tenant check.
const tenantPrefix = "/v1/tenants/{tenant_id}"

func (server *Server) handleTenantRoute(method, path string, handler http.HandlerFunc) {
	server.mux.Handle(method+" "+tenantPrefix+path, server.requireConfiguredTenant(handler))
}

func (server *Server) requireConfiguredTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if domain.TenantID(request.PathValue("tenant_id")) != server.tenantID {
			writeProblem(response, blankProblem(http.StatusNotFound, notFoundDetail))
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (server *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	server.debugf("%s %s", request.Method, request.URL.Path)
	withRequestID(server.handler).ServeHTTP(response, request)
}

func (server *Server) now() time.Time {
	if server.auth.Clock != nil {
		return server.auth.Clock()
	}
	return time.Now().UTC()
}

func (server *Server) debugf(format string, args ...any) {
	if server.debug {
		log.Printf("[DEBUG] "+format, args...) //nolint:gosec // callers pass constant formats
	}
}

// maxRequestBodyBytes caps every JSON request body. The largest legitimate one
// is a credential or a run's variables, a few kilobytes; a megabyte leaves
// room for those to grow while keeping one request from holding unbounded
// memory, since the body is buffered whole before it is validated.
const maxRequestBodyBytes = 1 << 20

func decodeRequestBody(response http.ResponseWriter, request *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxRequestBodyBytes))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		if !writeRequestTooLarge(response, err) {
			writeMalformedBody(response, schemaMismatch)
		}
		return false
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		writeMalformedBody(response, notOneObject)
		return false
	}
	bodyDecoder := json.NewDecoder(bytes.NewReader(raw))
	bodyDecoder.DisallowUnknownFields()
	if err := bodyDecoder.Decode(destination); err != nil {
		writeMalformedBody(response, schemaMismatch)
		return false
	}
	// Trailing bytes count toward the cap too, so this read can be the one
	// that crosses it.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if !writeRequestTooLarge(response, err) {
			writeMalformedBody(response, notOneObject)
		}
		return false
	}
	return true
}

const (
	schemaMismatch = "The request body doesn't match the expected schema: a field is unknown or has the wrong type."
	notOneObject   = "The request body must be exactly one JSON object."
	notFoundDetail = "The resource doesn't exist, or you can't see it."
)

// writeMalformedBody is the 400 for a body that can't be parsed, as opposed
// to the 422 for one that parses but breaks a rule. A client shouldn't send
// one, so the detail is for whoever is debugging it.
func writeMalformedBody(response http.ResponseWriter, detail string) {
	writeProblem(response, problemDocument{
		Type: problemTypeURI("malformed-body"), Title: "The request body can't be read",
		Status: http.StatusBadRequest, Detail: detail,
	})
}

// decodeOptionalRequestBody is decodeRequestBody for an action whose body
// carries nothing it needs: no body at all is accepted as {}, and anything
// sent must still be one JSON object that matches destination.
func decodeOptionalRequestBody(response http.ResponseWriter, request *http.Request, destination any) bool {
	reader := bufio.NewReader(request.Body)
	if _, err := reader.Peek(1); errors.Is(err, io.EOF) {
		return true
	}
	request.Body = struct {
		io.Reader
		io.Closer
	}{reader, request.Body}
	return decodeRequestBody(response, request, destination)
}

// writeRequestTooLarge answers 413 when err is the body cap tripping, and
// reports whether it did.
func writeRequestTooLarge(response http.ResponseWriter, err error) bool {
	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		return false
	}
	writeProblem(response, blankProblem(http.StatusRequestEntityTooLarge, "The request body must not exceed 1 MiB."))
	return true
}

func (server *Server) handleHealth(response http.ResponseWriter, request *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) handleSearchUsers(response http.ResponseWriter, request *http.Request) {
	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}

	users, err := server.service.SearchUsers(request.Context(), app.SearchUsersCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		StackID:  domain.StackID(request.URL.Query().Get("stack_id")),
		Query:    request.URL.Query().Get("q"),
		Page:     page,
	})
	if err != nil {
		server.debugf("handleSearchUsers error: %v", err)
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(users.Items, users.NextCursor))
}

// handleCreateTemplate answers 202 with the template, whether the source was
// new or already registered: either way a sync of it was just queued.
func (server *Server) handleCreateTemplate(response http.ResponseWriter, request *http.Request) {
	var body registerTemplateRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	started, err := server.service.RegisterTemplate(request.Context(), app.RegisterTemplateCommand{
		TenantID:  domain.TenantID(request.PathValue("tenant_id")),
		RepoOwner: body.RepoOwner,
		RepoName:  body.RepoName,
		SourceRef: body.SourceRef,
		RootPath:  body.RootPath,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	setLocation(response, request, "templates", string(started.Template.ID))
	writeJSON(response, http.StatusAccepted, newTemplateResponse(started.Template, server.service.Clock.Now()))
}

func (server *Server) handleListTemplates(response http.ResponseWriter, request *http.Request) {
	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}
	templates, err := server.service.ListTemplates(request.Context(), app.ListTemplatesCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		Page:     page,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	now := server.service.Clock.Now()
	items := mapItems(templates.Items, func(template domain.SourceTemplate) templateResponse { return newTemplateResponse(template, now) })
	writeJSON(response, http.StatusOK, newListResponse(items, templates.NextCursor))
}

func (server *Server) handleGetTemplate(response http.ResponseWriter, request *http.Request) {
	template, err := server.service.GetTemplate(request.Context(), app.GetTemplateCommand{
		TenantID:   domain.TenantID(request.PathValue("tenant_id")),
		TemplateID: domain.SourceTemplateID(request.PathValue("template_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newTemplateResponse(template, server.service.Clock.Now()))
}

func (server *Server) handleDeleteTemplate(response http.ResponseWriter, request *http.Request) {
	err := server.service.DeleteTemplate(request.Context(), app.DeleteTemplateCommand{
		TenantID:   domain.TenantID(request.PathValue("tenant_id")),
		TemplateID: domain.SourceTemplateID(request.PathValue("template_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	response.WriteHeader(http.StatusNoContent)
}

// handleStartTemplateSync answers 202 with the template, not the sync: a sync
// has no read of its own, and the template is what a client follows (owner
// decision, 2026-10-21).
func (server *Server) handleStartTemplateSync(response http.ResponseWriter, request *http.Request) {
	var body startTemplateSyncRequest
	if !decodeOptionalRequestBody(response, request, &body) {
		return
	}

	started, err := server.service.StartTemplateSync(request.Context(), app.StartTemplateSyncCommand{
		TenantID:   domain.TenantID(request.PathValue("tenant_id")),
		TemplateID: domain.SourceTemplateID(request.PathValue("template_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	setLocation(response, request, "templates", string(started.Template.ID))
	writeJSON(response, http.StatusAccepted, newTemplateResponse(started.Template, server.service.Clock.Now()))
}

func (server *Server) handleListTemplateRevisionsForTemplate(response http.ResponseWriter, request *http.Request) {
	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}
	revisions, err := server.service.ListTemplateRevisionsForTemplate(request.Context(), app.ListTemplateRevisionsForTemplateCommand{
		TenantID:   domain.TenantID(request.PathValue("tenant_id")),
		TemplateID: domain.SourceTemplateID(request.PathValue("template_id")),
		Page:       page,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(mapItems(revisions.Items, newTemplateRevisionResponse), revisions.NextCursor))
}

func (server *Server) handleGetTemplateRevisionVariables(response http.ResponseWriter, request *http.Request) {
	variables, err := server.service.GetTemplateRevisionVariables(request.Context(), app.GetTemplateRevisionVariablesCommand{
		TenantID:           domain.TenantID(request.PathValue("tenant_id")),
		TemplateRevisionID: domain.TemplateRevisionID(request.PathValue("template_revision_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	// Not paged: a revision's variables are fixed, and a form needs all of them.
	writeJSON(response, http.StatusOK, newListResponse(variables, ""))
}

func (server *Server) handleCreateStack(response http.ResponseWriter, request *http.Request) {
	var body createStackRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	credentialIDs := make([]domain.CredentialID, 0, len(body.DefaultCredentialIDs))
	for _, id := range body.DefaultCredentialIDs {
		credentialIDs = append(credentialIDs, domain.CredentialID(id))
	}

	stack, err := server.service.CreateStack(request.Context(), app.CreateStackCommand{
		TenantID:             domain.TenantID(request.PathValue("tenant_id")),
		Name:                 body.Name,
		Slug:                 body.Slug,
		Tags:                 body.Tags,
		DefaultCredentialIDs: credentialIDs,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	caps, err := app.ResolveStackCapabilities(request.Context(), server.service.Authorization, stack.ID)
	if err != nil {
		writeAppError(response, err)
		return
	}

	setLocation(response, request, "stacks", string(stack.ID))
	writeJSON(response, http.StatusCreated, newStackResponse(stack, caps))
}

func (server *Server) handleListStacks(response http.ResponseWriter, request *http.Request) {
	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}
	summaries, err := server.service.ListStackSummaries(request.Context(), app.ListStacksCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		Page:     page,
		Query:    request.URL.Query().Get("q"),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	stacks := make([]domain.Stack, 0, len(summaries.Items))
	for _, summary := range summaries.Items {
		stacks = append(stacks, summary.Stack)
	}
	capsByID, err := app.ResolveStacksCapabilities(request.Context(), server.service.Authorization, stacks)
	if err != nil {
		writeAppError(response, err)
		return
	}

	body := make([]stackListItemResponse, 0, len(summaries.Items))
	for _, summary := range summaries.Items {
		body = append(body, stackListItemResponse{
			stackResponse: newStackResponse(summary.Stack, capsByID[summary.Stack.ID]),
			TemplateCount: summary.TemplateCount,
		})
	}
	writeJSON(response, http.StatusOK, newListResponse(body, summaries.NextCursor))
}

func (server *Server) handleListAttention(response http.ResponseWriter, request *http.Request) {
	items, err := server.service.ListAttention(request.Context(), app.ListAttentionCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	// Not paged: it is computed in memory from every visible template, and
	// every screen that reads it needs all of it.
	writeJSON(response, http.StatusOK, newListResponse(mapItems(items, newAttentionItemResponse), ""))
}

func (server *Server) handleGetStack(response http.ResponseWriter, request *http.Request) {
	view, err := server.service.GetStack(request.Context(), app.GetStackCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		StackID:  domain.StackID(request.PathValue("stack_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newStackViewResponse(view))
}

// handleCreateStackCredential stores a Stack-scoped credential value; only a
// value marked not sensitive comes back.
func (server *Server) handleCreateStackCredential(response http.ResponseWriter, request *http.Request) {
	var body credentialRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}
	server.debugf("credential create request scope=stack tenant_id=%s stack_id=%s name=%s value_present=%t", request.PathValue("tenant_id"), request.PathValue("stack_id"), body.Name, body.Value != "")
	credential, err := server.service.CreateCredential(request.Context(), app.CreateCredentialCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")), Scope: domain.CredentialScopeStack,
		StackID: domain.StackID(request.PathValue("stack_id")), Name: body.Name, Value: body.Value, Sensitive: body.Sensitive,
	})
	if err != nil {
		server.debugf("credential create failed scope=stack tenant_id=%s stack_id=%s name=%s error=%v", request.PathValue("tenant_id"), request.PathValue("stack_id"), body.Name, err)
		writeAppError(response, err)
		return
	}
	server.debugf("credential create succeeded scope=stack tenant_id=%s stack_id=%s credential_id=%s name=%s", request.PathValue("tenant_id"), request.PathValue("stack_id"), credential.ID, credential.Name)
	writeJSON(response, http.StatusCreated, credential)
}

// handleListStackCredentials returns Stack credential metadata, with values
// only for credentials that are not sensitive.
func (server *Server) handleListStackCredentials(response http.ResponseWriter, request *http.Request) {
	server.debugf("credential list request scope=stack tenant_id=%s stack_id=%s", request.PathValue("tenant_id"), request.PathValue("stack_id"))
	credentials, err := server.service.ListCredentials(request.Context(), app.ListCredentialsCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")), Scope: domain.CredentialScopeStack,
		StackID: domain.StackID(request.PathValue("stack_id")),
	})
	if err != nil {
		server.debugf("credential list failed scope=stack tenant_id=%s stack_id=%s error=%v", request.PathValue("tenant_id"), request.PathValue("stack_id"), err)
		writeAppError(response, err)
		return
	}
	server.debugf("credential list succeeded scope=stack tenant_id=%s stack_id=%s count=%d", request.PathValue("tenant_id"), request.PathValue("stack_id"), len(credentials))
	writeJSON(response, http.StatusOK, newListResponse(credentials, ""))
}

// handleDeleteStackCredential deletes one Stack-scoped credential after authorization.
//
//nolint:dupl // Stack and stack-template scopes stay separate handlers, like their routes.
func (server *Server) handleDeleteStackCredential(response http.ResponseWriter, request *http.Request) {
	server.debugf("credential delete request scope=stack tenant_id=%s stack_id=%s credential_id=%s", request.PathValue("tenant_id"), request.PathValue("stack_id"), request.PathValue("credential_id"))
	err := server.service.DeleteCredential(request.Context(), app.DeleteCredentialCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")), StackID: domain.StackID(request.PathValue("stack_id")),
		CredentialID: domain.CredentialID(request.PathValue("credential_id")),
	})
	if err != nil {
		server.debugf("credential delete failed scope=stack tenant_id=%s stack_id=%s credential_id=%s error=%v", request.PathValue("tenant_id"), request.PathValue("stack_id"), request.PathValue("credential_id"), err)
		writeAppError(response, err)
		return
	}
	server.debugf("credential delete succeeded scope=stack tenant_id=%s stack_id=%s credential_id=%s", request.PathValue("tenant_id"), request.PathValue("stack_id"), request.PathValue("credential_id"))
	response.WriteHeader(http.StatusNoContent)
}

// handleCreateStackTemplateCredential stores a StackTemplate-scoped credential
// value; only a value marked not sensitive comes back.
func (server *Server) handleCreateStackTemplateCredential(response http.ResponseWriter, request *http.Request) {
	var body credentialRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}
	server.debugf("credential create request scope=stack_template tenant_id=%s stack_template_id=%s name=%s value_present=%t", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), body.Name, body.Value != "")
	credential, err := server.service.CreateCredential(request.Context(), app.CreateCredentialCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")), Scope: domain.CredentialScopeStackTemplate,
		StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")), Name: body.Name, Value: body.Value, Sensitive: body.Sensitive,
	})
	if err != nil {
		server.debugf("credential create failed scope=stack_template tenant_id=%s stack_template_id=%s name=%s error=%v", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), body.Name, err)
		writeAppError(response, err)
		return
	}
	server.debugf("credential create succeeded scope=stack_template tenant_id=%s stack_template_id=%s credential_id=%s name=%s", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), credential.ID, credential.Name)
	writeJSON(response, http.StatusCreated, credential)
}

// handleListStackTemplateCredentials returns StackTemplate credential metadata,
// with values only for credentials that are not sensitive.
func (server *Server) handleListStackTemplateCredentials(response http.ResponseWriter, request *http.Request) {
	server.debugf("credential list request scope=stack_template tenant_id=%s stack_template_id=%s", request.PathValue("tenant_id"), request.PathValue("stack_template_id"))
	credentials, err := server.service.ListCredentials(request.Context(), app.ListCredentialsCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")), Scope: domain.CredentialScopeStackTemplate,
		StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")),
	})
	if err != nil {
		server.debugf("credential list failed scope=stack_template tenant_id=%s stack_template_id=%s error=%v", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), err)
		writeAppError(response, err)
		return
	}
	server.debugf("credential list succeeded scope=stack_template tenant_id=%s stack_template_id=%s count=%d", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), len(credentials))
	writeJSON(response, http.StatusOK, newListResponse(credentials, ""))
}

// handleDeleteStackTemplateCredential deletes one StackTemplate-scoped credential after authorization.
//
//nolint:dupl // Stack and stack-template scopes stay separate handlers, like their routes.
func (server *Server) handleDeleteStackTemplateCredential(response http.ResponseWriter, request *http.Request) {
	server.debugf("credential delete request scope=stack_template tenant_id=%s stack_template_id=%s credential_id=%s", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), request.PathValue("credential_id"))
	err := server.service.DeleteCredential(request.Context(), app.DeleteCredentialCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")), StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")),
		CredentialID: domain.CredentialID(request.PathValue("credential_id")),
	})
	if err != nil {
		server.debugf("credential delete failed scope=stack_template tenant_id=%s stack_template_id=%s credential_id=%s error=%v", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), request.PathValue("credential_id"), err)
		writeAppError(response, err)
		return
	}
	server.debugf("credential delete succeeded scope=stack_template tenant_id=%s stack_template_id=%s credential_id=%s", request.PathValue("tenant_id"), request.PathValue("stack_template_id"), request.PathValue("credential_id"))
	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleAddTemplateToStack(response http.ResponseWriter, request *http.Request) {
	var body addTemplateToStackRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	configJSON, ok := stackTemplateConfigJSON(response, body.Config)
	if !ok {
		return
	}

	stackTemplate, err := server.service.AddTemplateToStack(request.Context(), app.AddTemplateToStackCommand{
		TenantID:           domain.TenantID(request.PathValue("tenant_id")),
		StackID:            domain.StackID(request.PathValue("stack_id")),
		TemplateRevisionID: domain.TemplateRevisionID(body.TemplateRevisionID),
		ComponentKey:       body.ComponentKey,
		ConfigJSON:         configJSON,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusCreated, newStackTemplateResponse(stackTemplate))
}

func (server *Server) handleUpdateStackTemplateConfig(response http.ResponseWriter, request *http.Request) {
	var body updateStackTemplateConfigRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}
	// The edit replaces the whole config, so a missing one would wipe every
	// value rather than leave them alone; it is sent on as empty, and the
	// app refuses it. Send {} to clear the config on purpose.
	var configJSON json.RawMessage
	if body.Config != nil {
		var ok bool
		configJSON, ok = stackTemplateConfigJSON(response, body.Config)
		if !ok {
			return
		}
	}

	stackTemplate, err := server.service.UpdateStackTemplateConfig(request.Context(), app.UpdateStackTemplateConfigCommand{
		TenantID:        domain.TenantID(request.PathValue("tenant_id")),
		StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")),
		ConfigJSON:      configJSON,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newStackTemplateResponse(stackTemplate))
}

func (server *Server) handleChangeStackTemplateRevision(response http.ResponseWriter, request *http.Request) {
	var body changeStackTemplateRevisionRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	var configJSON json.RawMessage
	if body.Config != nil {
		var ok bool
		configJSON, ok = stackTemplateConfigJSON(response, body.Config)
		if !ok {
			return
		}
	}

	stackTemplate, err := server.service.ChangeStackTemplateRevision(request.Context(), app.ChangeStackTemplateRevisionCommand{
		TenantID:                 domain.TenantID(request.PathValue("tenant_id")),
		StackTemplateID:          domain.StackTemplateID(request.PathValue("stack_template_id")),
		TargetTemplateRevisionID: domain.TemplateRevisionID(body.TargetTemplateRevisionID),
		ConfigJSON:               configJSON,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newStackTemplateResponse(stackTemplate))
}

func (server *Server) handleStartTemplateRun(response http.ResponseWriter, request *http.Request) {
	var body startTemplateRunRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	run, err := server.service.StartTemplateRun(request.Context(), app.StartTemplateRunCommand{
		TenantID:        domain.TenantID(request.PathValue("tenant_id")),
		StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")),
		Operation:       domain.OperationType(body.Operation),
		AutoApprove:     body.AutoApprove,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	setLocation(response, request, "template-runs", string(run.ID))
	writeJSON(response, http.StatusAccepted, newTemplateRunResponse(run))
}

func (server *Server) handleGetTemplateRun(response http.ResponseWriter, request *http.Request) {
	run, err := server.service.GetTemplateRun(request.Context(), app.GetTemplateRunCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newTemplateRunResponse(run))
}

func (server *Server) handleListTemplateRuns(response http.ResponseWriter, request *http.Request) {
	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}
	runs, err := server.service.ListTemplateRuns(request.Context(), app.ListTemplateRunsCommand{
		TenantID:        domain.TenantID(request.PathValue("tenant_id")),
		StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")),
		Page:            page,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(mapItems(runs.Items, newTemplateRunResponse), runs.NextCursor))
}

// handleGetTemplateRunByNumber reads a run by the number its page is
// addressed by, so a run beyond the first page of the list can still be
// opened. A number that isn't one names no run, which is a 404, as an unknown
// log name is.
func (server *Server) handleGetTemplateRunByNumber(response http.ResponseWriter, request *http.Request) {
	runNumber, err := strconv.Atoi(request.PathValue("run_number"))
	if err != nil {
		writeAppError(response, app.ErrNotFound)
		return
	}
	run, err := server.service.GetTemplateRunByNumber(request.Context(), app.GetTemplateRunByNumberCommand{
		TenantID:        domain.TenantID(request.PathValue("tenant_id")),
		StackTemplateID: domain.StackTemplateID(request.PathValue("stack_template_id")),
		RunNumber:       runNumber,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newTemplateRunResponse(run))
}

func (server *Server) handleGetTemplateRunLog(response http.ResponseWriter, request *http.Request) {
	content, err := server.service.GetTemplateRunLog(request.Context(), app.GetTemplateRunLogCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
		Name:     request.PathValue("name"),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
}

func (server *Server) handleListTemplateRunResourceChanges(response http.ResponseWriter, request *http.Request) {
	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}
	changes, err := server.service.ListTemplateRunResourceChanges(request.Context(), app.ListTemplateRunResourceChangesCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
		Page:     page,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(mapItems(changes.Items, newPlanResourceChangeResponse), changes.NextCursor))
}

func (server *Server) handleListTemplateRunLogs(response http.ResponseWriter, request *http.Request) {
	logs, err := server.service.ListTemplateRunLogs(request.Context(), app.ListTemplateRunLogsCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(mapItems(logs, newTemplateRunLogResponse), ""))
}

func (server *Server) handleApproveRun(response http.ResponseWriter, request *http.Request) {
	var body approveRunRequest
	if !decodeOptionalRequestBody(response, request, &body) {
		return
	}

	run, err := server.service.ApproveRun(request.Context(), app.ApproveRunCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	// The apply is queued, not done: the client follows it at the run.
	setLocation(response, request, "template-runs", string(run.ID))
	writeJSON(response, http.StatusAccepted, newTemplateRunResponse(run))
}

func (server *Server) handleDiscardRun(response http.ResponseWriter, request *http.Request) {
	var body discardRunRequest
	if !decodeOptionalRequestBody(response, request, &body) {
		return
	}

	err := server.service.DiscardRun(request.Context(), app.DiscardRunCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		RunID:    domain.TemplateRunID(request.PathValue("run_id")),
		Reason:   body.Reason,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	response.WriteHeader(http.StatusNoContent)
}

// setLocation points a 201 or 202 at the URL that reads back the resource it
// created or set working, under the request's tenant. An installed template and a credential have no
// read of their own, so their responses carry none.
func setLocation(response http.ResponseWriter, request *http.Request, collection, id string) {
	response.Header().Set("Location", "/v1/tenants/"+url.PathEscape(request.PathValue("tenant_id"))+"/"+collection+"/"+url.PathEscape(id))
}

func stackTemplateConfigJSON(response http.ResponseWriter, config map[string]any) (json.RawMessage, bool) {
	if config == nil {
		config = map[string]any{}
	}
	// config was decoded from JSON into a map, so encoding it again can't
	// fail on anything the caller sent: a failure is the server's.
	configJSON, err := json.Marshal(config)
	if err != nil {
		writeAppError(response, fmt.Errorf("encode stack template config: %w", err))
		return nil, false
	}
	return configJSON, true
}

type registerTemplateRequest struct {
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
	SourceRef string `json:"source_ref"`
	RootPath  string `json:"root_path"`
}

type createStackRequest struct {
	Name                 string            `json:"name"`
	Slug                 string            `json:"slug"`
	Tags                 map[string]string `json:"tags"`
	DefaultCredentialIDs []string          `json:"default_credential_ids"`
}

type addTemplateToStackRequest struct {
	TemplateRevisionID string         `json:"template_revision_id"`
	ComponentKey       string         `json:"component_key"`
	Config             map[string]any `json:"config"`
}

type updateStackTemplateConfigRequest struct {
	Config map[string]any `json:"config"`
}

type changeStackTemplateRevisionRequest struct {
	TargetTemplateRevisionID string         `json:"target_template_revision_id"`
	Config                   map[string]any `json:"config"`
}

type startTemplateRunRequest struct {
	Operation string `json:"operation"`
	// AutoApprove applies straight away, with no saved plan and no approval.
	// Only an apply run takes it, and it needs approve access on the stack as
	// well as operate access.
	AutoApprove bool `json:"auto_approve"`
}

type credentialRequest struct {
	// Name is the provider environment variable name; Value is accepted only on create.
	Name  string `json:"name"`
	Value string `json:"value"`
	// Sensitive is nil when the field is left out, which the app stores as
	// sensitive: a missing value must never mean "show it back".
	Sensitive *bool `json:"sensitive"`
}

type approveRunRequest struct{}

type startTemplateSyncRequest struct{}

type discardRunRequest struct {
	Reason string `json:"reason"`
}

type stackViewResponse struct {
	Stack     stackResponse           `json:"stack"`
	Templates []stackTemplateResponse `json:"templates"`
}

type stackResponse struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	// SlugIsDerived is true when the slug is the one the name gives, so a
	// screen can show the slug only when someone chose it.
	SlugIsDerived         bool                      `json:"slug_is_derived"`
	Status                string                    `json:"status"`
	Tags                  map[string]string         `json:"tags"`
	DefaultCredentialIDs  []string                  `json:"default_credential_ids"`
	CreatedBy             string                    `json:"created_by"`
	CreatedAt             string                    `json:"created_at"`
	EffectiveCapabilities stackCapabilitiesResponse `json:"effective_capabilities"`
}

// stackListItemResponse is a stack as the stacks index lists it: the stack,
// and how many templates it has installed.
type stackListItemResponse struct {
	stackResponse
	TemplateCount int `json:"template_count"`
}

// attentionItemResponse is one installed template that needs a person.
type attentionItemResponse struct {
	// Kind is waiting_approval, destroy_failed or run_failed.
	Kind string `json:"kind"`
	// At is when the item began to need a person; omitted when unknown.
	At            string                         `json:"at,omitempty"`
	Stack         attentionStackResponse         `json:"stack"`
	StackTemplate attentionStackTemplateResponse `json:"stack_template"`
	// Run is the plan waiting for approval, the destroy that failed, or the
	// plan or apply that failed; null for a failed template with no destroy
	// run on record.
	Run *templateRunResponse `json:"run"`
}

type attentionStackResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type attentionStackTemplateResponse struct {
	ID string `json:"id"`
	// WorkspaceName names the template when DisplayName is empty, as it does
	// on the stack's own pages.
	WorkspaceName string `json:"workspace_name"`
	DisplayName   string `json:"display_name"`
}

func newAttentionItemResponse(item app.AttentionItem) attentionItemResponse {
	response := attentionItemResponse{
		Kind: string(item.Kind),
		Stack: attentionStackResponse{
			ID:   string(item.Stack.ID),
			Name: item.Stack.Name,
			Slug: item.Stack.Slug,
		},
		StackTemplate: attentionStackTemplateResponse{
			ID:            string(item.StackTemplate.ID),
			WorkspaceName: item.StackTemplate.WorkspaceName,
			DisplayName:   item.StackTemplate.DisplayName,
		},
	}
	if item.Run != nil {
		run := newTemplateRunResponse(*item.Run)
		response.Run = &run
	}
	if !item.At.IsZero() {
		response.At = item.At.Format(time.RFC3339Nano)
	}
	return response
}

type stackCapabilitiesResponse struct {
	CanView         bool `json:"can_view"`
	CanOperate      bool `json:"can_operate"`
	CanApprove      bool `json:"can_approve"`
	CanManageAccess bool `json:"can_manage_access"`
}

type stackTemplateResponse struct {
	ID                            string `json:"id"`
	StackID                       string `json:"stack_id"`
	ComponentKey                  string `json:"component_key"`
	SourceTemplateID              string `json:"source_template_id"`
	DesiredTemplateRevisionID     string `json:"desired_template_revision_id"`
	LastAppliedTemplateRevisionID string `json:"last_applied_template_revision_id"`
	// SourceRef is the desired revision's ref, resolved per request. It is a
	// label, not component state — the component does not own a ref.
	SourceRef        string         `json:"source_ref"`
	WorkspaceName    string         `json:"workspace_name"`
	DisplayName      string         `json:"display_name"`
	Config           map[string]any `json:"config"`
	LastAppliedRunID string         `json:"last_applied_run_id"`
	LastAppliedAt    string         `json:"last_applied_at,omitempty"`
	PendingPlanRunID string         `json:"pending_plan_run_id"`
	PendingPlanAt    string         `json:"pending_plan_at,omitempty"`
	// PlanState and LiveState are the two derived comparisons. The snapshot
	// configs they are derived from are deliberately not returned: the client
	// re-deriving the comparison is the mistake this replaced.
	PlanState string `json:"plan_state"`
	LiveState string `json:"live_state"`
	CreatedBy string `json:"created_by"`
	Lifecycle string `json:"lifecycle"`
}

func newStackViewResponse(view app.StackView) stackViewResponse {
	templates := make([]stackTemplateResponse, 0, len(view.Templates))
	for _, stackTemplate := range view.Templates {
		templates = append(templates, newStackTemplateResponse(stackTemplate))
	}
	return stackViewResponse{
		Stack:     newStackResponse(view.Stack, view.Capabilities),
		Templates: templates,
	}
}

func newStackResponse(stack domain.Stack, capabilities app.StackCapabilities) stackResponse {
	credentialIDs := make([]string, 0, len(stack.DefaultCredentialIDs))
	for _, id := range stack.DefaultCredentialIDs {
		credentialIDs = append(credentialIDs, string(id))
	}

	tags := stack.Tags
	if tags == nil {
		tags = map[string]string{}
	}

	// A stack a repository hands back without a status is ready, the only
	// status there is.
	status := string(stack.Status)
	if status == "" {
		status = string(domain.StackStatusReady)
	}

	return stackResponse{
		ID:                   string(stack.ID),
		TenantID:             string(stack.TenantID),
		Name:                 stack.Name,
		Slug:                 stack.Slug,
		SlugIsDerived:        app.SlugIsDerived(stack.Name, stack.Slug),
		Status:               status,
		Tags:                 tags,
		DefaultCredentialIDs: credentialIDs,
		CreatedBy:            string(stack.CreatedBy),
		CreatedAt:            stack.CreatedAt.Format(time.RFC3339Nano),
		EffectiveCapabilities: stackCapabilitiesResponse{
			CanView:         capabilities.CanView,
			CanOperate:      capabilities.CanOperate,
			CanApprove:      capabilities.CanApprove,
			CanManageAccess: capabilities.CanManageAccess,
		},
	}
}

// newStackTemplateResponse takes the view rather than the record because two of
// the fields it returns — display_name and source_ref — are resolved per read
// from the desired revision and are not component state.
func newStackTemplateResponse(view app.StackTemplateView) stackTemplateResponse {
	stackTemplate := view.StackTemplate
	var config map[string]any
	configJSON := stackTemplate.DesiredConfig()
	if len(configJSON) > 0 {
		_ = json.Unmarshal(configJSON, &config)
	}
	if config == nil {
		config = map[string]any{}
	}

	response := stackTemplateResponse{
		ID:                            string(stackTemplate.ID),
		StackID:                       string(stackTemplate.StackID),
		ComponentKey:                  stackTemplate.ComponentKey,
		SourceTemplateID:              string(stackTemplate.SourceTemplateID),
		DesiredTemplateRevisionID:     string(stackTemplate.DesiredTemplateRevisionID),
		LastAppliedTemplateRevisionID: string(stackTemplate.LastAppliedTemplateRevisionID),
		SourceRef:                     view.SourceRef,
		WorkspaceName:                 stackTemplate.WorkspaceName,
		DisplayName:                   view.DisplayName,
		Config:                        config,
		LastAppliedRunID:              string(stackTemplate.LastAppliedRunID),
		PendingPlanRunID:              string(stackTemplate.PendingPlanRunID),
		PlanState:                     string(stackTemplate.PlanState()),
		LiveState:                     string(stackTemplate.LiveState()),
		CreatedBy:                     string(stackTemplate.CreatedBy),
		Lifecycle:                     string(stackTemplate.Lifecycle),
	}
	if !stackTemplate.LastAppliedAt.IsZero() {
		response.LastAppliedAt = stackTemplate.LastAppliedAt.Format(time.RFC3339Nano)
	}
	if !stackTemplate.PendingPlanAt.IsZero() {
		response.PendingPlanAt = stackTemplate.PendingPlanAt.Format(time.RFC3339Nano)
	}
	return response
}

func (server *Server) handleListStackGrants(response http.ResponseWriter, request *http.Request) {
	result, err := server.service.ListStackGrants(request.Context(), app.ListStackGrantsCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		StackID:  domain.StackID(request.PathValue("stack_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(result.Grants, ""))
}

func (server *Server) handleAssignStackRole(response http.ResponseWriter, request *http.Request) {
	var body assignRoleRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	result, err := server.service.AssignStackRole(request.Context(), app.AssignStackRoleCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		StackID:  domain.StackID(request.PathValue("stack_id")),
		UserSub:  request.PathValue("user_sub"),
		Role:     body.Role,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	// No Location: the grant's URL is the one just requested.
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(response, status, result.Grant)
}

// The person is named by the path alone, so the body has no user_sub to
// disagree with it, and one that sends it is refused as an unknown field. The
// stack and platform grant routes both take it.
type assignRoleRequest struct {
	Role string `json:"role"`
}

func (server *Server) handleRevokeStackRole(response http.ResponseWriter, request *http.Request) {
	err := server.service.RevokeStackRole(request.Context(), app.RevokeStackRoleCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		StackID:  domain.StackID(request.PathValue("stack_id")),
		UserSub:  request.PathValue("user_sub"),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	response.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleListPlatformGrants(response http.ResponseWriter, request *http.Request) {
	result, err := server.service.ListPlatformGrants(request.Context(), app.ListPlatformGrantsCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	writeJSON(response, http.StatusOK, newListResponse(result.Grants, ""))
}

func (server *Server) handleAssignPlatformRole(response http.ResponseWriter, request *http.Request) {
	var body assignRoleRequest
	if !decodeRequestBody(response, request, &body) {
		return
	}

	result, err := server.service.AssignPlatformRole(request.Context(), app.AssignPlatformRoleCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		UserSub:  request.PathValue("user_sub"),
		Role:     body.Role,
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	// No Location: the grant's URL is the one just requested.
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(response, status, result.Grant)
}

func (server *Server) handleRevokePlatformRole(response http.ResponseWriter, request *http.Request) {
	err := server.service.RevokePlatformRole(request.Context(), app.RevokePlatformRoleCommand{
		TenantID: domain.TenantID(request.PathValue("tenant_id")),
		UserSub:  request.PathValue("user_sub"),
	})
	if err != nil {
		writeAppError(response, err)
		return
	}

	response.WriteHeader(http.StatusNoContent)
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	// The status is already sent, so a failed encode has no one left to tell.
	_ = json.NewEncoder(response).Encode(body) //nolint:errchkjson // see above
}

type queueItemResponse struct {
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Summary     string     `json:"summary"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error"`
	CreatedAt   time.Time  `json:"created_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

// handleListQueue returns the work items the caller queued. Scoping is by actor
// alone, so no new permission concept is introduced: a caller only ever sees
// their own items.
func (server *Server) handleListQueue(response http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.Subject == "" {
		writeAppError(response, app.ErrUnauthenticated)
		return
	}
	if server.queue == nil {
		writeProblem(response, problemDocument{
			Type: problemTypeURI("queue-unavailable"), Title: "The queue isn't configured",
			Status: http.StatusServiceUnavailable, Detail: "The API was started without a queue reader. An operator has to configure one.",
		})
		return
	}

	page, ok := readPageRequest(response, request)
	if !ok {
		return
	}
	if err := page.Validate(); err != nil {
		writeAppError(response, err)
		return
	}
	after, err := app.DecodeCursor[queue.Cursor](queueList, page.Cursor)
	if err != nil {
		writeAppError(response, err)
		return
	}

	// One more than the page, which only proves a next page exists.
	statuses, err := server.queue.ListByActor(request.Context(), request.PathValue("tenant_id"), principal.Subject, after, page.Limit+1)
	if err != nil {
		writeAppError(response, fmt.Errorf("list queue: %w", err))
		return
	}
	nextCursor := ""
	if len(statuses) > page.Limit {
		statuses = statuses[:page.Limit]
		last := statuses[len(statuses)-1]
		nextCursor = app.EncodeCursor(queueList, queue.Cursor{CreatedAt: last.CreatedAt, ID: last.ID})
	}

	items := make([]queueItemResponse, 0, len(statuses))
	for _, status := range statuses {
		summary := status.Summary
		if summary == "" {
			summary = string(status.Kind)
		}
		items = append(items, queueItemResponse{
			Kind:        string(status.Kind),
			State:       string(status.State),
			Summary:     summary,
			Attempts:    status.Attempts,
			LastError:   status.LastError,
			CreatedAt:   status.CreatedAt,
			DeliveredAt: status.DeliveredAt,
		})
	}
	writeJSON(response, http.StatusOK, newListResponse(items, nextCursor))
}

// queueList names the queue list's cursors.
const queueList = "queue"

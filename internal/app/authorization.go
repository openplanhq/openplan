package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/authorization"
	"github.com/vishu42/openplan/internal/domain"
)

func requirePrincipal(ctx context.Context) (authentication.Principal, error) {
	principal, ok := authentication.PrincipalFromContext(ctx)
	if !ok || principal.Subject == "" {
		return authentication.Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

// requirePrincipalAndAuthorizer checks both preconditions of an authorization
// decision and returns the principal the caller will ask about. It decides
// nothing itself.
//
// The order is deliberate: authentication is checked before configuration, so
// an anonymous caller never learns the deployment is misconfigured. A nil
// authorizer is ErrUnavailable rather than ErrForbidden, because "I cannot
// tell" and "you may not" are different answers and only one of them is true.
//
//	authenticated, authorizer wired  → principal, nil
//	no principal, or empty Subject   → ErrUnauthenticated       (even if unwired)
//	authenticated, authorizer nil    → error (never a decision)
func requirePrincipalAndAuthorizer(ctx context.Context, auth *authorization.Authorization) (authentication.Principal, error) {
	principal, err := requirePrincipal(ctx)
	if err != nil {
		return authentication.Principal{}, err
	}
	if auth == nil {
		return authentication.Principal{}, fmt.Errorf("authorization not configured")
	}
	return principal, nil
}

// authorizePlatform answers "may the request's principal do relation to the
// platform singleton?" -- the global questions that used to be answered from
// IdP role claims.
//
// The relation is always a capability (can_create_stack, can_read_template),
// never a tier. Which tier satisfies a capability is the model's business, so
// re-tiering one is a model deploy that never touches this package.
//
//	principal holds platform editor, RelationCanCreateStack   → nil
//	principal holds platform viewer, RelationCanCreateStack   → ErrForbidden
//	principal holds platform viewer, RelationCanReadTemplate  → nil
//	OpenFGA unreachable                                       → error (never a decision)
func authorizePlatform(ctx context.Context, auth *authorization.Authorization, relation authorization.Relation) error {
	allowed, err := checkPlatform(ctx, auth, relation)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// checkPlatform is authorizePlatform for callers that branch on the answer
// rather than refusing, which is the administrator list bypass below.
func checkPlatform(ctx context.Context, auth *authorization.Authorization, relation authorization.Relation) (bool, error) {
	principal, err := requirePrincipalAndAuthorizer(ctx, auth)
	if err != nil {
		return false, err
	}
	return auth.Can(ctx, principal.Subject, relation, authorization.Platform)
}

// authorizeStack answers "may the request's principal do relation to this
// stack?" for reads and writes alike. A caller who can't view the stack is told
// it isn't there, so a refused write never confirms that a stack the caller
// can't see exists; only a caller who can see it learns they may not act.
//
// Both questions go in one BatchCheck rather than two round trips. Asking
// can_view alone when it is the relation keeps reads at one check.
//
// A platform administrator is not special-cased here. The model derives it:
// can_manage_access includes "can_administer from parent", so the same Check
// that answers for an owner answers for an administrator.
//
//	stack owned by alice, principal alice, RelationCanOperate  → nil
//	stack owned by alice, principal bob, RelationCanOperate    → ErrNotFound
//	bob is a viewer of it, RelationCanOperate                  → ErrForbidden
//	principal holds platform admin, any linked stack           → nil (from the model)
//	stackID = "bad:id"                                         → ErrNotFound
//	OpenFGA unreachable                                        → error (never a decision)
func authorizeStack(ctx context.Context, auth *authorization.Authorization, stackID domain.StackID, relation authorization.Relation) error {
	if _, err := requirePrincipalAndAuthorizer(ctx, auth); err != nil {
		return err
	}
	object, err := authorization.ObjectFromID(authorization.TypeStack, string(stackID))
	if errors.Is(err, authorization.ErrInvalidInput) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	relations := []authorization.Relation{authorization.RelationCanView}
	if relation != authorization.RelationCanView {
		relations = append(relations, relation)
	}
	allowed, err := batchCheckObject(ctx, auth, object, relations)
	if err != nil {
		return err
	}
	if !allowed[0] {
		return ErrNotFound
	}
	if !allowed[len(allowed)-1] {
		return ErrForbidden
	}
	return nil
}

// listAccessibleStacks is every stack the caller can view, newest first, for
// callers that need the whole set, such as attention.
func listAccessibleStacks(ctx context.Context, auth *authorization.Authorization, repository StackRepository, tenantID domain.TenantID) ([]domain.Stack, error) {
	return accessibleStacks(ctx, auth, repository, tenantID, nil, "", 0)
}

// stacksList names the stacks list's cursors.
const stacksList = "stacks"

// listAccessibleStacksPage is one page of the stacks the caller can view.
// Unlike listAccessibleStacks it stops walking the table once the page is
// full, plus one stack to prove a next page exists, so a request costs the
// BatchChecks its page needs rather than one per 50 stacks in the tenant.
func listAccessibleStacksPage(ctx context.Context, auth *authorization.Authorization, repository StackRepository, tenantID domain.TenantID, page PageRequest, query string) (Page[domain.Stack], error) {
	after, err := DecodeCursor[StackPageCursor](stacksList, page.Cursor)
	if err != nil {
		return Page[domain.Stack]{}, err
	}
	stacks, err := accessibleStacks(ctx, auth, repository, tenantID, after, query, page.Limit+1)
	if err != nil {
		return Page[domain.Stack]{}, err
	}
	return pageFrom(stacks, page.Limit, stacksList, func(stack domain.Stack) cursorKey {
		return StackPageCursor{CreatedAt: stack.CreatedAt, ID: stack.ID}
	}), nil
}

// accessibleStacks walks the tenant's stacks newest first from after, keeping
// those the caller can view, until it holds want of them or the table ends;
// want <= 0 keeps going to the end. query, when set, narrows the walk to
// stacks whose name or slug contains it.
//
//nolint:gocognit // Authorization logic; split it only as a reviewed refactor.
func accessibleStacks(
	ctx context.Context,
	auth *authorization.Authorization,
	repository StackRepository,
	tenantID domain.TenantID,
	after *StackPageCursor,
	query string,
	want int,
) ([]domain.Stack, error) {
	principal, err := requirePrincipalAndAuthorizer(ctx, auth)
	if err != nil {
		return nil, err
	}
	// Unlike authorizeStack, the bypass is kept here rather than left to the
	// model: one Check replaces a BatchCheck fan-out over every stack in the
	// tenant, all of which the model would answer true anyway.
	administrator, err := checkPlatform(ctx, auth, authorization.RelationCanAdminister)
	if err != nil {
		return nil, err
	}
	if administrator {
		if want <= 0 && after == nil && query == "" {
			return repository.ListStacks(ctx, tenantID)
		}
		if want > 0 {
			return repository.ListStacksPage(ctx, tenantID, after, want, query)
		}
	}
	// Candidates are read 50 at a time whatever the page size, to keep each
	// BatchCheck a sane size. An administrator only gets here to drain a
	// filtered or resumed walk, and every candidate is visible to them.
	const pageSize = 50
	cursor := after
	var accessible []domain.Stack
	for {
		candidates, err := repository.ListStacksPage(ctx, tenantID, cursor, pageSize, query)
		if err != nil {
			return nil, fmt.Errorf("list stack candidates: %w", err)
		}
		if len(candidates) == 0 {
			return accessible, nil
		}
		if len(candidates) > pageSize {
			return nil, fmt.Errorf("stack candidate page exceeds limit")
		}
		if cursor != nil && !stackPageOrderBefore(domain.Stack{ID: cursor.ID, CreatedAt: cursor.CreatedAt}, candidates[0]) {
			return nil, fmt.Errorf("stack candidate page did not advance")
		}
		for i := 1; i < len(candidates); i++ {
			if !stackPageOrderBefore(candidates[i-1], candidates[i]) {
				return nil, fmt.Errorf("stack candidate page is not strictly ordered")
			}
		}

		allowed, err := visibleStackCandidates(ctx, auth, principal.Subject, candidates, administrator)
		if err != nil {
			return nil, err
		}
		for i, visible := range allowed {
			if visible {
				accessible = append(accessible, candidates[i])
			}
		}
		// Stopping mid-batch is safe: the next page resumes after the last
		// stack returned, so the unreturned candidates are read again.
		if want > 0 && len(accessible) >= want {
			return accessible[:want], nil
		}
		if len(candidates) < pageSize {
			return accessible, nil
		}
		last := candidates[len(candidates)-1]
		cursor = &StackPageCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
}

// visibleStackCandidates answers can_view for each candidate in one
// BatchCheck. An administrator can view every stack, so needs none.
func visibleStackCandidates(ctx context.Context, auth *authorization.Authorization, subject string, candidates []domain.Stack, administrator bool) ([]bool, error) {
	if administrator {
		allowed := make([]bool, len(candidates))
		for i := range allowed {
			allowed[i] = true
		}
		return allowed, nil
	}
	checks := make([]authorization.Check, len(candidates))
	for i, candidate := range candidates {
		object, err := authorization.ObjectFromID(authorization.TypeStack, string(candidate.ID))
		if errors.Is(err, authorization.ErrInvalidInput) {
			return nil, fmt.Errorf("stack candidate has invalid ID")
		}
		if err != nil {
			return nil, err
		}
		checks[i] = authorization.Check{Relation: authorization.RelationCanView, Object: object}
	}
	allowed, err := auth.CanAll(ctx, subject, checks)
	if err != nil {
		return nil, err
	}
	if len(allowed) != len(candidates) {
		return nil, fmt.Errorf("batch result count does not match stack candidates")
	}
	return allowed, nil
}

func stackPageOrderBefore(left, right domain.Stack) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.After(right.CreatedAt)
	}
	return left.ID > right.ID
}

// authorizedStackTemplate loads a stack template and authorizes relation on the
// stack that owns it. A template that doesn't exist is ErrNotFound, the same
// answer as one the caller can't view, so the two can't be told apart.
func (service *Service) authorizedStackTemplate(ctx context.Context, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID, relation authorization.Relation) (domain.StackTemplate, error) {
	// Fails an unauthenticated or unconfigured request before the repository
	// read; authorizeStack below re-derives the principal for the Check.
	if _, err := requirePrincipalAndAuthorizer(ctx, service.Authorization); err != nil {
		return domain.StackTemplate{}, err
	}
	stackTemplate, err := service.StackTemplates.GetStackTemplate(ctx, tenantID, stackTemplateID)
	if err != nil {
		return domain.StackTemplate{}, err
	}
	if _, err := authorization.ObjectFromID(authorization.TypeStack, string(stackTemplate.StackID)); errors.Is(err, authorization.ErrInvalidInput) {
		return domain.StackTemplate{}, fmt.Errorf("stack template has invalid owning stack ID")
	} else if err != nil {
		return domain.StackTemplate{}, err
	}
	if err := authorizeStack(ctx, service.Authorization, stackTemplate.StackID, relation); err != nil {
		return domain.StackTemplate{}, err
	}
	return stackTemplate, nil
}

// operableStackTemplate is authorizedStackTemplate for the mutating commands:
// the same lookup, with the refusal recorded and wrapped the one way all of
// them want it. The read-only callers deliberately do not use it -- they write
// no audit event. An unseen or missing template is still audited: it is a
// refused write, whatever the client is told.
func (service *Service) operableStackTemplate(
	ctx context.Context,
	actor domain.UserID,
	tenantID domain.TenantID,
	stackTemplateID domain.StackTemplateID,
) (domain.StackTemplate, error) {
	stackTemplate, err := service.authorizedStackTemplate(ctx, tenantID, stackTemplateID, authorization.RelationCanOperate)
	if err != nil {
		// No StackID: the refusal can precede resolving which stack owns the
		// template, so naming one here would sometimes be a guess.
		service.auditFailedAccess(ctx, actor, tenantID, "")
		return domain.StackTemplate{}, fmt.Errorf("get stack template: %w", err)
	}
	return stackTemplate, nil
}

// PlatformCapabilities is the global half of what GET /v1/me projects. The
// field names keep the wire contract the web client already reads; what
// changed is where the answers come from.
type PlatformCapabilities struct {
	CanCreateStack          bool
	CanManageTemplates      bool
	CanManagePlatformAccess bool
}

// platformCapabilityRelations is the order a platform BatchCheck is built in,
// and platformCapabilitiesFrom is the decoding of its results. The two are
// positional and must agree, so they are kept adjacent rather than written out
// separately at the point of use.
var platformCapabilityRelations = []authorization.Relation{
	authorization.RelationCanCreateStack,
	authorization.RelationCanManageTemplates,
	authorization.RelationCanManagePlatformAccess,
}

func platformCapabilitiesFrom(results []bool) PlatformCapabilities {
	return PlatformCapabilities{
		CanCreateStack:          results[0],
		CanManageTemplates:      results[1],
		CanManagePlatformAccess: results[2],
	}
}

// ResolvePlatformCapabilities answers every global question in one BatchCheck.
// An unauthenticated or unconfigured caller is not an error here: /v1/me is
// reachable before any tuple exists, and a principal that holds nothing is a
// legitimate answer rather than a failure.
func ResolvePlatformCapabilities(ctx context.Context, auth *authorization.Authorization) (PlatformCapabilities, error) {
	results, err := batchCheckObject(ctx, auth, authorization.Platform, platformCapabilityRelations)
	if err != nil {
		return PlatformCapabilities{}, err
	}
	return platformCapabilitiesFrom(results), nil
}

type StackCapabilities struct {
	CanView         bool
	CanOperate      bool
	CanApprove      bool
	CanManageAccess bool
}

// stackCapabilityRelations and stackCapabilitiesFrom are the request order and
// the result decoding for a stack BatchCheck. Both resolvers below share them,
// so a reordering cannot reach one caller and miss the other -- which, while
// each resolver spelled the order out for itself, would have silently returned
// the wrong permissions rather than failing.
var stackCapabilityRelations = []authorization.Relation{
	authorization.RelationCanView,
	authorization.RelationCanOperate,
	authorization.RelationCanApprove,
	authorization.RelationCanManageAccess,
}

func stackCapabilitiesFrom(results []bool) StackCapabilities {
	return StackCapabilities{
		CanView:         results[0],
		CanOperate:      results[1],
		CanApprove:      results[2],
		CanManageAccess: results[3],
	}
}

// batchCheckObject asks every relation about one object in one round trip and
// returns the answers in request order. A response of the wrong length fails
// the call: the callers decode it positionally, so a short or long result would
// otherwise be read as the wrong permission rather than as a failure.
func batchCheckObject(
	ctx context.Context,
	auth *authorization.Authorization,
	object authorization.Object,
	relations []authorization.Relation,
) ([]bool, error) {
	principal, err := requirePrincipalAndAuthorizer(ctx, auth)
	if err != nil {
		return nil, err
	}
	checks := make([]authorization.Check, len(relations))
	for i, relation := range relations {
		checks[i] = authorization.Check{Relation: relation, Object: object}
	}
	results, err := auth.CanAll(ctx, principal.Subject, checks)
	if err != nil {
		return nil, err
	}
	if len(results) != len(checks) {
		return nil, fmt.Errorf("batch result count does not match checks")
	}
	return results, nil
}

func ResolveStackCapabilities(ctx context.Context, auth *authorization.Authorization, stackID domain.StackID) (StackCapabilities, error) {
	// Checked before the stack ID is parsed, so an anonymous caller still gets
	// ErrUnauthenticated rather than a complaint about the ID. batchCheckObject
	// repeats this; it is a pure read of the context.
	if _, err := requirePrincipalAndAuthorizer(ctx, auth); err != nil {
		return StackCapabilities{}, err
	}
	object, err := authorization.ObjectFromID(authorization.TypeStack, string(stackID))
	if err != nil {
		return StackCapabilities{}, err
	}
	results, err := batchCheckObject(ctx, auth, object, stackCapabilityRelations)
	if err != nil {
		return StackCapabilities{}, err
	}
	return stackCapabilitiesFrom(results), nil
}

func ResolveStacksCapabilities(ctx context.Context, auth *authorization.Authorization, stacks []domain.Stack) (map[domain.StackID]StackCapabilities, error) {
	if len(stacks) == 0 {
		return map[domain.StackID]StackCapabilities{}, nil
	}
	principal, err := requirePrincipalAndAuthorizer(ctx, auth)
	if err != nil {
		return nil, err
	}
	administrator, err := checkPlatform(ctx, auth, authorization.RelationCanAdminister)
	if err != nil {
		return nil, err
	}
	// Kept for the same reason as listAccessibleStacks: stacks arrives from an
	// unbounded ListStacks, so this collapses four checks per stack in the
	// tenant into one.
	if administrator {
		all := StackCapabilities{CanView: true, CanOperate: true, CanApprove: true, CanManageAccess: true}
		result := make(map[domain.StackID]StackCapabilities, len(stacks))
		for _, s := range stacks {
			result[s.ID] = all
		}
		return result, nil
	}
	checks := make([]authorization.Check, 0, len(stacks)*len(stackCapabilityRelations))
	for _, s := range stacks {
		object, err := authorization.ObjectFromID(authorization.TypeStack, string(s.ID))
		if err != nil {
			return nil, err
		}
		for _, relation := range stackCapabilityRelations {
			checks = append(checks, authorization.Check{Relation: relation, Object: object})
		}
	}
	results, err := auth.CanAll(ctx, principal.Subject, checks)
	if err != nil {
		return nil, err
	}
	if len(results) != len(checks) {
		return nil, fmt.Errorf("batch result count does not match checks")
	}
	// One stack's relations occupy one contiguous run, in the order they were
	// appended above, so each run decodes with the same function the
	// single-stack resolver uses.
	caps := make(map[domain.StackID]StackCapabilities, len(stacks))
	for i, s := range stacks {
		base := i * len(stackCapabilityRelations)
		caps[s.ID] = stackCapabilitiesFrom(results[base : base+len(stackCapabilityRelations)])
	}
	return caps, nil
}

// authorizedTemplateRun is authorizedStackTemplate for a run: a missing run is
// ErrNotFound, and the run's stack template decides the rest.
func (service *Service) authorizedTemplateRun(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, relation authorization.Relation) (domain.TemplateRun, error) {
	if _, err := requirePrincipalAndAuthorizer(ctx, service.Authorization); err != nil {
		return domain.TemplateRun{}, err
	}
	run, err := service.TemplateRuns.GetTemplateRun(ctx, tenantID, runID)
	if err != nil {
		return domain.TemplateRun{}, err
	}
	if _, err := service.authorizedStackTemplate(ctx, tenantID, run.StackTemplateID, relation); err != nil {
		return domain.TemplateRun{}, err
	}
	return run, nil
}

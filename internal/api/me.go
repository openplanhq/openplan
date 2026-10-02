package api

import (
	"time"

	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/domain"
)

// meResponse is the identity envelope returned by GET /v1/me.
type meResponse struct {
	Sub                string             `json:"sub"`
	DisplayName        string             `json:"displayName"`
	Email              string             `json:"email,omitempty"`
	GlobalCapabilities globalCapabilities `json:"globalCapabilities"`
	TenantID           string             `json:"tenantID"`
	// SessionExpiresAt is when this session ends: the earlier of its idle and
	// absolute bounds, both of which openplan owns. It lets the web client
	// re-authenticate at a quiet moment instead of being interrupted by a 401.
	// It is not a control: the API rejects an expired session regardless of
	// what the browser believes.
	SessionExpiresAt string `json:"sessionExpiresAt,omitempty"`
}

// globalCapabilities encodes coarse-grained permissions answered by OpenFGA.
// The JSON names predate the move off IdP role claims and are kept: they
// are the web client's contract, and only the source of the answers changed.
type globalCapabilities struct {
	IsPlatformAdmin bool `json:"isPlatformAdmin"`
	CanCreateStack  bool `json:"canCreateStack"`
	// CanPublishTemplate gates registering a template, which is also what the
	// template detail screen's Sync button posts — so the button is hidden
	// rather than left to earn a 403.
	CanPublishTemplate bool `json:"canPublishTemplate"`
}

// meFromPrincipal maps the authenticated principal and its resolved platform
// capabilities to a meResponse. The capabilities are passed in rather than
// derived here, because answering them is an OpenFGA call the app layer owns.
func meFromPrincipal(principal authentication.Principal, tenantID domain.TenantID, capabilities globalCapabilities) meResponse {
	sessionExpiresAt := ""
	if !principal.ExpiresAt.IsZero() {
		sessionExpiresAt = principal.ExpiresAt.UTC().Format(time.RFC3339)
	}

	return meResponse{
		Sub:                principal.Subject,
		DisplayName:        principal.DisplayName(),
		Email:              principal.Email,
		GlobalCapabilities: capabilities,
		TenantID:           string(tenantID),
		SessionExpiresAt:   sessionExpiresAt,
	}
}

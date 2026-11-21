package authentication

import (
	"cmp"
	"context"
	"time"
)

// Principal is the normalized identity made available to handlers. It is built
// from the session row, whose claims were copied from an ID token verified once
// at the callback. It carries identity only: authorization is answered by
// OpenFGA, so no role claim from the token reaches an access decision.
// ExpiresAt is identity metadata carried through for presentation — it is
// not an authorization input.
type Principal struct {
	Subject           string
	Name              string
	PreferredUsername string
	Email             string
	ExpiresAt         time.Time
}

// DisplayName labels the person from the claims copied onto the session row
// at sign-in.
func (principal Principal) DisplayName() string {
	return displayName(principal.Name, principal.PreferredUsername, principal.Email, principal.Subject)
}

// displayName is the one fallback order for a person's label, shared by the
// verified token and the principal: the same person is labelled in two places
// (the app header from the principal, a grants list from the projection the
// token fills), and the two must agree.
//
// Every claim but sub is optional: a provider is not required to send name,
// preferred_username, or email, and optionalStringClaim deliberately reports an
// absent claim as empty rather than rejecting the token. Falling back to the
// subject last means the answer is never blank — a raw sub is a poor label, but
// it identifies the right person, which an empty string does not.
func displayName(name, preferredUsername, email, subject string) string {
	return cmp.Or(name, preferredUsername, email, subject)
}

type principalContextKey struct{}

func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

// PrincipalFromContext returns the request's principal. One with an empty
// subject names nobody and reads as absent, so no caller has to refuse it
// itself.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	if !ok || principal.Subject == "" {
		return Principal{}, false
	}
	return principal, true
}

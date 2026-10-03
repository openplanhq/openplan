package authentication

import (
	"context"
	"errors"
	"strings"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (VerifiedToken, error) {
	payload, err := v.verifiedPayload(ctx, raw)
	if err != nil {
		return VerifiedToken{}, err
	}
	return v.validatedToken(payload)
}

// verifiedPayload checks a compact JWS against the provider's keys and returns
// the verified payload. It says nothing about the claims inside: ID tokens and
// back-channel logout tokens require different ones, and both need exactly this
// signature check first.
func (v *OIDCVerifier) verifiedPayload(ctx context.Context, raw string) ([]byte, error) {
	if raw == "" || len(raw) > maxTokenBytes || strings.Count(raw, ".") != 2 {
		return nil, ErrInvalidToken
	}
	algorithm, keyID, ok := signingParameters(raw)
	if !ok {
		return nil, ErrInvalidToken
	}
	key, err := v.keyFor(ctx, keyID, algorithm)
	if err != nil {
		return nil, err
	}
	payload, err := jws.Verify([]byte(raw), jws.WithKey(algorithm, key), jws.WithCompact())
	if err != nil {
		return v.payloadAfterSignatureFailure(ctx, raw, keyID, algorithm)
	}
	return payload, nil
}

func (v *OIDCVerifier) payloadAfterSignatureFailure(ctx context.Context, raw, keyID string, algorithm jwa.SignatureAlgorithm) ([]byte, error) {
	key, refreshErr, err := v.refreshedKeyFor(ctx, keyID, algorithm)
	cooledDown := errors.Is(refreshErr, errRefreshCooldown)
	if err != nil {
		return nil, err
	}
	if refreshErr != nil && !cooledDown {
		return nil, ErrVerifierUnavailable
	}

	payload, err := jws.Verify([]byte(raw), jws.WithKey(algorithm, key), jws.WithCompact())
	if err != nil && cooledDown {
		return nil, ErrVerifierUnavailable
	}
	if err != nil {
		return nil, ErrInvalidToken
	}
	return payload, nil
}

// signingParameters reads the algorithm and key ID from the one protected
// header of a compact JWS, refusing a token that names no key or an algorithm
// outside the asymmetric set a provider signs with.
func signingParameters(raw string) (jwa.SignatureAlgorithm, string, bool) {
	refused := jwa.EmptySignatureAlgorithm()
	message, err := jws.Parse([]byte(raw), jws.WithCompact())
	if err != nil {
		return refused, "", false
	}
	signatures := message.Signatures()
	if len(signatures) != 1 || signatures[0] == nil || signatures[0].ProtectedHeaders() == nil {
		return refused, "", false
	}
	header := signatures[0].ProtectedHeaders()
	provided, hasAlgorithm := header.Algorithm()
	keyID, hasKeyID := header.KeyID()
	if !hasAlgorithm || !hasKeyID || keyID == "" {
		return refused, "", false
	}

	switch provided.String() {
	case "RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "ES512", "EdDSA":
	default:
		return refused, "", false
	}
	algorithm, ok := jwa.LookupSignatureAlgorithm(provided.String())
	return algorithm, keyID, ok
}

// parseClaims reads the claims out of a payload verifiedPayload has already
// authenticated, holding them to what any token from this provider must
// satisfy -- issuer, audience, and the time claims -- plus the claims the
// caller requires.
func (v *OIDCVerifier) parseClaims(payload []byte, required ...string) (jwt.Token, error) {
	v.mu.RLock()
	issuer := v.discovery.Issuer
	v.mu.RUnlock()

	options := []jwt.ParseOption{
		// The signature is already verified; this parses claims from the
		// verified payload.
		jwt.WithVerify(false),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(v.cfg.Audience),
		jwt.WithClock(jwt.ClockFunc(v.cfg.Clock)),
		jwt.WithAcceptableSkew(clockSkew),
	}
	for _, claim := range required {
		options = append(options, jwt.WithRequiredClaim(claim))
	}
	return jwt.Parse(payload, options...)
}

func (v *OIDCVerifier) validatedToken(payload []byte) (VerifiedToken, error) {
	token, err := v.parseClaims(payload, jwt.ExpirationKey)
	if err != nil {
		return VerifiedToken{}, ErrInvalidToken
	}

	var verified VerifiedToken
	var azp string
	for name, target := range map[string]*string{
		"azp":                &azp,
		"name":               &verified.Name,
		"preferred_username": &verified.PreferredUsername,
		"email":              &verified.Email,
		"nonce":              &verified.Nonce,
		"sid":                &verified.SessionID,
	} {
		value, ok := optionalStringClaim(token, name)
		if !ok {
			return VerifiedToken{}, ErrInvalidToken
		}
		*target = value
	}

	// OIDC Core 1.0 §3.1.3.7 steps 4-5: azp, whenever the token carries it,
	// must equal our client ID, and it is additionally *required* once aud
	// carries more than one value. jwt.WithAudience above only asserts aud
	// *contains* the client ID, which is not sufficient on its own once a
	// token minted for a different client can name ours in aud too. That is
	// the substitution azp exists to catch, and it can happen with a
	// single-entry aud, so the claim is checked on every token rather than
	// only on multi-audience ones.
	audiences, _ := token.Audience()
	if (azp != "" && azp != v.cfg.Audience) || (azp == "" && len(audiences) > 1) {
		return VerifiedToken{}, ErrInvalidToken
	}

	var hasSubject, hasExpiry bool
	verified.Subject, hasSubject = token.Subject()
	verified.ExpiresAt, hasExpiry = token.Expiration()
	if !hasSubject || verified.Subject == "" || !hasExpiry {
		return VerifiedToken{}, ErrInvalidToken
	}
	return verified, nil
}

// optionalStringClaim reads a claim the token is not required to carry. An
// absent claim yields an empty value and ok; a claim present with a non-string
// value is malformed and yields !ok. Claims read through it are presentation
// only and never authorization-bearing.
func optionalStringClaim(token jwt.Token, name string) (string, bool) {
	if !token.Has(name) {
		return "", true
	}
	var value string
	if err := token.Get(name, &value); err != nil {
		return "", false
	}
	return value, true
}

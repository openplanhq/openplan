package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vishu42/openplan/internal/authentication"
)

func (store *Store) CreateSession(ctx context.Context, session authentication.Session) error {
	ciphertext, err := store.sessionCipher.Encrypt(session.IDToken)
	if err != nil {
		return fmt.Errorf("create session: encrypt id token: %w", err)
	}
	_, err = store.pool.Exec(ctx, `
		insert into sessions (
			id_hash,
			subject,
			name,
			preferred_username,
			email,
			idp_session_id,
			id_token_ciphertext,
			created_at,
			last_seen_at,
			absolute_expires_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`,
		session.IDHash,
		session.Subject,
		session.Name,
		session.PreferredUsername,
		session.Email,
		session.IDPSessionID,
		ciphertext,
		session.CreatedAt,
		session.LastSeenAt,
		session.AbsoluteExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (store *Store) SessionByHash(ctx context.Context, idHash string) (authentication.Session, error) {
	var (
		session    authentication.Session
		ciphertext string
		revokedAt  *time.Time
	)
	err := store.pool.QueryRow(ctx, `
		select
			id_hash,
			subject,
			name,
			preferred_username,
			email,
			idp_session_id,
			id_token_ciphertext,
			created_at,
			last_seen_at,
			absolute_expires_at,
			revoked_at
		from sessions
		where id_hash = $1
	`, idHash).Scan(
		&session.IDHash,
		&session.Subject,
		&session.Name,
		&session.PreferredUsername,
		&session.Email,
		&session.IDPSessionID,
		&ciphertext,
		&session.CreatedAt,
		&session.LastSeenAt,
		&session.AbsoluteExpiresAt,
		&revokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authentication.Session{}, authentication.ErrSessionNotFound
	}
	if err != nil {
		return authentication.Session{}, fmt.Errorf("session by hash: %w", err)
	}
	if revokedAt != nil {
		session.RevokedAt = *revokedAt
	}
	idToken, err := store.sessionCipher.Decrypt(ciphertext)
	if err != nil {
		return authentication.Session{}, fmt.Errorf("session by hash: decrypt id token: %w", err)
	}
	session.IDToken = idToken
	return session, nil
}

// TouchSession moves the idle bound only. The absolute bound is deliberately
// not touched: it is a cap from sign-in, and extending it here would make it
// unreachable for an active session.
func (store *Store) TouchSession(ctx context.Context, idHash string, seenAt time.Time) error {
	_, err := store.pool.Exec(ctx, `
		update sessions set last_seen_at = $2
		where id_hash = $1 and revoked_at is null
	`, idHash, seenAt)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (store *Store) RevokeSession(ctx context.Context, idHash string, at time.Time) error {
	_, err := store.pool.Exec(ctx, `
		update sessions set revoked_at = $2
		where id_hash = $1 and revoked_at is null
	`, idHash, at)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (store *Store) RevokeSessionsByIDPSessionID(ctx context.Context, idpSessionID string, at time.Time) (int, error) {
	return store.revokeSessions(ctx, "revoke sessions by idp session id", "idp_session_id = $1", idpSessionID, at)
}

func (store *Store) RevokeSessionsBySubject(ctx context.Context, subject string, at time.Time) (int, error) {
	return store.revokeSessions(ctx, "revoke sessions by subject", "subject = $1", subject, at)
}

// revokeSessions revokes the live sessions that where, a constant of the
// caller's, picks by key. An empty key is never a revocation key: an empty
// sid, for one, would match every session from a provider that omits the
// claim.
func (store *Store) revokeSessions(ctx context.Context, label, where, key string, at time.Time) (int, error) {
	if key == "" {
		return 0, nil
	}
	tag, err := store.pool.Exec(ctx, `
		update sessions set revoked_at = $2
		where `+where+` and revoked_at is null
	`, key, at)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	return int(tag.RowsAffected()), nil
}

// DeleteSessionsExpiredBefore drops rows past their absolute bound. Revoked
// rows are left to the same bound rather than deleted on revocation: they are
// already dead to IsLive, and one rule for when a row leaves the table is
// easier to reason about than two.
func (store *Store) DeleteSessionsExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := store.pool.Exec(ctx, `
		delete from sessions where absolute_expires_at < $1
	`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// RevokeSessionsBySubjectWithoutIDPSession revokes this subject's sessions
// that carry no IdP session id. Those rows are unreachable by a sid-keyed
// revoke, so a back-channel logout naming a sid we never stored has no other
// way to reach them; rows that do carry a sid are deliberately left alone,
// since each is addressable by its own.
func (store *Store) RevokeSessionsBySubjectWithoutIDPSession(ctx context.Context, subject string, at time.Time) (int, error) {
	return store.revokeSessions(ctx, "revoke sessions by subject without idp session", "subject = $1 and idp_session_id = ''", subject, at)
}

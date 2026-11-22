package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vishu42/openplan/internal/app"
)

// UpsertUser projects one verified identity.
//
// first_seen_at is written only by the insert branch: the conflict branch
// moves last_seen_at and refreshes the display data, so a returning user keeps
// the moment they first appeared.
func (store *Store) UpsertUser(ctx context.Context, profile app.UserProfile, seenAt time.Time) error {
	_, err := store.pool.Exec(ctx, `
		insert into users (sub, email, display_name, first_seen_at, last_seen_at)
		values ($1, $2, $3, $4, $4)
		on conflict (sub) do update set
			email = excluded.email,
			display_name = excluded.display_name,
			last_seen_at = excluded.last_seen_at
	`, profile.Sub, profile.Email, profile.DisplayName, seenAt)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

// SearchUsers matches display name or email case-insensitively, anywhere in the
// value.
//
// The query is escaped before it reaches LIKE: without that, a caller typing
// "%" or "_" would be writing pattern syntax rather than searching for the
// character they typed, and "%" alone would return the whole table regardless
// of what the caller meant.
func (store *Store) SearchUsers(ctx context.Context, query string, after *app.UserCursor, limit int) ([]app.UserProfile, error) {
	pattern := "%" + escapeLikePattern(query) + "%"
	var afterName, afterSub any
	if after != nil {
		afterName, afterSub = after.DisplayName, after.Sub
	}
	return queryRows(ctx, store.pool, "search users", scanUser, `
		select`+userColumns+`
		from users
		where (display_name ilike $1 escape '\' or email ilike $1 escape '\')
			and ($2::text is null or (display_name, sub) > ($2, $3::text))
		order by display_name, sub
		limit $4
	`, pattern, afterName, afterSub, limit)
}

// UsersBySubs resolves many subjects in one round trip. A subject with no row
// is simply absent from the map: it has never signed in, which the caller
// renders differently from a row whose display name happens to be empty.
func (store *Store) UsersBySubs(ctx context.Context, subs []string) (map[string]app.UserProfile, error) {
	profiles := make(map[string]app.UserProfile, len(subs))
	if len(subs) == 0 {
		return profiles, nil
	}
	users, err := queryRows(ctx, store.pool, "users by subs", scanUser, `
		select`+userColumns+`
		from users
		where sub = any($1)
	`, subs)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		profiles[user.Sub] = user
	}
	return profiles, nil
}

// userColumns are a users row's columns in the order scanUser scans them.
const userColumns = ` sub, display_name, email`

func scanUser(row pgx.Row) (app.UserProfile, error) {
	var user app.UserProfile
	err := row.Scan(&user.Sub, &user.DisplayName, &user.Email)
	return user, err
}

// escapeLikePattern neutralises the LIKE metacharacters so a search term is
// matched as the literal text the caller typed. The backslash is escaped first,
// or it would go on to escape the escapes added after it.
func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	value = strings.ReplaceAll(value, "_", `\_`)
	return value
}

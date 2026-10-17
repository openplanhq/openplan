package app

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/domain"
)

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()

	key := StackPageCursor{CreatedAt: time.Date(2026, 10, 17, 9, 0, 0, 123456000, time.UTC), ID: "stack_1"}
	got, err := DecodeCursor[StackPageCursor]("stacks", EncodeCursor("stacks", key))

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, key.CreatedAt.Equal(got.CreatedAt))
	assert.Equal(t, key.ID, got.ID)
}

func TestDecodeCursorEmptyIsFirstPage(t *testing.T) {
	t.Parallel()

	got, err := DecodeCursor[StackPageCursor]("stacks", "")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeCursorRejects(t *testing.T) {
	t.Parallel()

	valid := StackPageCursor{CreatedAt: time.Now().UTC(), ID: "stack_1"}
	raw := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for name, cursor := range map[string]string{
		"not base64url":  "%%%",
		"not json":       raw("not json"),
		"another list":   EncodeCursor("runs", valid),
		"wrong key type": raw(`{"l":"stacks","k":{"run_number":3}}`),
		"zero key":       raw(`{"l":"stacks","k":{"created_at":"0001-01-01T00:00:00Z","id":""}}`),
		"trailing data":  raw(`{"l":"stacks","k":{"created_at":"2026-10-17T09:00:00Z","id":"s"}} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeCursor[StackPageCursor]("stacks", cursor)
			require.ErrorIs(t, err, ErrInvalidQuery)
			fieldErrs := FieldErrors(err)
			require.Len(t, fieldErrs, 1)
			assert.Equal(t, "cursor", fieldErrs[0].Parameter)
			assert.Equal(t, IssueInvalidFormat, fieldErrs[0].Issue)
		})
	}
}

func TestValidatePageRequest(t *testing.T) {
	t.Parallel()

	for _, limit := range []int{1, DefaultPageLimit, MaxPageLimit} {
		assert.NoError(t, validatePageRequest(PageRequest{Limit: limit}), "limit %d", limit)
	}
	for _, limit := range []int{-1, 0, MaxPageLimit + 1} {
		err := validatePageRequest(PageRequest{Limit: limit})
		require.ErrorIs(t, err, ErrInvalidCommand, "limit %d", limit)
		fieldErrs := FieldErrors(err)
		require.Len(t, fieldErrs, 1)
		assert.Equal(t, "limit", fieldErrs[0].Parameter)
		assert.Equal(t, IssueOutOfRange, fieldErrs[0].Issue)
	}
}

func TestPageFrom(t *testing.T) {
	t.Parallel()

	stack := func(id string) domain.Stack {
		return domain.Stack{ID: domain.StackID(id), CreatedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	}
	key := func(s domain.Stack) cursorKey { return StackPageCursor{CreatedAt: s.CreatedAt, ID: s.ID} }

	// limit+1 rows: the extra row only proves there is a next page.
	page := pageFrom([]domain.Stack{stack("c"), stack("b"), stack("a")}, 2, "stacks", key)
	assert.Equal(t, []domain.Stack{stack("c"), stack("b")}, page.Items)
	next, err := DecodeCursor[StackPageCursor]("stacks", page.NextCursor)
	require.NoError(t, err)
	assert.Equal(t, domain.StackID("b"), next.ID)

	// Exactly limit rows is the last page: no cursor leading to nothing.
	page = pageFrom([]domain.Stack{stack("b"), stack("a")}, 2, "stacks", key)
	assert.Len(t, page.Items, 2)
	assert.Empty(t, page.NextCursor)

	// No rows is an empty list, never nil.
	page = pageFrom[domain.Stack](nil, 2, "stacks", key)
	assert.NotNil(t, page.Items)
	assert.Empty(t, page.NextCursor)
}

func TestInvalidQueryIsASentinel(t *testing.T) {
	t.Parallel()

	assert.True(t, errors.Is(ErrInvalidQuery, ErrInvalidQuery))
	assert.Contains(t, Sentinels(), ErrInvalidQuery)
}

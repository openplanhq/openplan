package api

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/app"
)

func TestListResponseShape(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(newListResponse[string](nil, ""))
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[],"next_cursor":null}`, string(encoded))

	encoded, err = json.Marshal(newListResponse([]string{"a"}, "next"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":["a"],"next_cursor":"next"}`, string(encoded))
}

func TestParsePageRequest(t *testing.T) {
	t.Parallel()

	page, problems := parsePageRequest(url.Values{})
	assert.Empty(t, problems)
	assert.Equal(t, app.PageRequest{Limit: app.DefaultPageLimit}, page)

	page, problems = parsePageRequest(url.Values{"limit": {"7"}, "cursor": {"abc"}})
	assert.Empty(t, problems)
	assert.Equal(t, app.PageRequest{Limit: 7, Cursor: "abc"}, page)

	// Out of range still parses: the range is the app's rule, a 422.
	page, problems = parsePageRequest(url.Values{"limit": {"0"}})
	assert.Empty(t, problems)
	assert.Equal(t, 0, page.Limit)

	_, problems = parsePageRequest(url.Values{"limit": {"abc"}})
	require.Len(t, problems, 1)
	assert.Equal(t, "limit", problems[0].Parameter)
	assert.Equal(t, app.IssueInvalidFormat, problems[0].Issue)
}

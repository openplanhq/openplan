package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/vishu42/openplan/internal/app"
)

// listResponse is the one shape every list endpoint answers with. Lists that
// page fill next_cursor until their last page; the small bounded ones (a
// run's logs, a stack's grants and credentials, attention) always send null,
// so any of them can start paging later without changing the shape.
type listResponse[T any] struct {
	Items []T `json:"items"`
	// NextCursor is sent back as ?cursor= for the next page; null on the last.
	NextCursor *string `json:"next_cursor"`
}

// newListResponse never sends items as null: a Go nil slice would be.
func newListResponse[T any](items []T, nextCursor string) listResponse[T] {
	if items == nil {
		items = []T{}
	}
	response := listResponse[T]{Items: items}
	if nextCursor != "" {
		response.NextCursor = &nextCursor
	}
	return response
}

// mapItems converts a page of domain values into response items.
func mapItems[T, R any](items []T, convert func(T) R) []R {
	converted := make([]R, 0, len(items))
	for _, item := range items {
		converted = append(converted, convert(item))
	}
	return converted
}

// parsePageRequest reads limit and cursor. Only parsing happens here: a limit
// that isn't an integer can't be parsed, which is a 400. Whether a parsed
// limit is allowed, and whether the cursor belongs to the list, are the app's
// rules. With no limit, a page is the default size.
func parsePageRequest(query url.Values) (app.PageRequest, []problemFieldError) {
	page := app.PageRequest{Limit: app.DefaultPageLimit, Cursor: query.Get("cursor")}
	raw := query.Get("limit")
	if raw == "" {
		return page, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return page, []problemFieldError{{Parameter: "limit", Issue: app.IssueInvalidFormat, Detail: "limit must be an integer"}}
	}
	page.Limit = limit
	return page, nil
}

// readPageRequest is parsePageRequest for a handler: it writes the 400 itself
// and reports whether the handler should go on.
func readPageRequest(response http.ResponseWriter, request *http.Request) (app.PageRequest, bool) {
	page, unparsable := parsePageRequest(request.URL.Query())
	if len(unparsable) > 0 {
		writeProblem(response, invalidQueryProblem(unparsable))
		return page, false
	}
	return page, true
}

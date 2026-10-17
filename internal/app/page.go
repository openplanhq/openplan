package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Every growing list pages by cursor (keyset), never by offset. The deciding
// case is the stacks list: a non-admin's stacks are filtered by OpenFGA after
// the query, so an offset would re-walk and re-check every stack before the
// page on every request, while a cursor starts where the last page stopped.
// A cursor also keeps pages stable while rows arrive at the head, which runs
// and the queue do while someone reads them.
const (
	// DefaultPageLimit matches the authorization batch listAccessibleStacks
	// checks at once, so a default page of stacks is one round trip.
	DefaultPageLimit = 50
	// MaxPageLimit bounds the BatchCheck fan-out and the payload of one page.
	MaxPageLimit = 100
)

// ErrInvalidQuery is a query parameter that can't be read, such as a cursor
// this API never gave out. It is the 400 for a malformed request, where a
// value that reads but breaks a rule is ErrInvalidCommand's 422.
var ErrInvalidQuery = errors.New("invalid query")

// PageRequest asks for one page of a list: at most Limit items after the
// position Cursor names. An empty Cursor is the first page.
type PageRequest struct {
	Limit  int
	Cursor string
}

// FirstPage is the default page a caller with no paging parameters gets.
func FirstPage() PageRequest { return PageRequest{Limit: DefaultPageLimit} }

// Page is one page of a list. NextCursor is empty on the last page.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// validatePageRequest checks the limit; the cursor is checked where it is
// decoded, against the list it must belong to.
func validatePageRequest(page PageRequest) error {
	if page.Limit < 1 || page.Limit > MaxPageLimit {
		return parameterError(ErrInvalidCommand, IssueOutOfRange,
			fmt.Sprintf("limit must be between 1 and %d", MaxPageLimit), "limit")
	}
	return nil
}

// Validate is validatePageRequest for a list the API reads without a
// service, such as the queue.
func (page PageRequest) Validate() error { return validatePageRequest(page) }

// cursorKey is a list's position: the sort key of the last item on a page.
// Valid rejects a key that decodes but names no row, such as a zero time.
type cursorKey interface {
	Valid() bool
}

// cursorEnvelope names the list a cursor belongs to, so one list's cursor
// handed to another is refused rather than read as a position in it.
type cursorEnvelope struct {
	List string          `json:"l"`
	Key  json.RawMessage `json:"k"`
}

// EncodeCursor makes the opaque token a client sends back for the next page.
// It is unsigned on purpose: it is only a position, and every query it feeds
// stays tenant-scoped and authorization-checked, so a forged one can only
// skip ahead in what the caller may already read.
func EncodeCursor(list string, key cursorKey) string {
	encodedKey, err := json.Marshal(key)
	if err != nil {
		// Keys are structs of strings, ints and times, which always marshal.
		panic(fmt.Sprintf("encode %s cursor: %v", list, err))
	}
	envelope, err := json.Marshal(cursorEnvelope{List: list, Key: encodedKey})
	if err != nil {
		panic(fmt.Sprintf("encode %s cursor: %v", list, err))
	}
	return base64.RawURLEncoding.EncodeToString(envelope)
}

// DecodeCursor reads a token EncodeCursor made for list. An empty token is
// the first page, which is nil. Anything else that isn't such a token is a
// parameter error on cursor wrapping ErrInvalidQuery.
func DecodeCursor[K any, PK interface {
	*K
	cursorKey
}](list, raw string) (*K, error) {
	if raw == "" {
		return nil, nil
	}
	invalid := parameterError(ErrInvalidQuery, IssueInvalidFormat,
		"cursor isn't one this list gave out; start again from the first page", "cursor")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid
	}
	var envelope cursorEnvelope
	if err := strictUnmarshal(decoded, &envelope); err != nil || envelope.List != list {
		return nil, invalid
	}
	var key K
	if err := strictUnmarshal(envelope.Key, &key); err != nil || !PK(&key).Valid() {
		return nil, invalid
	}
	return &key, nil
}

func strictUnmarshal(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("trailing data")
	}
	return nil
}

// pageFrom turns rows fetched with limit+1 into a page: the extra row, when
// there is one, only proves a next page exists, and the cursor is the key of
// the last row the page keeps. A page never ends with a cursor that leads to
// nothing.
func pageFrom[T any](rows []T, limit int, list string, key func(T) cursorKey) Page[T] {
	if rows == nil {
		rows = []T{}
	}
	if len(rows) <= limit {
		return Page[T]{Items: rows}
	}
	items := rows[:limit]
	return Page[T]{Items: items, NextCursor: EncodeCursor(list, key(items[len(items)-1]))}
}

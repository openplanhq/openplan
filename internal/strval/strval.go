package strval

import (
	"strings"
	"unicode"
)

// SafeOpaque reports whether value is usable as an opaque identifier or
// credential in a URL path, a header, or a request body: non-empty and free of
// whitespace and control characters, so it cannot terminate a header or split a
// request.
//
// It says nothing about the value being correct, only about it being safe to
// transmit uninterpreted.
//
//	"01H8XV"      → true
//	"has space"   → false
//	"has\nnewline" → false
//	""            → false
func SafeOpaque(value string) bool {
	return value != "" && strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) == -1
}

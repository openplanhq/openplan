package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"
)

// RequestIDHeader carries the id that ties a response to the API's log lines.
// Every response has one, and an error body repeats it as request_id, so a
// person reporting a 500 can quote something an operator can search for.
const RequestIDHeader = "X-Request-Id"

// inboundRequestID bounds an id a proxy in front of the API already assigned.
// It is reused so the proxy's logs and ours share one id, but only when it is
// short and plain: the id is written into log lines, and a caller-chosen
// value with spaces or newlines could forge them.
var inboundRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// withRequestID assigns the request its id before anything else runs, so the
// authentication middleware's 401 and the unrouted 404 carry one too.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		id := request.Header.Get(RequestIDHeader)
		if !inboundRequestID.MatchString(id) {
			id = newRequestID()
		}
		response.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(response, request)
	})
}

func newRequestID() string {
	var buffer [16]byte
	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(buffer[:])
	return hex.EncodeToString(buffer[:])
}

// requestIDOf reads the id withRequestID set on the response. The response
// header is the one place every writer can reach without the request, which
// writeProblem is not given.
func requestIDOf(response http.ResponseWriter) string {
	return response.Header().Get(RequestIDHeader)
}

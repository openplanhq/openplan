package runner

import (
	"reflect"
	"testing"
)

func TestGitCredentialEmpty(t *testing.T) {
	t.Parallel()

	if !NewGitCredential("").Empty() {
		t.Fatal("empty token should produce an empty credential")
	}
	if NewGitCredential("ghs_token").Empty() {
		t.Fatal("non-empty token should not produce an empty credential")
	}
}

// The header is the whole mechanism: it must be scoped to github.com over
// HTTPS, must clear any inherited extraheader, and must be named Authorization
// so git's own trace redactor recognises it.
func TestGitEnvironmentCarriesScopedAuthorizationHeader(t *testing.T) {
	t.Parallel()

	got := gitEnvironment(NewGitCredential("ghs_token"))

	want := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=true",
		"GIT_TRACE=",
		"GIT_TRACE_PACKET=",
		"GIT_TRACE_CURL=",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=http.extraheader",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=http.https://github.com/.extraheader",
		// The value is x-access-token:ghs_token, base64-encoded.
		"GIT_CONFIG_VALUE_1=Authorization: Basic eC1hY2Nlc3MtdG9rZW46Z2hzX3Rva2Vu",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
}

// Without a credential the git config keys must be absent entirely: an empty
// Authorization header is not the same as no header, and public clones must
// keep working untouched.
func TestGitEnvironmentWithoutCredentialSetsNoConfig(t *testing.T) {
	t.Parallel()

	got := gitEnvironment(GitCredential{})

	want := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=true",
		"GIT_TRACE=",
		"GIT_TRACE_PACKET=",
		"GIT_TRACE_CURL=",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env = %#v, want %#v", got, want)
	}
}

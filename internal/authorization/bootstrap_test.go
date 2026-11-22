package authorization

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEachPage pins the guard every paged OpenFGA read relies on: pages are
// read until the provider hands back no token, and a token handed back twice
// fails instead of looping forever.
func TestEachPage(t *testing.T) {
	repeated := errors.New("repeated")
	for name, test := range map[string]struct {
		next []string
		want []string
		err  error
	}{
		"one page":       {next: []string{""}, want: []string{""}},
		"several pages":  {next: []string{"a", "b", ""}, want: []string{"", "a", "b"}},
		"repeated token": {next: []string{"a", "b", "a"}, want: []string{"", "a", "b"}, err: repeated},
	} {
		t.Run(name, func(t *testing.T) {
			var got []string
			err := eachPage(repeated, func(token string) (string, error) {
				got = append(got, token)
				return test.next[len(got)-1], nil
			})
			require.ErrorIs(t, err, test.err)
			require.Equal(t, test.want, got)
		})
	}
}

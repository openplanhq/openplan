package activities

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommandError(t *testing.T) {
	t.Parallel()

	long := "Error: " + strings.Repeat("é", 5000)
	tests := []struct {
		name string
		log  string
		want string
	}{
		{
			name: "the block and the lines under it",
			log:  "Initializing...\n\nError: validating provider credentials: 403\n\n  with provider[\"aws\"],\n  on main.tf line 1\n",
			want: "Error: validating provider credentials: 403\n\n  with provider[\"aws\"],\n  on main.tf line 1",
		},
		{
			name: "only the first of two errors",
			log:  "Error: first\n\ndetail one\n\nError: second\n",
			want: "Error: first\n\ndetail one",
		},
		{
			name: "stops at a warning",
			log:  "Error: first\n\nWarning: deprecated\n",
			want: "Error: first",
		},
		{
			name: "no error line",
			log:  "Killed\n",
			want: "",
		},
		{
			name: "a log cut off at the spool cap",
			log:  "[openplan: output truncated at 64 MiB]\n",
			want: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := commandError([]byte(test.log)); got != test.want {
				t.Fatalf("commandError() = %q, want %q", got, test.want)
			}
		})
	}

	t.Run("capped without splitting a rune", func(t *testing.T) {
		t.Parallel()
		got := commandError([]byte(long))
		if len(got) > commandErrorLimit || !utf8.ValidString(got) || !strings.HasPrefix(got, "Error: ") {
			t.Fatalf("commandError() = %d bytes, valid=%v, want at most %d valid bytes starting \"Error: \"", len(got), utf8.ValidString(got), commandErrorLimit)
		}
	})
}

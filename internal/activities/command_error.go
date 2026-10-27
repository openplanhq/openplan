package activities

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// commandErrorLimit caps the message a failed command leaves on its run. It
// is template-controlled text stored on every failed execution, so it is
// bounded; Terraform's first error fits in it with room to spare.
const commandErrorLimit = 4096

// commandError is the first error a failed Terraform command printed, as it
// printed it: the "Error: " line and the lines under it, up to the next
// diagnostic. Every command runs with -no-color, so diagnostics are plain
// lines with nothing to strip. It is "" when the output holds no error,
// as when the command was killed (issue 522).
func commandError(log []byte) string {
	var block []string
	for line := range strings.SplitSeq(string(log), "\n") {
		if block == nil {
			if strings.HasPrefix(line, "Error: ") {
				block = []string{line}
			}
			continue
		}
		if strings.HasPrefix(line, "Error: ") || strings.HasPrefix(line, "Warning: ") {
			break
		}
		block = append(block, line)
	}
	return truncateUTF8(strings.TrimSpace(strings.Join(block, "\n")), commandErrorLimit)
}

// truncateUTF8 cuts s to at most limit bytes without splitting a rune.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := []byte(s[:limit])
	for len(cut) > 0 && !utf8.Valid(cut) {
		cut = cut[:len(cut)-1]
	}
	return string(bytes.TrimRight(cut, " \n"))
}

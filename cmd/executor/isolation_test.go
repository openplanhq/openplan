package main

import (
	"os"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/runuser"
)

const goodStatus = "Name:\topenplan-execu\nUid:\t0\t0\t0\t0\nCapEff:\t00000000000000c1\nNoNewPrivs:\t1\n"

func TestCheckCapabilityStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		goodStatus: "",
		strings.Replace(goodStatus, "00000000000000c1", "00000000a80425fb", 1): "cap_drop",
		strings.Replace(goodStatus, "00000000000000c1", "0000000000000081", 1): "CAP_SETUID, CAP_SETGID and CAP_CHOWN",
		strings.Replace(goodStatus, "NoNewPrivs:\t1", "NoNewPrivs:\t0", 1):     "no-new-privileges",
		"Name:\tx\n": "CapEff",
	}
	for status, want := range cases {
		err := checkCapabilityStatus(status)
		if want == "" && err != nil {
			t.Errorf("good status: %v", err)
		}
		if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("status %q: error = %v, want it to mention %q", status, err, want)
		}
	}
}

func TestCheckSessionLimit(t *testing.T) {
	t.Parallel()
	users := runuser.PoolUsers()
	if err := checkSessionLimit(20, users); err != nil {
		t.Fatalf("20 of 40: %v", err)
	}
	if err := checkSessionLimit(21, users); err == nil || !strings.Contains(err.Error(), "EXECUTOR_MAX_SESSIONS") {
		t.Fatalf("21 of 40: %v", err)
	}
}

// The executor runs only in its Linux image, as root. Anywhere else it must
// refuse to start rather than run sessions without isolation.
func TestCheckIsolationRefusesANonRootExecutor(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("needs a non-root user")
	}

	err := checkIsolation(config.ExecutorConfig{MaxSessions: 20}, runuser.PoolUsers())
	if err == nil || !strings.Contains(err.Error(), "want 0") {
		t.Fatalf("checkIsolation = %v, want a refusal for a non-root executor", err)
	}
}

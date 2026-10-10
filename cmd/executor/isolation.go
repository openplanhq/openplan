package main

import (
	"fmt"
	"strings"

	"github.com/vishu42/openplan/internal/runuser"
)

// wantCapabilities is CAP_CHOWN (0), CAP_SETGID (6) and CAP_SETUID (7): what
// the executor needs to start each branch as its pool user, and nothing more.
const wantCapabilities = "00000000000000c1"

// checkCapabilityStatus checks /proc/self/status. More capabilities mean the
// container kept Docker's defaults: cap_drop is missing. Without
// no_new_privs, a setuid binary a branch runs could raise its powers again.
func checkCapabilityStatus(status string) error {
	fields := map[string]string{}
	for line := range strings.SplitSeq(status, "\n") {
		name, value, ok := strings.Cut(line, ":")
		if ok {
			fields[name] = strings.TrimSpace(value)
		}
	}
	capEff, ok := fields["CapEff"]
	if !ok {
		return fmt.Errorf("no CapEff in /proc/self/status")
	}
	if capEff != wantCapabilities {
		return fmt.Errorf("effective capabilities are %s, want exactly CAP_SETUID, CAP_SETGID and CAP_CHOWN (%s): check cap_drop: [ALL] and cap_add", capEff, wantCapabilities)
	}
	if fields["NoNewPrivs"] != "1" {
		return fmt.Errorf("no_new_privs is off: run with security_opt no-new-privileges:true (Kubernetes: allowPrivilegeEscalation: false)")
	}
	return nil
}

// checkSessionLimit keeps two pool users per session: the spares absorb leases
// stuck until their take-back.
func checkSessionLimit(maxSessions int, users []runuser.User) error {
	if maxSessions*2 > len(users) {
		return fmt.Errorf("EXECUTOR_MAX_SESSIONS is %d, at most %d with %d pool users", maxSessions, len(users)/2, len(users))
	}
	return nil
}

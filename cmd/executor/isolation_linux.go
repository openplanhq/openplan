//go:build linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/runhelper"
	"github.com/vishu42/openplan/internal/runuser"
)

// newIsolation runs every branch as one of the image's pool users.
func newIsolation() (activities.Isolation, error) {
	helper, err := runhelper.NewClient()
	if err != nil {
		return activities.Isolation{}, err
	}
	return activities.Isolation{Users: runuser.PoolUsers(), Helper: helper}, nil
}

// checkIsolation fails closed: an executor that cannot keep branches apart
// must not run template code.
func checkIsolation(cfg config.ExecutorConfig, users []runuser.User) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("the executor runs as uid %d, want 0: run it in its image, which starts it as root with three capabilities", os.Geteuid())
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	if err := checkCapabilityStatus(string(status)); err != nil {
		return err
	}
	for _, want := range users {
		got, err := user.Lookup(want.Name)
		if err != nil {
			return fmt.Errorf("pool user %s: %w", want.Name, err)
		}
		if got.Uid != strconv.FormatUint(uint64(want.UID), 10) {
			return fmt.Errorf("pool user %s is uid %s, want %d", want.Name, got.Uid, want.UID)
		}
	}
	return checkSessionLimit(cfg.MaxSessions, users)
}

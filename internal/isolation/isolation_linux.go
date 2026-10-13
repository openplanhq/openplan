//go:build linux

package isolation

import (
	"fmt"
	"os"
	osuser "os/user"
	"strconv"

	"golang.org/x/sys/unix"
)

// New returns the executor's Manager: every session runs as one of the image's
// pool users, through the helper. It fails closed: an executor that cannot
// keep sessions apart must not run template code, so New checks the process
// and the machine first and refuses to build a Manager if any check fails.
func New(cfg Config) (*Manager, error) {
	users := poolUsers()
	if err := checkHost(cfg.MaxSessions, users); err != nil {
		return nil, err
	}
	h, err := newHelperClient()
	if err != nil {
		return nil, err
	}
	return newManager(cfg, users, h), nil
}

// checkHost checks every assumption the package makes about the process it
// runs in: root, with exactly the three capabilities its own code uses, under
// no_new_privs, with the pool users it expects, and enough of them for
// maxSessions.
func checkHost(maxSessions int, users []user) error {
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
		got, err := osuser.Lookup(want.name)
		if err != nil {
			return fmt.Errorf("pool user %s: %w", want.name, err)
		}
		if got.Uid != strconv.FormatUint(uint64(want.uid), 10) {
			return fmt.Errorf("pool user %s is uid %s, want %d", want.name, got.Uid, want.uid)
		}
	}
	return checkSessionLimit(maxSessions, users)
}

// HardenProcess clears this process's dumpable flag, so the kernel makes its
// /proc/<pid> entries root's and refuses to let a same-uid process ptrace it.
// The executor calls it first, before its config is read or any subprocess
// exists, and refuses to start if it fails.
//
// Without it, a process of the executor's uid could read
// /proc/<executor pid>/environ, the kernel's snapshot of the environment from
// exec (the artifact store's credentials and the Temporal address), which no
// os.Unsetenv or cmd.Env filtering reaches; and /proc/<executor pid>/mem, the
// heap holding every run's sealing key and opened credentials (#240). Sessions
// run as pool users of their own, so this is a second layer.
//
// It covers this process only: the kernel sets the flag again on execve, so
// tofu, git and the helper run as before. Root and CAP_SYS_PTRACE are not
// stopped. The cost is no core dumps and no attaching a debugger as the
// executor's user.
func HardenProcess() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}

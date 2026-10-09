//go:build linux

package main

import "golang.org/x/sys/unix"

// setNotDumpable clears this process's dumpable flag, so the kernel makes its
// /proc/<pid> entries root's and refuses to let a same-uid process ptrace it.
//
// Tofu runs template code as the executor's uid, and that code is untrusted.
// Without this it could read /proc/<executor pid>/environ, the kernel's
// snapshot of the environment from exec (the artifact store's credentials and
// the Temporal address), which no os.Unsetenv or cmd.Env filtering reaches;
// and /proc/<executor pid>/mem, the heap holding every run's sealing key and
// opened credentials (#240).
//
// It covers this process only: the kernel sets the flag again on execve, so
// tofu and git run as before, and one run can still read another's /proc
// until runs get their own uid (#331). Root and CAP_SYS_PTRACE are not
// stopped. The cost is no core dumps and no attaching a debugger as the
// executor's user.
func setNotDumpable() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}

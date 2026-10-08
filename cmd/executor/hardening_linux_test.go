//go:build linux

package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"testing"
)

const (
	// dumpableHelperCommand turns this test binary into a long-lived child the
	// test can probe through /proc.
	dumpableHelperCommand = "dumpable-helper"
	helperCanary          = "OPENPLAN_TEST_CANARY=executor-secret"
)

// TestDumpableHelperProcess is not a test. It is the child the /proc tests
// probe: it stands in for the executor, optionally hardened, and stays alive
// until the parent closes its stdin.
func TestDumpableHelperProcess(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) != 3 || args[1] != dumpableHelperCommand {
		return
	}
	if args[2] == "hardened" {
		if err := setNotDumpable(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

// startDumpableHelper starts the helper with the canary as its whole
// environment and returns its pid once it has hardened itself, if asked to.
func startDumpableHelper(t *testing.T, mode string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDumpableHelperProcess$", "--", dumpableHelperCommand, mode)
	cmd.Env = []string{helperCanary}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("helper did not become ready: line %q, err %v, stderr %s", line, err, stderr.String())
	}
	return cmd.Process.Pid
}

// Tofu runs template code as the executor's uid, and a same-uid process can
// read another's /proc entries: environ is the kernel's snapshot of the
// environment from exec, which no later os.Unsetenv or cmd.Env filtering
// touches, and mem is the heap holding the runs' sealing keys and opened
// credentials. The test reads both the way a template would, from a same-uid
// process, so it pins what the kernel enforces rather than the prctl call.
func TestSetNotDumpableHidesProcessFromSameUID(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any /proc/<pid> regardless of the dumpable flag; run as an unprivileged user")
	}

	// Control: without hardening the canary is readable. Without this the
	// assertions below could pass because /proc is mounted hidepid, not
	// because of the flag.
	environ, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", startDumpableHelper(t, "plain")))
	if err != nil {
		t.Fatalf("control: read a dumpable process's environ: %v", err)
	}
	if !bytes.Contains(environ, []byte(helperCanary)) {
		t.Fatalf("control: environ %q does not contain the canary", environ)
	}

	pid := startDumpableHelper(t, "hardened")
	for _, file := range []string{"environ", "mem"} {
		_, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, file))
		if !errors.Is(err, fs.ErrPermission) {
			t.Errorf("read /proc/<pid>/%s of a hardened process: err = %v, want permission denied", file, err)
		}
	}
}

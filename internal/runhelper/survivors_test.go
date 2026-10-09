package runhelper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeStatus fakes /proc/<pid>/status with the two lines survivors reads.
func writeStatus(t *testing.T, proc, pid, state string, uid uint32) {
	t.Helper()
	dir := filepath.Join(proc, pid)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	status := fmt.Sprintf("Name:\tsleep\nState:\t%s\nUid:\t%d\t%d\t%d\t%d\n", state, uid, uid, uid, uid)
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A zombie is dead and can do nothing; only live processes of the user count.
func TestSurvivorsAreTheUsersLiveProcesses(t *testing.T) {
	t.Parallel()
	proc := t.TempDir()
	writeStatus(t, proc, "10", "Z (zombie)", 70001)
	writeStatus(t, proc, "11", "S (sleeping)", 70002)
	writeStatus(t, proc, "12", "S (sleeping)", 70001)
	if err := os.Mkdir(filepath.Join(proc, "sys"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := survivors(proc, 70001)
	if err != nil || !reflect.DeepEqual(got, []int{12}) {
		t.Fatalf("survivors = %v, %v; want [12]", got, err)
	}
}

// The helper runs as the pool user, so a process of that user can make it exit
// 0 having killed nothing. The executor's own look must catch the survivor.
func TestWaitForNoSurvivorsFailsWhileOneLives(t *testing.T) {
	t.Parallel()
	proc := t.TempDir()
	writeStatus(t, proc, "12", "S (sleeping)", 70001)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := waitForNoSurvivors(ctx, proc, 70001); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("waitForNoSurvivors = %v, want a survivor error", err)
	}
	if err := waitForNoSurvivors(context.Background(), proc, 70002); err != nil {
		t.Fatalf("waitForNoSurvivors(other uid) = %v", err)
	}
}

// Without /proc there is no way to know, so the reclaim fails rather than
// hand the user on.
func TestWaitForNoSurvivorsFailsWithoutProc(t *testing.T) {
	t.Parallel()
	if err := waitForNoSurvivors(context.Background(), filepath.Join(t.TempDir(), "missing"), 70001); err == nil {
		t.Fatal("waitForNoSurvivors without /proc returned nil")
	}
}

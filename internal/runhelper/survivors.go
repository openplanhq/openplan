package runhelper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// survivorPoll is how often waitForNoSurvivors looks again. SIGKILL is
// delivered asynchronously, so a reclaimed user's processes can take a moment
// to go.
const survivorPoll = 50 * time.Millisecond

// survivorTimeout bounds the wait when ctx has no sooner deadline: a killed
// process leaves within milliseconds unless it is stuck in the kernel.
const survivorTimeout = 10 * time.Second

// waitForNoSurvivors returns once uid has no live process under procRoot, and
// fails if one outlasts ctx or survivorTimeout. It runs in the executor, not
// the helper, because only the executor's own look can be trusted: the helper
// runs as uid, so a process of uid can tamper with it.
func waitForNoSurvivors(ctx context.Context, procRoot string, uid uint32) error {
	ctx, cancel := context.WithTimeout(ctx, survivorTimeout)
	defer cancel()
	for {
		pids, err := survivors(procRoot, uid)
		if err != nil {
			return fmt.Errorf("look for processes of uid %d: %w", uid, err)
		}
		if len(pids) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("processes %v of uid %d are still running", pids, uid)
		case <-time.After(survivorPoll):
		}
	}
}

// survivors returns the pids of uid's live processes under procRoot. A zombie
// is dead and holds nothing, so it does not count.
func survivors(procRoot string, uid uint32) ([]int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	want := strconv.FormatUint(uint64(uid), 10)
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		// Gone since the listing: nothing left to count.
		status, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "status"))
		if err != nil {
			continue
		}
		if ownedLive(string(status), want) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// ownedLive reports whether a /proc/<pid>/status describes a live process
// whose real or effective uid is uid.
func ownedLive(status, uid string) bool {
	var owned, dead bool
	for line := range strings.SplitSeq(status, "\n") {
		name, value, _ := strings.Cut(line, ":")
		fields := strings.Fields(value)
		switch {
		case name == "State" && len(fields) > 0:
			dead = fields[0] == "Z" || fields[0] == "X"
		case name == "Uid" && len(fields) > 1:
			owned = fields[0] == uid || fields[1] == uid
		}
	}
	return owned && !dead
}

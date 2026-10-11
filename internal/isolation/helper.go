package isolation

import "context"

// helper does the work inside a session's workspace as the session's pool
// user (helperClient). The executor never reads or writes there itself: root
// without CAP_DAC_OVERRIDE cannot, and a session controls every name in it, so
// root following a planted symlink would read or write root's own files
// (#331). Every method runs as u, with no more access than the session
// already has, so nothing a method does can reach past that session.
type helper interface {
	// kill ends every process u owns. Root without CAP_KILL cannot signal
	// another uid, so the kill runs from inside the session, as u. The
	// session executor calls it after each command and when one is cancelled,
	// so nothing a command starts (nohup … &) outlives the command.
	kill(ctx context.Context, u user) error
	// reclaim makes u safe to hand to the next session: it kills u's
	// processes, deletes u's files in the shared scratch directories (/tmp,
	// /var/tmp, /dev/shm), and empties workspace unless it is "". A nil
	// error must mean nothing of u is left running: the manager frees u on
	// success and keeps it out of the pool on failure. The workspace is
	// emptied rather than removed because its parent is root's; root removes
	// the then-empty directory itself.
	reclaim(ctx context.Context, u user, workspace string) error
	// readFiles reads names from dir as u, leaving out names that don't
	// exist. Read as root, a file the session swapped for a symlink to the
	// executor's /proc/<pid>/environ would put the executor's S3 keys into
	// whatever the caller makes of it, such as a plan the apply phase
	// unpacks for the session.
	readFiles(ctx context.Context, u user, dir string, names []string) (map[string][]byte, error)
	// writeFiles writes files into dir as u. The executor hands over only
	// plaintext content, so keys it used to open that content never reach the
	// session, and root never writes to a name the session could have pointed
	// elsewhere.
	writeFiles(ctx context.Context, u user, dir string, files map[string][]byte) error
	// checkDir reports whether path is a directory, as u sees it. The path is
	// u's: a symlink in it could point root at a path only root can see,
	// while u gets the same answer tofu will.
	checkDir(ctx context.Context, u user, path string) error
}

// inProcessHelper does the helper's file work in this process, as the current
// user, and kills nothing: killing every process of the current user would
// end the test run. It is NewForTest's, never the executor's.
type inProcessHelper struct{}

func (inProcessHelper) kill(context.Context, user) error { return nil }

func (inProcessHelper) reclaim(_ context.Context, _ user, workspace string) error {
	if workspace == "" {
		return nil
	}
	return emptyDir(workspace)
}

func (inProcessHelper) readFiles(_ context.Context, _ user, dir string, names []string) (map[string][]byte, error) {
	return readFiles(dir, names)
}

func (inProcessHelper) writeFiles(_ context.Context, _ user, dir string, files map[string][]byte) error {
	return writeFiles(dir, files)
}

func (inProcessHelper) checkDir(_ context.Context, _ user, path string) error {
	return checkDir(path)
}

package isolation

import (
	"context"
	"path/filepath"
)

// Session is one run's hold on a pool user and a run directory, from
// Manager.Open to Manager.Close. Get one from Open or Manager.Session; a
// Session made any other way, or kept past its Close, is refused everywhere.
type Session struct {
	// id is the run's lease key, "<tenant>/<run>": how the manager finds the
	// session, and its run directory relative to the run root.
	id string
	// runDir is the run directory, "<run root>/<tenant>/<run>", fixed by Open.
	// Every path the session hands out is built from it, and it is unexported
	// so nothing outside the package can point a session somewhere else.
	runDir string
	// user is the pool user leased to this session.
	user user
	// m is the manager that leased it, asked on every use whether the lease
	// still stands.
	m *Manager
}

// Workspace is the session's half of its run directory, which the session's
// user owns: the source, and HOME and TMPDIR for its commands.
func (s *Session) Workspace() string {
	return filepath.Join(s.runDir, workspaceDirName)
}

// LogsDir is the executor's half of its run directory, where the executor
// spools the run's command logs. It is root's, beside the workspace, so the
// session can neither read it nor plant a symlink in it to point root's
// writes elsewhere.
func (s *Session) LogsDir() string {
	return filepath.Join(s.runDir, logsDirName)
}

func (s *Session) home() string {
	return filepath.Join(s.Workspace(), homeDirName)
}

func (s *Session) tempDir() string {
	return filepath.Join(s.Workspace(), tempDirName)
}

// open reports whether s is still its run's session. A nil or zero Session
// has no Manager, and one past its Close no longer holds the lease; either
// could name a user that is another session's by now.
func (s *Session) open() error {
	if s == nil || s.m == nil || !s.m.holds(s) {
		return ErrSessionClosed
	}
	return nil
}

type sessionKey struct{}

// Context returns ctx tagged with s, for the runners built on
// SessionExecutor, which serve every session and learn from ctx which one a
// command is for. It is the only way to have a command run as a session's
// user, and the key is the package's own, so no caller can write a user into
// ctx itself.
func (s *Session) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// sessionFrom returns the session ctx is tagged with, if it is still open.
func sessionFrom(ctx context.Context) (*Session, error) {
	s, ok := ctx.Value(sessionKey{}).(*Session)
	if !ok {
		return nil, ErrNoSession
	}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

// ReadFiles reads names from dir, a folder inside the session's workspace,
// as the session's user. A name that doesn't exist is left out of the result,
// so the caller decides which files it needs.
//
// Read as root, a file the session swapped for a symlink to the executor's
// /proc/<pid>/environ would hand the executor's own secrets to whatever the
// caller does with the content. As the session's user, a symlink yields only
// what the session could read anyway.
func (s *Session) ReadFiles(ctx context.Context, dir string, names []string) (map[string][]byte, error) {
	if err := s.open(); err != nil {
		return nil, err
	}
	return s.m.helper.readFiles(ctx, s.user, dir, names)
}

// WriteFiles writes files, by name, into dir inside the session's workspace,
// as the session's user. Root never writes to a name the session could have
// pointed elsewhere.
func (s *Session) WriteFiles(ctx context.Context, dir string, files map[string][]byte) error {
	if err := s.open(); err != nil {
		return err
	}
	return s.m.helper.writeFiles(ctx, s.user, dir, files)
}

// CheckDir reports whether path, inside the session's workspace, is a
// directory as the session's user sees it. A symlink there could point root
// at a path only root can see, while the session's user gets the same answer
// tofu will.
func (s *Session) CheckDir(ctx context.Context, path string) error {
	if err := s.open(); err != nil {
		return err
	}
	return s.m.helper.checkDir(ctx, s.user, path)
}

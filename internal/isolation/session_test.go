package isolation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func openTestSession(t *testing.T, m *Manager, id string) *Session {
	t.Helper()
	s, err := m.Open(context.Background(), id, filepath.Join(m.runRoot, id))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestContextCarriesTheSession(t *testing.T) {
	t.Parallel()
	m := newTestManager(t.TempDir(), 1, inProcessHelper{})
	s := openTestSession(t, m, "t/run")

	got, err := sessionFrom(s.Context(context.Background()))
	if err != nil || got != s {
		t.Fatalf("sessionFrom = %v, %v; want the session", got, err)
	}
	if _, err := sessionFrom(context.Background()); !errors.Is(err, ErrNoSession) {
		t.Fatalf("sessionFrom(no session) = %v, want ErrNoSession", err)
	}
}

// A Session only the package can make: a nil or zero one, or one built around a
// user, belongs to no manager and is refused.
func TestMadeUpSessionsAreRefused(t *testing.T) {
	t.Parallel()
	for name, s := range map[string]*Session{
		"nil":          nil,
		"zero":         {},
		"with a user":  {id: "t/run", user: poolUsers()[0]},
		"with no root": {id: "t/run", runDir: "/runs/t/run"},
	} {
		if _, err := sessionFrom(s.Context(context.Background())); !errors.Is(err, ErrSessionClosed) {
			t.Errorf("%s: sessionFrom = %v, want ErrSessionClosed", name, err)
		}
		if _, err := s.ReadFiles(context.Background(), "/", []string{"x"}); !errors.Is(err, ErrSessionClosed) {
			t.Errorf("%s: ReadFiles = %v, want ErrSessionClosed", name, err)
		}
	}
}

// After Close the session's user may be handed to the next session. The old
// Session must not reach it, even once the user belongs to another run.
func TestClosedSessionsAreRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := newTestManager(t.TempDir(), 1, inProcessHelper{})
	old := openTestSession(t, m, "t/old")
	kept := old.Context(ctx)
	if err := m.Close(ctx, "t/old"); err != nil {
		t.Fatal(err)
	}
	next := openTestSession(t, m, "t/next")
	if next.user != old.user {
		t.Fatalf("next session got %v, want the old session's user back", next.user)
	}

	if _, err := sessionFrom(kept); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("sessionFrom(closed) = %v, want ErrSessionClosed", err)
	}
	if _, err := old.ReadFiles(ctx, old.Workspace(), []string{"x"}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("ReadFiles on a closed session = %v, want ErrSessionClosed", err)
	}
	if err := old.WriteFiles(ctx, next.Workspace(), map[string][]byte{"x": nil}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("WriteFiles on a closed session = %v, want ErrSessionClosed", err)
	}
	if err := old.CheckDir(ctx, next.Workspace()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("CheckDir on a closed session = %v, want ErrSessionClosed", err)
	}
}

func TestSessionFilesRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := newTestManager(t.TempDir(), 1, inProcessHelper{})
	s := openTestSession(t, m, "t/run")
	dir := filepath.Join(s.Workspace(), "source")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	want := map[string][]byte{"tfplan": []byte("the plan"), ".terraform.lock.hcl": []byte("locked")}
	if err := s.WriteFiles(ctx, dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadFiles(ctx, dir, []string{"tfplan", ".terraform.lock.hcl", "missing"})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadFiles = %q, %v; want %q, the missing name left out", got, err, want)
	}
}

func TestSessionCheckDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := newTestManager(t.TempDir(), 1, inProcessHelper{})
	s := openTestSession(t, m, "t/run")
	file := filepath.Join(s.Workspace(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.CheckDir(ctx, s.Workspace()); err != nil {
		t.Fatalf("CheckDir(workspace) = %v", err)
	}
	if err := s.CheckDir(ctx, file); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("CheckDir(file) = %v", err)
	}
	if err := s.CheckDir(ctx, filepath.Join(s.Workspace(), "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("CheckDir(missing) = %v", err)
	}
}

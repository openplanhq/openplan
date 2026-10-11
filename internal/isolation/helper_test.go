package isolation

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestHelperMain is not a test. It is the helper that helperClient starts in
// these tests: this test binary, re-run after "--", standing in for the
// executor.
func TestHelperMain(t *testing.T) {
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		return
	}
	switch args[1] {
	case "print-environment":
		fmt.Print(strings.Join(os.Environ(), "\x00"))
		os.Exit(0)
	// Helpers someone tampered with: one answers with more than the
	// executor would keep, one with a file the executor didn't ask for.
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for range 3 {
			_, _ = os.Stdout.Write(chunk)
		}
		os.Exit(0)
	case "send-unasked":
		archive, _ := encodeFiles(map[string][]byte{"main.tf": []byte("x")})
		_, _ = os.Stdout.Write(archive)
		os.Exit(0)
	}
	os.Exit(HelperMain(args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func testClient() helperClient {
	return helperClient{command: []string{os.Args[0], "-test.run=^TestHelperMain$", "--"}, procRoot: "/proc"}
}

func TestClientMovesFilesAsTheUser(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	ctx := context.Background()
	u := developmentUsers(1)[0]
	from, to := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "tfplan"), []byte("the plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := testClient().readFiles(ctx, u, from, []string{"tfplan", ".terraform.lock.hcl"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, map[string][]byte{"tfplan": []byte("the plan")}) {
		t.Fatalf("readFiles = %q, want the plan and no lock file", files)
	}
	if err := testClient().writeFiles(ctx, u, to, files); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(to, "tfplan")); string(got) != "the plan" {
		t.Fatalf("written plan = %q", got)
	}
	if err := testClient().checkDir(ctx, u, filepath.Join(from, "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("checkDir = %v", err)
	}
}

// The helper runs as a pool user, so any process of that user can read its
// /proc/<pid>/environ. It must start with nothing from the executor's.
func TestClientStartsTheHelperWithAnEmptyEnvironment(t *testing.T) {
	skipAsRoot(t)
	t.Setenv("OPENPLAN_TEST_CANARY", "leaked")

	var stdout bytes.Buffer
	if err := testClient().run(context.Background(), developmentUsers(1)[0], nil, &stdout, "print-environment"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "OPENPLAN_TEST_CANARY") {
		t.Fatalf("helper environment = %q, want empty", stdout.String())
	}
}

// A process of the session's user can tamper with the helper, so the
// executor keeps only a bounded answer, and only the files it asked for.
func TestClientDistrustsTheHelpersAnswer(t *testing.T) {
	t.Parallel()
	skipAsRoot(t)
	u := developmentUsers(1)[0]

	out := &cappedBuffer{limit: 1 << 20}
	if err := testClient().run(context.Background(), u, nil, out, "flood"); err != nil {
		t.Fatal(err)
	}
	if !out.overflowed || out.Len() != 1<<20 {
		t.Fatalf("kept %d bytes, overflowed %v; want the first MiB and the overflow noted", out.Len(), out.overflowed)
	}

	tampered := testClient()
	tampered.command = append(tampered.command, "send-unasked")
	if _, err := tampered.readFiles(context.Background(), u, t.TempDir(), []string{"tfplan"}); err == nil || !strings.Contains(err.Error(), "not asked for") {
		t.Fatalf("readFiles = %v, want main.tf refused", err)
	}
}

func mustEncode(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	archive, err := encodeFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestTransferRoundTripsFiles(t *testing.T) {
	t.Parallel()
	want := map[string][]byte{"tfplan": []byte("plan"), ".terraform.lock.hcl": []byte("lock"), "empty": {}}

	got, err := decodeFiles(mustEncode(t, want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("decodeFiles = %q, %v; want %q", got, err, want)
	}
}

// Either side decodes what the other sent, and the helper may have been
// tampered with: only regular files with plain names get through, each once.
func TestTransferRefusesWhatIsNotAPlainFile(t *testing.T) {
	t.Parallel()
	entries := func(headers ...tar.Header) []byte {
		var buffer bytes.Buffer
		archive := tar.NewWriter(&buffer)
		for _, header := range headers {
			_ = archive.WriteHeader(&header)
		}
		_ = archive.Close()
		return buffer.Bytes()
	}
	plain := tar.Header{Name: "tfplan", Typeflag: tar.TypeReg}
	for name, archive := range map[string][]byte{
		"a path":      entries(tar.Header{Name: "../escape", Typeflag: tar.TypeReg}),
		"an absolute": entries(tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg}),
		"a symlink":   entries(tar.Header{Name: "tfplan", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}),
		"a directory": entries(tar.Header{Name: "dir", Typeflag: tar.TypeDir}),
		"a duplicate": entries(plain, plain),
	} {
		if _, err := decodeFiles(archive); err == nil {
			t.Errorf("decodeFiles accepted %s", name)
		}
	}
	if _, err := encodeFiles(map[string][]byte{"../escape": nil}); err == nil {
		t.Error("encodeFiles accepted a path")
	}
}

func TestReadAndWriteFilesTakeOnlyPlainNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"", ".", "..", "../escape", "a/b"} {
		if _, err := readFiles(dir, []string{name}); err == nil {
			t.Errorf("readFiles accepted %q", name)
		}
		if err := writeFiles(dir, map[string][]byte{name: nil}); err == nil {
			t.Errorf("writeFiles accepted %q", name)
		}
	}
}

// A template can leave directories it made unreadable. Reclaim must still
// empty the workspace, or the next session on that user inherits them.
func TestEmptyDirRemovesUnreadableDirectories(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	locked := filepath.Join(workspace, "a", "locked")
	if err := os.MkdirAll(filepath.Join(locked, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "deep", "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}

	if err := emptyDir(workspace); err != nil {
		t.Fatalf("emptyDir = %v", err)
	}
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Fatalf("workspace holds %v, %v; want it empty", entries, err)
	}
}

// A symlink in the workspace is removed, never followed.
func TestEmptyDirDoesNotFollowSymlinks(t *testing.T) {
	t.Parallel()
	workspace, outside := t.TempDir(), t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workspace, "link")); err != nil {
		t.Fatal(err)
	}

	if err := emptyDir(workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("file behind the symlink is gone: %v", err)
	}
}

func TestClearScratchRemovesOnlyTheUsersEntries(t *testing.T) {
	t.Parallel()
	scratch := t.TempDir()
	if err := os.WriteFile(filepath.Join(scratch, "mine"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(scratch, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}

	// A uid that owns nothing here leaves everything alone.
	if err := clearScratch(70040, []string{scratch, filepath.Join(scratch, "missing")}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 2 {
		t.Fatalf("entries = %v, want both kept", entries)
	}

	if err := clearScratch(uint32(os.Getuid()), []string{scratch}); err != nil { //nolint:gosec // the kernel stores uids in 32 bits
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 0 {
		t.Fatalf("entries = %v, want both removed", entries)
	}
}

// As anyone but a pool user, kill(-1) would take down the developer's whole
// session, or as root the container. The guard must hold before any signal.
func TestKillRefusesOutsideThePool(t *testing.T) {
	t.Parallel()

	for _, op := range []string{"kill", "reclaim"} {
		var stderr bytes.Buffer
		if code := HelperMain([]string{op}, nil, nil, &stderr); code == 0 || !strings.Contains(stderr.String(), "not a pool user") {
			t.Fatalf("HelperMain(%s) = %d, %q; want a refusal", op, code, stderr.String())
		}
	}
}

func TestHelperMainMovesFiles(t *testing.T) {
	t.Parallel()
	from, to := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(from, "tfplan"), []byte("the plan"), 0o600); err != nil {
		t.Fatal(err)
	}

	var archive, stderr bytes.Buffer
	if code := HelperMain([]string{"read-files", from, "tfplan"}, nil, &archive, &stderr); code != 0 {
		t.Fatalf("read-files = %d: %s", code, stderr.String())
	}
	if code := HelperMain([]string{"write-files", to}, &archive, nil, &stderr); code != 0 {
		t.Fatalf("write-files = %d: %s", code, stderr.String())
	}
	if got, _ := os.ReadFile(filepath.Join(to, "tfplan")); string(got) != "the plan" {
		t.Fatalf("written plan = %q", got)
	}
}

func TestHelperMainRejectsUnknownOperations(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if code := HelperMain([]string{"read-files"}, nil, nil, &stderr); code == 0 {
		t.Fatal("read-files without a directory succeeded")
	}
	if code := HelperMain([]string{"rm", "-rf", "/"}, nil, nil, &stderr); code == 0 {
		t.Fatal("an unknown operation succeeded")
	}
}

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
func TestReclaimFailsWhileAProcessSurvives(t *testing.T) {
	t.Parallel()
	proc := t.TempDir()
	writeStatus(t, proc, "12", "S (sleeping)", 70040)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Stands in for a subverted helper: it exits 0 and does nothing.
	subverted := helperClient{command: []string{"/bin/true"}, procRoot: proc}
	u := poolUsers()[PoolSize-1]
	if os.Geteuid() != 0 {
		// Off root the helper can only start as the current user.
		u = user{name: "development", uid: uint32(os.Getuid()), gid: uint32(os.Getgid())} //nolint:gosec // the kernel stores uids and gids in 32 bits
		writeStatus(t, proc, "13", "S (sleeping)", u.uid)
	}
	if err := subverted.reclaim(ctx, u, ""); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("reclaim = %v, want a survivor error", err)
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

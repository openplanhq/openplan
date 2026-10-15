//go:build sandbox

// Package sandboxtest checks the sandbox's promises (#245) against real runsc,
// in the executor image, with the privileges docker-compose.yaml gives it.
// make sandbox-test runs it; go test ./... skips it, having no build tag.
package sandboxtest

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/planbundle"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/sandbox"
)

const (
	runRoot   = "/var/lib/openplan/runs"
	state     = "/var/lib/openplan/sandboxes"
	guestRoot = "/opt/openplan/guest"
	runsc     = "/usr/local/bin/runsc"
)

var (
	cgroups string
	network *sandbox.Network
	runs    atomic.Int64
)

func TestMain(m *testing.M) {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "sandbox tests run as root in the executor image: make sandbox-test")
		os.Exit(1)
	}
	parent, err := sandbox.DelegateCgroups("/sys/fs/cgroup")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cgroups = parent
	network, err = sandbox.SetupNetwork(state, netip.MustParsePrefix(config.DefaultExecutorSandboxSubnet))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func newRuntime(memoryMiB int64) *sandbox.Runtime {
	return sandbox.NewRuntime(runsc, state, guestRoot, "/sys/fs/cgroup", cgroups, sandbox.Limits{MemoryMiB: memoryMiB, CPUs: 1, Pids: 256}, network)
}

// run is one template run through the real activities and the real runtime.
type run struct {
	activities *activities.TemplateRunActivities
	id         string
	workspace  string
	publicKey  []byte
}

func newRun(t *testing.T, runner sandbox.Runner, store config.ArtifactStoreConfig) *run {
	t.Helper()
	a := activities.NewTemplateRunActivities(runner, sandbox.Workspaces{Root: runRoot}, store, nil, runseal.NewKeyRing())
	// Numbered: a test with two runs must not give both one run directory.
	id := fmt.Sprintf("%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), runs.Add(1))
	out, err := a.PrepareWorkspace(context.Background(), domain.PrepareWorkspaceActivityInput{TenantID: "tenant", RunID: domain.TemplateRunID(id)})
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	t.Cleanup(func() {
		_ = a.CleanupWorkspace(context.Background(), domain.CleanupWorkspaceActivityInput{TenantID: "tenant", RunID: domain.TemplateRunID(id)})
	})
	return &run{activities: a, id: id, workspace: out.WorkspacePath, publicKey: out.PublicKey}
}

// template writes a root module whose only resource runs probe at apply.
func (r *run) template(t *testing.T, probe string) {
	t.Helper()
	r.write(t, fmt.Sprintf("resource \"terraform_data\" \"probe\" {\n  provisioner \"local-exec\" {\n    command = <<-EOT\n%s\n    EOT\n  }\n}\n", probe))
}

// write makes main the run's root module. It writes as the sandbox user: the
// workspace is that user's, 0700.
func (r *run) write(t *testing.T, main string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", `cat > "$1"`, "sh", filepath.Join(r.workspace, "main.tf"))
	cmd.Stdin = strings.NewReader(main)
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: sandbox.SandboxUID, Gid: sandbox.SandboxUID}}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write main.tf: %v: %s", err, out)
	}
}

// terraform runs one command in the run's workspace with env as its
// credentials.
func (r *run) terraform(ctx context.Context, t *testing.T, command domain.TerraformCommandType, env map[string]string) (domain.RunTerraformActivityOutput, error) {
	t.Helper()
	sealed, err := runseal.Seal(r.publicKey, env)
	if err != nil {
		t.Fatal(err)
	}
	return r.activities.RunTerraform(ctx, domain.RunTerraformActivityInput{
		TenantID: "tenant", RunID: domain.TemplateRunID(r.id), TerraformPath: r.workspace,
		WorkspaceName: "default", Command: command, RunPhase: domain.RunPhaseApply, SealedEnvironment: sealed,
	})
}

func (r *run) apply(ctx context.Context, t *testing.T, env map[string]string) error {
	t.Helper()
	if _, err := r.terraform(ctx, t, domain.TerraformCommandInit, nil); err != nil {
		t.Fatalf("init: %v", err)
	}
	_, err := r.terraform(ctx, t, domain.TerraformCommandApplyAutoApprove, env)
	// A probe passes by exiting 0, so an apply that never ran it must not
	// pass for it.
	if err == nil && !strings.Contains(r.log(t, "apply.log"), "Apply complete!") {
		t.Fatalf("the apply ran no probe:\n%s", r.log(t, "apply.log"))
	}
	return err
}

func (r *run) log(t *testing.T, name string) string {
	t.Helper()
	body, _ := os.ReadFile(filepath.Join(filepath.Dir(r.workspace), "logs", name))
	return string(body)
}

// The probes build their marker from two halves, so their own command line
// and template never contain it.
const canaryProbe = `printf '%s%s' 'leak-me-' '7f3a' > /tmp/pat
if grep -a -l -F -f /tmp/pat /proc/[0-9]*/environ 2>/dev/null; then exit 1; fi
if grep -r -l -s -F -f /tmp/pat / --exclude-dir=proc --exclude-dir=sys 2>/dev/null | grep -v '^/tmp/pat$'; then exit 1; fi
exit 0`

// #245's canary: run B holds a credential while run A looks everywhere it can
// for it.
func TestARunCannotSeeAnotherRunsCredential(t *testing.T) {
	runtime := newRuntime(512)
	holder := newRun(t, runtime, config.ArtifactStoreConfig{})
	holder.template(t, "sleep 25")
	prober := newRun(t, runtime, config.ArtifactStoreConfig{})
	prober.template(t, canaryProbe)

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := holder.apply(context.Background(), t, map[string]string{"CANARY_B": "leak-me-7f3a"}); err != nil {
			t.Errorf("holder apply: %v", err)
		}
	})
	time.Sleep(8 * time.Second)
	if err := prober.apply(context.Background(), t, nil); err != nil {
		t.Fatalf("prober found run B's credential: %v\n%s", err, prober.log(t, "apply.log"))
	}
	wg.Wait()
}

// The worker's own environment never reaches tofu.
func TestTheWorkersSecretsStayOut(t *testing.T) {
	t.Setenv("S3_SECRET_ACCESS_KEY", "worker-secret-9c1e")
	r := newRun(t, newRuntime(512), config.ArtifactStoreConfig{})
	r.template(t, `printf '%s%s' 'worker-secret-' '9c1e' > /tmp/pat
if grep -a -l -F -f /tmp/pat /proc/[0-9]*/environ 2>/dev/null; then exit 1; fi
exit 0`)
	if err := r.apply(context.Background(), t, nil); err != nil {
		t.Fatalf("tofu saw the worker's secret: %v\n%s", err, r.log(t, "apply.log"))
	}
}

// A plan file swapped for a link to the sandbox's own environment, which holds
// its storage key, is refused, not uploaded.
func TestUploadPlanRefusesASymlinkedPlan(t *testing.T) {
	artifacts := t.TempDir()
	if err := os.Chown(artifacts, 0, 10001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(artifacts, 0o2770|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	store := config.ArtifactStoreConfig{Kind: config.ArtifactStoreFilesystem, FilesystemRoot: artifacts}
	r := newRun(t, newRuntime(512), store)
	r.template(t, `ln -sf /proc/self/environ tfplan`)
	if err := r.apply(context.Background(), t, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	key, _ := planbundle.NewKey()
	sealedKey, err := runseal.Seal(r.publicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	err = r.activities.UploadPlan(context.Background(), domain.PlanArtifactActivityInput{
		TenantID: "tenant", RunID: domain.TemplateRunID(r.id), TerraformPath: r.workspace, SealedPlanKey: sealedKey,
	})
	// On the plan file itself, not on anything else UploadPlan could trip on.
	if err == nil || !strings.Contains(err.Error(), "tfplan") {
		t.Fatalf("UploadPlan error = %v, want the symlinked plan refused", err)
	}
	entries, _ := os.ReadDir(artifacts)
	if len(entries) != 0 {
		t.Fatalf("the store holds %d entries, want none", len(entries))
	}
}

func TestNothingOutlivesItsActivity(t *testing.T) {
	r := newRun(t, newRuntime(512), config.ArtifactStoreConfig{})
	r.template(t, `nohup sleep 300 >/dev/null 2>&1 &`)
	if err := r.apply(context.Background(), t, nil); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	r.template(t, `if ps | grep -v grep | grep -q 'sleep 300'; then exit 1; fi`)
	if _, err := r.terraform(context.Background(), t, domain.TerraformCommandApplyAutoApprove, nil); err != nil {
		t.Fatalf("the first activity's process survived into the second: %v", err)
	}
}

// Review focus 2: the hog fails its own activity in words; its neighbour runs.
func TestAMemoryHogFailsOnlyItsOwnActivity(t *testing.T) {
	runtime := newRuntime(256)
	hog := newRun(t, runtime, config.ArtifactStoreConfig{})
	// Real bytes into the sandbox's memory-backed /tmp: zeros may never be
	// committed, and with swap allowed this pages out instead of failing.
	// busybox head takes no M suffix.
	hog.template(t, `yes xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx | head -c 1048576000 > /tmp/fill`)
	neighbour := newRun(t, runtime, config.ArtifactStoreConfig{})
	neighbour.template(t, `sleep 5`)

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := neighbour.apply(context.Background(), t, nil); err != nil {
			t.Errorf("neighbour: %v", err)
		}
	})
	err := hog.apply(context.Background(), t, nil)
	wg.Wait()
	if err == nil || !strings.Contains(err.Error(), "ran out of memory") {
		t.Fatalf("hog error = %v, want out of memory", err)
	}
}

func TestAForkBombIsContained(t *testing.T) {
	runtime := newRuntime(512)
	bomb := newRun(t, runtime, config.ArtifactStoreConfig{})
	bomb.template(t, `b(){ b|b & }; b; sleep 20`)
	neighbour := newRun(t, runtime, config.ArtifactStoreConfig{})
	neighbour.template(t, `sleep 5`)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { _ = bomb.apply(ctx, t, nil) })
	time.Sleep(3 * time.Second)
	if err := neighbour.apply(context.Background(), t, nil); err != nil {
		t.Fatalf("neighbour: %v", err)
	}
	if err := exec.Command("/bin/true").Run(); err != nil {
		t.Fatalf("the executor could not start a process during the bomb: %v", err)
	}
	cancel()
	wg.Wait()
}

func TestCancellingAnActivityRemovesItsSandbox(t *testing.T) {
	r := newRun(t, newRuntime(512), config.ArtifactStoreConfig{})
	r.template(t, `sleep 300`)
	if _, err := r.terraform(context.Background(), t, domain.TerraformCommandInit, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(5*time.Second, cancel)
	start := time.Now()
	if _, err := r.terraform(ctx, t, domain.TerraformCommandApplyAutoApprove, nil); err == nil {
		t.Fatal("a cancelled apply succeeded")
	}
	if took := time.Since(start); took > 25*time.Second {
		t.Fatalf("cancellation took %v", took)
	}
	out, _ := exec.Command(runsc, "--root", filepath.Join(state, "runsc"), "list", "-quiet").Output()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("sandboxes left: %s", out)
	}
}

// Review focus 5: no limits, no sandbox, and nothing left behind.
func TestNoCgroupsMeansNoSandbox(t *testing.T) {
	runtime := newRuntime(512)
	// The executor's own leaf holds processes, so cgroup v2 refuses to give
	// its children controllers: limits can't apply there. (A cgroup that
	// merely doesn't exist yet, runsc creates.)
	runtime.Cgroups = "/executor"
	r := newRun(t, runtime, config.ArtifactStoreConfig{})
	r.template(t, `echo BODY-RAN`)
	_, err := r.terraform(context.Background(), t, domain.TerraformCommandInit, nil)
	if err == nil {
		t.Fatal("a sandbox started without its cgroup")
	}
	t.Logf("refused: %v", err)
	if strings.Contains(r.log(t, "apply-init.log"), "Initializing") {
		t.Fatal("tofu ran without its limits")
	}
	if entries, _ := os.ReadDir(filepath.Join(state, "bundles")); len(entries) != 0 {
		t.Fatalf("%d bundles left behind", len(entries))
	}
	if entries, _ := os.ReadDir(filepath.Join(state, "netns")); len(entries) != 0 {
		t.Fatalf("%d network namespaces left behind", len(entries))
	}
}

// eth0Addresses are the executor's own addresses, which no sandbox may take.
func eth0Addresses(t *testing.T) []string {
	t.Helper()
	eth0, err := net.InterfaceByName("eth0")
	if err != nil {
		t.Fatal(err)
	}
	addrs, err := eth0.Addrs()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}

// The network (#245): a real clone and a real provider download, each from
// a sandbox of its own, while the executor keeps its address.
func TestASandboxReachesTheInternet(t *testing.T) {
	before := eth0Addresses(t)
	ctx := context.Background()
	r := newRun(t, newRuntime(512), config.ArtifactStoreConfig{})
	// octocat/Hello-World's first commit: tiny, public, and never moving.
	if _, err := r.activities.FetchSource(ctx, domain.FetchSourceActivityInput{
		TenantID: "tenant", RunID: domain.TemplateRunID(r.id), RepoOwner: "octocat", RepoName: "Hello-World",
		ResolvedCommitSHA: "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d", RootPath: ".",
	}); err != nil {
		t.Fatalf("FetchSource: %v", err)
	}
	r.write(t, `terraform {
  required_providers {
    random = { source = "hashicorp/random", version = "3.6.3" }
  }
}

resource "random_id" "probe" {
  byte_length = 4
}
`)
	if _, err := r.terraform(ctx, t, domain.TerraformCommandInit, nil); err != nil {
		t.Fatalf("init: %v\n%s", err, r.log(t, "apply-init.log"))
	}
	if !strings.Contains(r.log(t, "apply-init.log"), "Installed hashicorp/random v3.6.3") {
		t.Fatalf("init installed no provider:\n%s", r.log(t, "apply-init.log"))
	}
	if _, err := r.terraform(ctx, t, domain.TerraformCommandApplyAutoApprove, nil); err != nil {
		t.Fatalf("apply: %v\n%s", err, r.log(t, "apply.log"))
	}
	if after := eth0Addresses(t); !slices.Equal(before, after) {
		t.Fatalf("executor addresses %v became %v", before, after)
	}
}

var addressLine = regexp.MustCompile(`ADDRESS=([0-9.]+)/`)

// One sandbox can't connect to another, though the executor can reach both.
func TestSandboxesCannotReachEachOther(t *testing.T) {
	runtime := newRuntime(512)
	holder := newRun(t, runtime, config.ArtifactStoreConfig{})
	// The listener's output goes nowhere: holding the provisioner's, it would
	// keep tofu waiting after the sleep.
	holder.template(t, `nc -lk -p 8080 -e true >/dev/null 2>&1 &
ip -4 -o addr show eth0 | awk '{print "ADDRESS=" $4}'
sleep 25`)
	var wg sync.WaitGroup
	wg.Go(func() {
		if err := holder.apply(context.Background(), t, nil); err != nil {
			t.Errorf("holder apply: %v", err)
		}
	})
	defer wg.Wait()

	// The holder prints its address as its apply starts.
	var address string
	for deadline := time.Now().Add(20 * time.Second); address == "" && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if match := addressLine.FindStringSubmatch(holder.log(t, "apply.log")); match != nil {
			address = match[1]
		}
	}
	if address == "" {
		t.Fatalf("the holder printed no address:\n%s", holder.log(t, "apply.log"))
	}

	prober := newRun(t, runtime, config.ArtifactStoreConfig{})
	prober.template(t, fmt.Sprintf(`if nc -z -w 3 %s 8080; then echo REACHED-THE-NEIGHBOUR; exit 1; fi`, address))
	if err := prober.apply(context.Background(), t, nil); err != nil {
		t.Fatalf("a sandbox reached its neighbour at %s: %v\n%s", address, err, prober.log(t, "apply.log"))
	}
	// The control: the holder was listening all along, so the prober was
	// kept out rather than finding nothing there.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, "8080"), 3*time.Second)
	if err != nil {
		t.Fatalf("the executor can't reach the holder either (%v), so this proves nothing", err)
	}
	_ = conn.Close()
}

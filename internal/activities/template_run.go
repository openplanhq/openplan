package activities

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/logsink"
	"github.com/vishu42/openplan/internal/planbundle"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/runuser"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

type StatusRecorder interface {
	// RecordTemplateRunStatus persists one workflow status transition for a run.
	RecordTemplateRunStatus(context.Context, domain.TemplateRunStatusActivityInput) error
	// RecordTemplateRunStep persists the step a running run has started.
	RecordTemplateRunStep(context.Context, domain.TemplateRunStepActivityInput) error
	// RecordTemplateRunEvent persists something a running run did to its
	// stack template.
	RecordTemplateRunEvent(context.Context, domain.TemplateRunEventActivityInput) error
}

// TerraformRunner is the activity-local boundary for running Terraform.
//
// The production implementation shells out to the OpenTofu CLI, while tests can
// provide a fake runner to verify activity behavior without starting external
// processes.
type TerraformRunner interface {
	// RunTerraform executes the Terraform command requested by the workflow and
	// returns the metadata of the log it uploaded, if any, and for a plan
	// whether it has changes. A command that fails after its log was uploaded
	// returns both the log and the error.
	RunTerraform(context.Context, domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error)
}

// PlanArtifactStore keeps each run's encrypted saved plan between its plan
// phase and its apply phase.
type PlanArtifactStore interface {
	PutPlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, sealed []byte) error
	GetPlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error)
	DeletePlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) error
}

// TemplateRunLogStore persists output produced by a template run.
type TemplateRunLogStore interface {
	// PutTemplateRunLog stores the output for a run phase and returns the
	// metadata row describing it.
	PutTemplateRunLog(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, phase string, body io.Reader) (domain.TemplateRunLog, error)
}

type CredentialReader interface {
	// ListCredentialsForStackTemplate returns encrypted credentials inherited by a template.
	ListCredentialsForStackTemplate(context.Context, domain.TenantID, domain.StackTemplateID) ([]domain.CredentialSet, error)
}

type CredentialDecryptor interface {
	// Decrypt opens one stored credential. Only the control plane holds the key.
	Decrypt(string) (string, error)
}

// WorkspaceHelper does the work inside a session's workspace as the session's
// pool user (runhelper.Client). The executor never reads or writes there
// itself: root without CAP_DAC_OVERRIDE cannot, and a branch controls every
// name in it (#331).
type WorkspaceHelper interface {
	Kill(ctx context.Context, user runuser.User) error
	Reclaim(ctx context.Context, user runuser.User, workspace string) error
	Pack(ctx context.Context, user runuser.User, dir string) ([]byte, error)
	Unpack(ctx context.Context, user runuser.User, dir string, bundle []byte) error
	CheckRoot(ctx context.Context, user runuser.User, dir string) error
}

// Isolation is what runs each session as a pool user of its own.
type Isolation struct {
	Users  []runuser.User
	Helper WorkspaceHelper
}

const (
	// workspaceDirName and logsDirName are the two halves of a run directory:
	// the branch's, and the executor's.
	workspaceDirName = "workspace"
	logsDirName      = "logs"
)

// TemplateRunActivities are the execution activities: the ones that run next
// to tenant Terraform on the executor, which holds no database connection and
// no keys beyond its per-run sealing keys.
type TemplateRunActivities struct {
	// runRoot is the base directory under which per-tenant, per-run workspaces are created.
	runRoot string
	// terraformRunner executes Terraform-compatible commands for RunTerraform activity calls.
	terraformRunner TerraformRunner
	// git clones template source repositories into run workspaces.
	git runner.GitRunner
	// plans keeps saved plans between a run's plan and apply phases.
	plans PlanArtifactStore
	// keys holds each in-flight run's private sealing key.
	keys *runseal.KeyRing
	// users leases each session a pool user of its own.
	users *runuser.Pool
	// helper works inside a session's workspace as its pool user.
	helper WorkspaceHelper
}

// NewTemplateRunActivities constructs the execution activities.
//
// By default it wires a local OpenTofu-backed runner that uploads phase logs
// to logStore. Tests may pass a TerraformRunner override to avoid invoking the
// OpenTofu binary. A nil keys gets a fresh ring.
func NewTemplateRunActivities(runRoot string, logStore TemplateRunLogStore, plans PlanArtifactStore, keys *runseal.KeyRing, isolation Isolation, terraformRunners ...TerraformRunner) *TemplateRunActivities {
	terraformRunner := TerraformRunner(localTerraformRunner{
		runRoot:  runRoot,
		runner:   runner.NewIsolatedProcessRunner(isolation.Helper.Kill),
		logStore: logStore,
	})
	if len(terraformRunners) > 0 {
		terraformRunner = terraformRunners[0]
	}
	if keys == nil {
		keys = runseal.NewKeyRing()
	}
	activities := &TemplateRunActivities{
		runRoot:         runRoot,
		terraformRunner: terraformRunner,
		git:             runner.NewIsolatedGitRunner(isolation.Helper.Kill),
		plans:           plans,
		keys:            keys,
		helper:          isolation.Helper,
	}
	activities.users = runuser.NewPool(isolation.Users, activities.reclaim)
	return activities
}

// PrepareWorkspace leases the session a pool user and lays out its run
// directory: logs/ for the executor, and workspace/ with home/ and tmp/ for
// the branch.
//
// The run directory is derived from the configured run root plus tenant and
// run IDs, validated as single safe path components, so callers cannot escape
// the run root.
func (activities *TemplateRunActivities) PrepareWorkspace(ctx context.Context, input domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
	runDir, err := logsink.RunDirectory(activities.runRoot, input.TenantID, input.RunID)
	if err != nil {
		return domain.PrepareWorkspaceActivityOutput{}, err
	}
	id := domain.RunKeyID(input.TenantID, input.RunID)
	user, err := activities.users.Lease(ctx, id, runDir)
	if err != nil {
		return domain.PrepareWorkspaceActivityOutput{}, fmt.Errorf("lease a pool user: %w", err)
	}
	workspace, err := makeRunDirectory(runDir, user)
	if err != nil {
		// Hand the user back now rather than hold it for 25 hours.
		return domain.PrepareWorkspaceActivityOutput{}, errors.Join(err, activities.users.Release(ctx, id))
	}

	// The session pins every later activity of this run to this process, so the
	// key generated here is the one that opens what the control plane seals.
	publicKey, err := activities.keys.Generate(id)
	if err != nil {
		return domain.PrepareWorkspaceActivityOutput{}, errors.Join(err, activities.users.Release(ctx, id))
	}
	return domain.PrepareWorkspaceActivityOutput{WorkspacePath: workspace, PublicKey: publicKey}, nil
}

// makeRunDirectory lays out one session's directories and hands the branch its
// own. Everything is made as root first: once workspace/ belongs to the pool
// user, root (without CAP_DAC_OVERRIDE) can no longer create anything in it.
// No run code has executed yet and root owns every parent, so nothing can race.
func makeRunDirectory(runDir string, user runuser.User) (string, error) {
	// 0711 down to the run: a branch can walk to its own workspace but list
	// nobody's.
	if err := os.MkdirAll(runDir, 0o711); err != nil { //nolint:gosec // 0711 lets a branch through to its workspace, not list the runs
		return "", fmt.Errorf("prepare run directory: %w", err)
	}
	if err := os.Chmod(runDir, 0o711); err != nil { //nolint:gosec // as above
		return "", fmt.Errorf("prepare run directory: %w", err)
	}
	if err := os.Mkdir(filepath.Join(runDir, logsDirName), 0o700); err != nil {
		return "", fmt.Errorf("prepare log directory: %w", err)
	}
	workspace := filepath.Join(runDir, workspaceDirName)
	branchDirs := []string{workspace, filepath.Join(workspace, "home"), filepath.Join(workspace, "tmp")}
	for _, dir := range branchDirs {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return "", fmt.Errorf("prepare workspace: %w", err)
		}
	}
	// Innermost first: chowning workspace/ before its children would lock
	// root out of them.
	for _, dir := range slices.Backward(branchDirs) {
		if err := os.Lchown(dir, int(user.UID), int(user.GID)); err != nil {
			return "", fmt.Errorf("hand the workspace to %s: %w", user.Name, err)
		}
	}
	return workspace, nil
}

// CleanupWorkspace ends a session: it releases the run's pool user, whose
// reclaim kills the branch's processes and deletes the run directory, and on
// the apply phase also deletes the saved plan, which nothing reads after the
// apply.
//
// The run is found from its identity, never from the path in the input, so
// this can only ever remove a run directory under the run root. Both deletions
// are best effort from the workflow's point of view: a run directory left
// behind costs disk until the lease expires, and a saved plan left behind is
// unreadable once the run is terminal, because the control plane drops its
// key.
func (activities *TemplateRunActivities) CleanupWorkspace(ctx context.Context, input domain.CleanupWorkspaceActivityInput) error {
	// For the ID validation: the pool knows the run directory itself.
	if _, err := logsink.RunDirectory(activities.runRoot, input.TenantID, input.RunID); err != nil {
		return err
	}
	var errs []error
	if err := activities.users.Release(ctx, domain.RunKeyID(input.TenantID, input.RunID)); err != nil {
		errs = append(errs, fmt.Errorf("reclaim the run's branch: %w", err))
	}
	if input.DeletePlan && activities.plans != nil {
		if err := activities.plans.DeletePlan(ctx, input.TenantID, input.RunID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// reclaim is the pool's Reclaimer: it ends a session's branch and deletes its
// run directory. The helper empties workspace/ as the pool user; the rest of
// the run directory is root's.
func (activities *TemplateRunActivities) reclaim(ctx context.Context, user runuser.User, runDir string) error {
	workspace := filepath.Join(runDir, workspaceDirName)
	if _, err := os.Lstat(workspace); errors.Is(err, fs.ErrNotExist) {
		workspace = ""
	}
	if err := activities.helper.Reclaim(ctx, user, workspace); err != nil {
		return err
	}
	return os.RemoveAll(runDir)
}

// Sweep ends every branch an earlier executor process left behind. Run it once,
// before the worker takes sessions: after a restart no lease is valid, since
// the pool and the key ring died with the process. It fails closed: an
// executor that cannot clear a pool user must not hand that user out.
func (activities *TemplateRunActivities) Sweep(ctx context.Context) error {
	var errs []error
	for _, user := range activities.users.Users() {
		if err := activities.helper.Reclaim(ctx, user, ""); err != nil {
			errs = append(errs, err)
		}
	}
	tenants, err := os.ReadDir(activities.runRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return errors.Join(errs...)
	}
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, tenant := range tenants {
		runs, err := os.ReadDir(filepath.Join(activities.runRoot, tenant.Name()))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, run := range runs {
			if err := activities.sweepRunDirectory(ctx, filepath.Join(activities.runRoot, tenant.Name(), run.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (activities *TemplateRunActivities) sweepRunDirectory(ctx context.Context, runDir string) error {
	if err := activities.sweepWorkspace(ctx, filepath.Join(runDir, workspaceDirName)); err != nil {
		return fmt.Errorf("sweep %s: %w", runDir, err)
	}
	return os.RemoveAll(runDir)
}

// sweepWorkspace empties workspace as the pool user who owns it, so root can
// then remove it.
func (activities *TemplateRunActivities) sweepWorkspace(ctx context.Context, workspace string) error {
	info, err := os.Lstat(workspace)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		// RemoveAll removes a symlink itself, never what it points to.
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("workspace has no owner")
	}
	// Root's: a PrepareWorkspace that died before the chown.
	if stat.Uid == 0 {
		return nil
	}
	user, ok := activities.users.ByUID(stat.Uid)
	if !ok {
		return fmt.Errorf("workspace owner %d is not a pool user", stat.Uid)
	}
	return activities.helper.Reclaim(ctx, user, workspace)
}

// UploadPlan encrypts the saved plan the plan command left in TerraformPath,
// with the lock file it was made against, and stores it for the apply phase.
//
// The key arrives sealed to this run's key, so it exists in plaintext only
// here and on the control plane. The bundle is bound to the run, so a bundle
// stored under one run's key cannot be passed off as another's.
func (activities *TemplateRunActivities) UploadPlan(ctx context.Context, input domain.PlanArtifactActivityInput) error {
	key, err := activities.openPlanKey(input)
	if err != nil {
		return err
	}
	ctx, user, err := activities.branch(ctx, input.TenantID, input.RunID)
	if err != nil {
		return err
	}
	// The helper reads the plan as the pool user, so a plan file the branch
	// swapped for a symlink yields only what the branch could read anyway.
	bundle, err := activities.helper.Pack(ctx, user, input.TerraformPath)
	if err != nil {
		return fmt.Errorf("pack saved plan: %w", err)
	}
	sealed, err := planbundle.Seal(key, bundle, domain.RunKeyID(input.TenantID, input.RunID))
	if err != nil {
		return fmt.Errorf("seal saved plan: %w", err)
	}
	if err := activities.requirePlans().PutPlan(ctx, input.TenantID, input.RunID, sealed); err != nil {
		return err
	}
	return nil
}

// DownloadPlan puts a run's saved plan, and the lock file it was made against,
// back into TerraformPath before init, so init installs the providers the plan
// expects and apply finds the plan where the plan phase left it.
func (activities *TemplateRunActivities) DownloadPlan(ctx context.Context, input domain.PlanArtifactActivityInput) error {
	key, err := activities.openPlanKey(input)
	if err != nil {
		return err
	}
	sealed, err := activities.requirePlans().GetPlan(ctx, input.TenantID, input.RunID)
	if err != nil {
		return err
	}
	bundle, err := planbundle.Open(key, sealed, domain.RunKeyID(input.TenantID, input.RunID))
	if err != nil {
		return err
	}
	ctx, user, err := activities.branch(ctx, input.TenantID, input.RunID)
	if err != nil {
		return err
	}
	// The key never leaves the executor: only the plaintext bundle crosses the
	// pipe to the helper, which writes it as the pool user.
	if err := activities.helper.Unpack(ctx, user, input.TerraformPath, bundle); err != nil {
		return fmt.Errorf("unpack saved plan: %w", err)
	}
	return nil
}

func (activities *TemplateRunActivities) openPlanKey(input domain.PlanArtifactActivityInput) ([]byte, error) {
	if strings.TrimSpace(input.TerraformPath) == "" {
		return nil, fmt.Errorf("terraform path is required")
	}
	var key []byte
	if err := activities.keys.Open(domain.RunKeyID(input.TenantID, input.RunID), input.SealedPlanKey, &key); err != nil {
		return nil, fmt.Errorf("open plan key: %w", err)
	}
	return key, nil
}

// branch returns ctx carrying the session's branch: its pool user, and HOME
// and TMPDIR inside its workspace. It fails when the session holds no lease,
// so a step never runs as another session's user.
func (activities *TemplateRunActivities) branch(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) (context.Context, runuser.User, error) {
	runDir, err := logsink.RunDirectory(activities.runRoot, tenantID, runID)
	if err != nil {
		return nil, runuser.User{}, err
	}
	user, err := activities.users.Lookup(domain.RunKeyID(tenantID, runID))
	if err != nil {
		return nil, runuser.User{}, err
	}
	workspace := filepath.Join(runDir, workspaceDirName)
	return runuser.WithBranch(ctx, runuser.Branch{
		User:    user,
		Home:    filepath.Join(workspace, "home"),
		TempDir: filepath.Join(workspace, "tmp"),
	}), user, nil
}

// requirePlans returns the plan store, or one that fails every call when none
// was wired, so a misconfigured executor fails the run instead of panicking.
func (activities *TemplateRunActivities) requirePlans() PlanArtifactStore {
	if activities.plans == nil {
		return missingPlanStore{}
	}
	return activities.plans
}

type missingPlanStore struct{}

var errNoPlanStore = errors.New("executor has no plan store")

func (missingPlanStore) PutPlan(context.Context, domain.TenantID, domain.TemplateRunID, []byte) error {
	return errNoPlanStore
}

func (missingPlanStore) GetPlan(context.Context, domain.TenantID, domain.TemplateRunID) ([]byte, error) {
	return nil, errNoPlanStore
}

func (missingPlanStore) DeletePlan(context.Context, domain.TenantID, domain.TemplateRunID) error {
	return errNoPlanStore
}

// ReleaseRunKey drops a finished run's sealing key, so nothing sealed to it can
// be opened afterwards.
func (activities *TemplateRunActivities) ReleaseRunKey(_ context.Context, input domain.ReleaseRunKeyActivityInput) error {
	activities.keys.Forget(domain.RunKeyID(input.TenantID, input.RunID))
	return nil
}

// FetchSource clones the template source into the prepared run workspace.
//
// The full repository is cloned under a stable "source" directory, while the
// returned TerraformPath points at the configured template root inside that
// clone. Keeping WorkspacePath separate lets log files remain run-scoped even
// when Terraform executes from a nested module directory.
func (activities *TemplateRunActivities) FetchSource(ctx context.Context, input domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
	rootPath, err := safeTemplateRootPath(input.RootPath)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, fmt.Errorf("source root path: %w", err)
	}
	if strings.TrimSpace(input.WorkspacePath) == "" {
		return domain.FetchSourceActivityOutput{}, fmt.Errorf("workspace path is required")
	}
	ctx, user, err := activities.branch(ctx, input.TenantID, input.RunID)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, err
	}

	sourcePath := filepath.Join(input.WorkspacePath, "source")
	git := activities.git
	repoURL, err := gitHubRepoURL(input.RepoOwner, input.RepoName)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, err
	}
	// The control plane resolved the token best effort and sealed it to this
	// run's key; an empty token means the fetch proceeds unauthenticated, with
	// FetchHint explaining why should it then fail.
	var credential runner.GitCredential
	if len(input.SealedToken) > 0 {
		var token string
		if err := activities.keys.Open(domain.RunKeyID(input.TenantID, input.RunID), input.SealedToken, &token); err != nil {
			return domain.FetchSourceActivityOutput{}, fmt.Errorf("open source token: %w", err)
		}
		if token != "" {
			credential = runner.NewGitCredential(token)
		}
	}
	// The commit is what the revision means, so it is what runs. Checking out a
	// ref would let the source move between a plan and the apply that was
	// approved against it, and would ignore the revision entirely once an
	// upgrade pointed the component somewhere the installed ref never reached.
	//
	// The ref remains the fallback only for runs queued before the commit was
	// threaded through; those payloads have no commit to check out. Once such a
	// queue has drained the branch is dead, and with it the last path by which a
	// run resolves its own source.
	if commitSHA := strings.TrimSpace(input.ResolvedCommitSHA); commitSHA != "" {
		if err := git.CheckoutCommit(ctx, repoURL, commitSHA, sourcePath, credential); err != nil {
			return domain.FetchSourceActivityOutput{}, fmt.Errorf("checkout source commit %s: %w%s", commitSHA, err, input.FetchHint)
		}
	} else if err := git.Clone(ctx, repoURL, input.SourceRef, sourcePath, credential); err != nil {
		return domain.FetchSourceActivityOutput{}, fmt.Errorf("clone source: %w%s", err, input.FetchHint)
	}

	terraformPath := filepath.Clean(filepath.Join(sourcePath, rootPath))
	// As the pool user: the executor never looks inside a workspace itself.
	if err := activities.helper.CheckRoot(ctx, user, terraformPath); err != nil {
		return domain.FetchSourceActivityOutput{}, fmt.Errorf("source root %q: %w", rootPath, err)
	}

	return domain.FetchSourceActivityOutput{TerraformPath: terraformPath}, nil
}

// RunTerraform executes one Terraform phase requested by TemplatePlanWorkflow.
//
// It opens the credentials the control plane sealed to this run's key, then
// delegates command selection, log handling, and subprocess execution to the
// configured TerraformRunner. Plaintext credentials exist only in this process
// and the Terraform subprocess, never in workflow history.
//
// A command that fails after its log was uploaded is reported as an
// ApplicationError carrying the log metadata as details, so the workflow can
// still record the log a user needs to see why the command failed.
func (activities *TemplateRunActivities) RunTerraform(ctx context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	input.Environment = nil
	if len(input.SealedEnvironment) > 0 {
		var environment map[string]string
		if err := activities.keys.Open(domain.RunKeyID(input.TenantID, input.RunID), input.SealedEnvironment, &environment); err != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("open terraform credentials: %w", err)
		}
		input.Environment = environment
	}
	ctx, _, err := activities.branch(ctx, input.TenantID, input.RunID)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	output, err := activities.terraformRunner.RunTerraform(ctx, input)
	if err != nil {
		if output.Log.ObjectKey == "" {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("run terraform: %w", err)
		}
		return domain.RunTerraformActivityOutput{}, temporal.NewApplicationErrorWithOptions("run terraform", domain.TerraformCommandFailedErrorType, temporal.ApplicationErrorOptions{
			Cause:   err,
			Details: []any{output.Log},
		})
	}
	return output, nil
}

// localTerraformRunner adapts the shared runner package to the activity interface.
//
// It adds activity-specific concerns around Terraform-compatible execution, such as mapping
// workflow command types to log phases and opening the per-workspace log file
// before delegating to runner.LocalProcessRunner.
type localTerraformRunner struct {
	// runRoot is where the run directories, and so the log spools, live.
	runRoot string
	// runner owns Terraform CLI argument construction and subprocess execution.
	runner   *runner.LocalProcessRunner
	logStore TemplateRunLogStore
	// heartbeat reports liveness to Temporal. Tests replace it; a zero value
	// means recordActivityHeartbeat, which is a no-op off an activity context.
	heartbeat func(context.Context)
	// heartbeatInterval overrides domain.TerraformHeartbeatInterval in tests
	// that cannot wait twenty seconds for a tick.
	heartbeatInterval time.Duration
}

// recordActivityHeartbeat reports to Temporal when there is an activity to
// report to. The same runner is exercised directly by tests holding an
// ordinary context, where recording a heartbeat would panic.
//
// The heartbeat carries no details. Temporal does not read them: what keeps an
// activity alive is that a heartbeat arrived, not what it said, and the
// timeout behaves identically whether the payload describes the command's
// progress or is empty. Progress reporting is worth adding back the day
// someone needs to diagnose a stalled run from the Temporal UI, and it would
// have to carry what the command has produced and how long it has been quiet,
// since neither is derivable from the heartbeat timestamps Temporal keeps on
// its own. Nothing reads it today, so nothing is sent.
func recordActivityHeartbeat(ctx context.Context) {
	if !activity.IsActivity(ctx) {
		return
	}
	activity.RecordHeartbeat(ctx)
}

// RunTerraform writes command output to the run's log file and runs OpenTofu.
//
// The log phase is derived from the Terraform command and the run phase it belongs to
// so each phase writes to a predictable file under the run directory's logs/,
// which the branch cannot see: a symlink planted in the workspace would
// otherwise have root append tofu's output to a file of root's. The file is opened for append, so commands sharing a phase leave
// their output in the order they ran. Stdout and stderr share
// the same writer for now, preserving command output ordering in a single phase
// log. The log file is closed after the command completes, and close errors are
// surfaced only when the command itself succeeded.
func (localRunner localTerraformRunner) RunTerraform(ctx context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	fileName, err := logsink.FileNameForTerraformCommand(input.Command, input.RunPhase)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	phase := strings.TrimSuffix(fileName, logsink.LogFileExtension)

	runDir, err := logsink.RunDirectory(localRunner.runRoot, input.TenantID, input.RunID)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	writer, err := logsink.NewFileSink(runDir).Open(fileName)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("open terraform log: %w", err)
	}

	redactingWriter := newRedactingWriter(writer, credentialValues(input.Environment))
	stopHeartbeat := localRunner.startHeartbeat(ctx)
	result, runErr := localRunner.runner.Run(ctx, runner.TerraformCommand{
		WorkspacePath: terraformPath(input),
		WorkspaceName: input.WorkspaceName,
		Command:       input.Command,
		ConfigJSON:    input.ConfigJSON,
		Environment:   input.Environment,
		Stdout:        redactingWriter,
		Stderr:        redactingWriter,
	})
	stopHeartbeat()
	closeErr := redactingWriter.Close()
	if closeErr != nil {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("close terraform log: %w", closeErr)
	}
	var log domain.TemplateRunLog
	if localRunner.logStore != nil {
		file, err := os.Open(filepath.Join(runDir, logsDirName, fileName))
		if err != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("open terraform log for upload: %w", err)
		}
		uploaded, uploadErr := localRunner.logStore.PutTemplateRunLog(ctx, input.TenantID, input.RunID, phase, file)
		closeErr := file.Close()
		if uploadErr != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("upload terraform log: %w", uploadErr)
		}
		if closeErr != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("close terraform log after upload: %w", closeErr)
		}
		log = uploaded
	}
	return domain.RunTerraformActivityOutput{Log: log, HasChanges: result.HasChanges, Summary: result.Summary}, runErr
}

// startHeartbeat reports liveness to Temporal until the returned stop function
// is called.
//
// The ticker is what keeps a long apply alive: Temporal fails an activity that
// misses its heartbeat timeout, so a command that runs for the better part of
// an hour has to say so on its own, and only the executor knows it is still
// there. Stopping is synchronous -- the goroutine has ended before stop
// returns -- so no heartbeat is recorded after the activity has returned.
func (localRunner localTerraformRunner) startHeartbeat(ctx context.Context) (stop func()) {
	heartbeat := localRunner.heartbeat
	if heartbeat == nil {
		heartbeat = recordActivityHeartbeat
	}
	interval := localRunner.heartbeatInterval
	if interval <= 0 {
		interval = domain.TerraformHeartbeatInterval
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeat(ctx)
			}
		}
	}()

	return func() {
		close(done)
		<-stopped
	}
}

type redactingWriter struct {
	writer  io.WriteCloser
	secrets []string
}

// newRedactingWriter wraps a log destination and replaces exact credential values.
func newRedactingWriter(writer io.WriteCloser, secrets []string) *redactingWriter {
	return &redactingWriter{writer: writer, secrets: secrets}
}

// Write redacts configured secrets before forwarding command output to the destination.
func (writer *redactingWriter) Write(body []byte) (int, error) {
	redacted := append([]byte(nil), body...)
	for _, secret := range writer.secrets {
		redacted = bytes.ReplaceAll(redacted, []byte(secret), []byte("******"))
	}
	_, err := writer.writer.Write(redacted)
	if err != nil {
		return 0, err
	}
	return len(body), nil
}

// Close releases the wrapped log destination.
func (writer *redactingWriter) Close() error { return writer.writer.Close() }

// credentialValues extracts non-empty values so Terraform logs can be scrubbed.
func credentialValues(environment map[string]string) []string {
	secrets := make([]string, 0, len(environment))
	for _, value := range environment {
		if value != "" {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}

// resolveCredentialEnvironment decrypts inherited Stack credentials and applies StackTemplate overrides.
func resolveCredentialEnvironment(ctx context.Context, reader CredentialReader, decryptor CredentialDecryptor, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) (map[string]string, error) {
	credentials, err := reader.ListCredentialsForStackTemplate(ctx, tenantID, stackTemplateID)
	if err != nil {
		return nil, err
	}
	environment := make(map[string]string, len(credentials))
	for _, credential := range credentials {
		if credential.StackTemplateID != "" {
			continue
		}
		value, err := decryptor.Decrypt(credential.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt %q: %w", credential.Name, err)
		}
		environment[credential.Name] = value
	}
	for _, credential := range credentials {
		if credential.StackTemplateID == "" {
			continue
		}
		value, err := decryptor.Decrypt(credential.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt %q: %w", credential.Name, err)
		}
		environment[credential.Name] = value
	}
	return environment, nil
}

func terraformPath(input domain.RunTerraformActivityInput) string {
	if strings.TrimSpace(input.TerraformPath) != "" {
		return input.TerraformPath
	}
	return input.WorkspacePath
}

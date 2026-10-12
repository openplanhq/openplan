package activities

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/isolation"
	"github.com/vishu42/openplan/internal/logsink"
	"github.com/vishu42/openplan/internal/planbundle"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/runseal"
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

// TemplateRunActivities are the execution activities: the ones that run next
// to tenant Terraform on the executor, which holds no database connection and
// no keys beyond its per-run sealing keys.
type TemplateRunActivities struct {
	// terraformRunner executes Terraform-compatible commands for RunTerraform activity calls.
	terraformRunner TerraformRunner
	// git clones template source repositories into run workspaces.
	git runner.GitRunner
	// plans keeps saved plans between a run's plan and apply phases.
	plans PlanArtifactStore
	// keys holds each in-flight run's private sealing key.
	keys *runseal.KeyRing
	// sessions runs each run's session as a pool user of its own (#331), in a
	// run directory it lays out under its run root.
	sessions *isolation.Manager
}

// NewTemplateRunActivities constructs the execution activities. Every
// subprocess they start runs in its run's session, through sessions.
//
// By default it wires a local OpenTofu-backed runner that uploads phase logs
// to logStore. Tests may pass a TerraformRunner override to avoid invoking the
// OpenTofu binary. A nil keys gets a fresh ring.
func NewTemplateRunActivities(logStore TemplateRunLogStore, plans PlanArtifactStore, keys *runseal.KeyRing, sessions *isolation.Manager, terraformRunners ...TerraformRunner) *TemplateRunActivities {
	terraformRunner := TerraformRunner(localTerraformRunner{
		sessions: sessions,
		runner:   runner.NewLocalProcessRunnerWithExecutor(isolation.SessionExecutor()),
		logStore: logStore,
	})
	if len(terraformRunners) > 0 {
		terraformRunner = terraformRunners[0]
	}
	if keys == nil {
		keys = runseal.NewKeyRing()
	}
	return &TemplateRunActivities{
		terraformRunner: terraformRunner,
		git:             runner.NewLocalGitRunnerWithExecutor(isolation.SessionExecutor()),
		plans:           plans,
		keys:            keys,
		sessions:        sessions,
	}
}

// PrepareWorkspace opens the run's session, which lays out the run's directory
// under the run root: the session's workspace/, and the executor's logs/
// beside it. The session checks that the tenant and run IDs are single path
// components, so no input can place a run directory outside the run root.
func (activities *TemplateRunActivities) PrepareWorkspace(ctx context.Context, input domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
	session, err := activities.sessions.Open(ctx, string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.PrepareWorkspaceActivityOutput{}, fmt.Errorf("open the run's session: %w", err)
	}

	// The session pins every later activity of this run to this process, so the
	// key generated here is the one that opens what the control plane seals.
	publicKey, err := activities.keys.Generate(domain.RunKeyID(input.TenantID, input.RunID))
	if err != nil {
		// Ending the session hands the user back now rather than hold it until
		// its lease expires.
		return domain.PrepareWorkspaceActivityOutput{}, errors.Join(err, activities.sessions.Close(ctx, string(input.TenantID), string(input.RunID)))
	}
	return domain.PrepareWorkspaceActivityOutput{WorkspacePath: session.Workspace(), PublicKey: publicKey}, nil
}

// CleanupWorkspace ends the run's session, which kills the session's
// processes and deletes the run directory, and on the apply phase also deletes
// the saved plan, which nothing reads after the apply.
//
// The run is found from its identity, never from the path in the input, so
// this can only ever remove a run directory under the run root. Both deletions
// are best effort from the workflow's point of view: a run directory left
// behind costs disk until the lease expires, and a saved plan left behind is
// unreadable once the run is terminal, because the control plane drops its
// key.
func (activities *TemplateRunActivities) CleanupWorkspace(ctx context.Context, input domain.CleanupWorkspaceActivityInput) error {
	var errs []error
	if err := activities.sessions.Close(ctx, string(input.TenantID), string(input.RunID)); err != nil {
		errs = append(errs, fmt.Errorf("end the run's session: %w", err))
	}
	if input.DeletePlan && activities.plans != nil {
		if err := activities.plans.DeletePlan(ctx, input.TenantID, input.RunID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
	session, err := activities.session(input.TenantID, input.RunID)
	if err != nil {
		return err
	}
	// Read as the session's user, so a plan file the session swapped for a
	// symlink yields only what the session could read anyway.
	files, err := session.ReadFiles(ctx, input.TerraformPath, planbundle.Files)
	if err != nil {
		return fmt.Errorf("read saved plan: %w", err)
	}
	bundle, err := planbundle.Pack(files)
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
	files, err := planbundle.Unpack(bundle)
	if err != nil {
		return err
	}
	session, err := activities.session(input.TenantID, input.RunID)
	if err != nil {
		return err
	}
	// The key never leaves the executor: only the plaintext files cross to
	// the session, written as its user.
	if err := session.WriteFiles(ctx, input.TerraformPath, files); err != nil {
		return fmt.Errorf("write saved plan: %w", err)
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

// session returns the run's session. It fails when the run holds none, so a
// step never runs as another session's user, or as whichever user a
// restarted executor would hand out.
func (activities *TemplateRunActivities) session(tenantID domain.TenantID, runID domain.TemplateRunID) (*isolation.Session, error) {
	return activities.sessions.Session(string(tenantID), string(runID))
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
	session, err := activities.session(input.TenantID, input.RunID)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, err
	}
	ctx = session.Context(ctx)

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
	// As the session's user: the executor never looks inside a workspace itself.
	if err := session.CheckDir(ctx, terraformPath); err != nil {
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
	session, err := activities.session(input.TenantID, input.RunID)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	output, err := activities.terraformRunner.RunTerraform(session.Context(ctx), input)
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
	// sessions holds each run's session, and with it the run's log spool.
	sessions *isolation.Manager
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
// which the session cannot see: a symlink planted in the workspace would
// otherwise have root append tofu's output to a file of root's. The file is
// opened for append, so commands sharing a phase leave their output in the
// order they ran. Stdout and stderr share
// the same writer for now, preserving command output ordering in a single phase
// log. The log file is closed after the command completes, and close errors are
// surfaced only when the command itself succeeded.
func (localRunner localTerraformRunner) RunTerraform(ctx context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	fileName, err := logsink.FileNameForTerraformCommand(input.Command, input.RunPhase)
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	phase := strings.TrimSuffix(fileName, logsink.LogFileExtension)

	session, err := localRunner.sessions.Session(string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	writer, err := logsink.NewFileSink(session.LogsDir()).Open(fileName)
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
		file, err := os.Open(filepath.Join(session.LogsDir(), fileName))
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

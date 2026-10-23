package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/logsink"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/sandbox"
	"go.temporal.io/sdk/activity"
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

// PlanArtifactStore keeps each run's encrypted saved plan between its plan
// phase and its apply phase.
type PlanArtifactStore interface {
	PutPlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, sealed []byte) error
	GetPlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error)
	DeletePlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) error
}

// PlanChangesStore keeps a run's plan's resource changes, masked, between the
// executor that read them and the control plane that records them.
type PlanChangesStore interface {
	PutPlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, body []byte) error
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

// artifactsGroup is the group, openplan, that Dockerfile.executor and
// Dockerfile.api share the filesystem artifact store through. A storage
// sandbox's user joins it to write there.
const artifactsGroup = 10001

// TemplateRunActivities are the execution activities. Each runs its body in a
// sandbox of its own through sandbox; what stays here is our code alone: the
// run keys, the opened inputs, the log spool and the heartbeat.
type TemplateRunActivities struct {
	sandbox    sandbox.Runner
	workspaces sandbox.Workspaces
	// store is the artifact store's configuration, passed only into the
	// sandboxes that run no tenant code: UploadLog, UploadPlan, DownloadPlan.
	store config.ArtifactStoreConfig
	// plans deletes an applied run's saved plan, from here: CleanupWorkspace
	// runs no tenant code.
	plans PlanArtifactStore
	// keys holds each in-flight run's private sealing key. It never enters a
	// sandbox.
	keys *runseal.KeyRing
	// heartbeat reports liveness to Temporal. Tests replace it; a zero value
	// means recordActivityHeartbeat, which is a no-op off an activity context.
	heartbeat func(context.Context)
	// heartbeatInterval overrides domain.TerraformHeartbeatInterval in tests
	// that cannot wait twenty seconds for a tick.
	heartbeatInterval time.Duration
	// live is where RunTerraform publishes each command's spool as it grows,
	// from this process: the sandbox never holds the Valkey credential. Nil
	// leaves live logs off.
	live logsink.Stream
}

// NewTemplateRunActivities constructs the execution activities. A nil keys
// gets a fresh ring.
func NewTemplateRunActivities(runner sandbox.Runner, workspaces sandbox.Workspaces, store config.ArtifactStoreConfig, plans PlanArtifactStore, keys *runseal.KeyRing) *TemplateRunActivities {
	if keys == nil {
		keys = runseal.NewKeyRing()
	}
	return &TemplateRunActivities{sandbox: runner, workspaces: workspaces, store: store, plans: plans, keys: keys}
}

// WithLiveLogs publishes each command's output live to stream. A nil stream
// leaves live logs off; only tests pass nil, since the executor will not start
// without its Valkey.
func (activities *TemplateRunActivities) WithLiveLogs(stream logsink.Stream) *TemplateRunActivities {
	activities.live = stream
	return activities
}

// PrepareWorkspace lays out the run's directory and makes the run's sealing
// key. Temporal's session pins every later activity of the run to this
// process, so this key is the one that opens what the control plane seals.
func (activities *TemplateRunActivities) PrepareWorkspace(ctx context.Context, input domain.PrepareWorkspaceActivityInput) (domain.PrepareWorkspaceActivityOutput, error) {
	workspace, err := activities.workspaces.Prepare(ctx, string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.PrepareWorkspaceActivityOutput{}, fmt.Errorf("prepare the run's workspace: %w", err)
	}
	publicKey, err := activities.keys.Generate(domain.RunKeyID(input.TenantID, input.RunID))
	if err != nil {
		return domain.PrepareWorkspaceActivityOutput{}, errors.Join(err, activities.workspaces.Remove(ctx, string(input.TenantID), string(input.RunID)))
	}
	return domain.PrepareWorkspaceActivityOutput{WorkspacePath: workspace, PublicKey: publicKey}, nil
}

// CleanupWorkspace deletes the run's directory and, on the apply phase, its
// saved plan. The run is found from its identity, never from the path in the
// input, so this only ever removes a run directory under the run root.
func (activities *TemplateRunActivities) CleanupWorkspace(ctx context.Context, input domain.CleanupWorkspaceActivityInput) error {
	var errs []error
	if err := activities.workspaces.Remove(ctx, string(input.TenantID), string(input.RunID)); err != nil {
		errs = append(errs, fmt.Errorf("remove the run's workspace: %w", err))
	}
	if input.DeletePlan && activities.plans != nil {
		if err := activities.plans.DeletePlan(ctx, input.TenantID, input.RunID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// FetchSource checks the template source out into the run's workspace, in a
// sandbox with the repository's read-only token.
func (activities *TemplateRunActivities) FetchSource(ctx context.Context, input domain.FetchSourceActivityInput) (domain.FetchSourceActivityOutput, error) {
	rootPath, err := safeTemplateRootPath(input.RootPath)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, fmt.Errorf("source root path: %w", err)
	}
	workspace, err := activities.workspaces.Workspace(string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.FetchSourceActivityOutput{}, err
	}
	repoURL, err := gitHubRepoURL(input.RepoOwner, input.RepoName)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, err
	}
	// Best effort on the control plane: an empty token fetches
	// unauthenticated, with FetchHint explaining why should that fail.
	var token string
	if len(input.SealedToken) > 0 {
		if err := activities.keys.Open(domain.RunKeyID(input.TenantID, input.RunID), input.SealedToken, &token); err != nil {
			return domain.FetchSourceActivityOutput{}, fmt.Errorf("open source token: %w", err)
		}
	}
	sourcePath := filepath.Join(workspace, "source")
	terraformPath := filepath.Clean(filepath.Join(sourcePath, rootPath))
	// The commit is what the revision means, so it is what runs; the ref is
	// the fallback only for runs queued before the commit was threaded
	// through.
	_, err = activities.sandbox.Run(ctx, sandbox.Job{
		Activity: bodyFetchSource,
		Request: fetchSourceRequest{
			RepoURL: repoURL, CommitSHA: strings.TrimSpace(input.ResolvedCommitSHA), Ref: input.SourceRef,
			SourcePath: sourcePath, TerraformPath: terraformPath, Token: token,
		},
		Mounts: []sandbox.Mount{{Path: workspace}},
	}, io.Discard)
	if err != nil {
		return domain.FetchSourceActivityOutput{}, fmt.Errorf("fetch source: %w%s", err, input.FetchHint)
	}
	return domain.FetchSourceActivityOutput{TerraformPath: terraformPath}, nil
}

// RunTerraform runs one Terraform command in a sandbox with only the run's
// cloud credentials, spooling its output, redacted and capped, to the run's
// logs/, which no tofu sandbox mounts. With live logs on, the spool is also
// published to its stream as it grows. UploadLog uploads it afterwards,
// whether or not the command succeeded.
func (activities *TemplateRunActivities) RunTerraform(ctx context.Context, input domain.RunTerraformActivityInput) (domain.RunTerraformActivityOutput, error) {
	var credentials domain.RunCredentials
	if len(input.SealedEnvironment) > 0 {
		if err := activities.keys.Open(domain.RunKeyID(input.TenantID, input.RunID), input.SealedEnvironment, &credentials); err != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("open terraform credentials: %w", err)
		}
	}
	workspace, err := activities.workspaces.Workspace(string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	path, err := within(workspace, terraformPath(input))
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	if !domain.ValidLogName(input.LogName) {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("invalid log name %q", input.LogName)
	}
	logsDir, err := activities.workspaces.LogsDir(string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.RunTerraformActivityOutput{}, err
	}
	// A changes file left by an earlier attempt of this command would be
	// uploaded as this attempt's, so it goes before the command runs.
	changesPath := filepath.Join(logsDir, changesFileName(input.LogName))
	if err := os.Remove(changesPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("remove earlier plan changes: %w", err)
	}
	file, err := logsink.NewFileSink(logsDir).Open(logsink.FileName(input.LogName))
	if err != nil {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("open terraform log: %w", err)
	}
	spool := newRedactingWriter(newCappedWriter(file, spoolLimit), redactedValues(credentials))
	// The flusher tails the spool file, not the writer, so the command's
	// output never waits on Valkey, and what it publishes is already
	// redacted and capped.
	stopLive := func() {}
	if activities.live != nil {
		flusher := logsink.NewFlusher(activities.live, logsink.StreamKey(input.RunID, input.LogName),
			filepath.Join(logsDir, logsink.FileName(input.LogName)))
		stopLive = flusher.Start(ctx)
	}

	stopHeartbeat := activities.startHeartbeat(ctx)
	data, runErr := activities.sandbox.Run(ctx, sandbox.Job{
		Activity: bodyRunTerraform,
		Request: runTerraformRequest{
			TerraformPath: path, WorkspaceName: input.WorkspaceName, Command: input.Command,
			ConfigJSON: input.ConfigJSON, Environment: credentials.Environment,
		},
		Mounts: []sandbox.Mount{{Path: workspace}},
	}, spool)
	closeErr := spool.Close()
	// The final flush comes before the command's error is looked at, so a
	// failed command's last lines, usually the ones that say why, are
	// published too; and before the heartbeat stops, so the seconds it may
	// spend on a slow Valkey cannot time the activity out.
	stopLive()
	stopHeartbeat()
	if closeErr != nil && runErr == nil {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("close terraform log: %w", closeErr)
	}
	if runErr != nil {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("run terraform: %w", runErr)
	}
	var result runTerraformResult
	if err := json.Unmarshal(data, &result); err != nil {
		return domain.RunTerraformActivityOutput{}, fmt.Errorf("decode terraform result: %w", err)
	}
	// The changes go beside the log, where only this process writes, and
	// UploadLog takes them to the artifact store. They stay out of the
	// activity's output, so Temporal's history never holds them.
	if result.Changes != nil {
		encoded, err := json.Marshal(result.Changes)
		if err != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("encode plan changes: %w", err)
		}
		//nolint:gosec // UploadLog's sandbox reads it as the sandbox user, as it does the log
		if err := os.WriteFile(changesPath, encoded, 0o644); err != nil {
			return domain.RunTerraformActivityOutput{}, fmt.Errorf("write plan changes: %w", err)
		}
	}
	return domain.RunTerraformActivityOutput{HasChanges: result.HasChanges, Summary: result.Summary}, nil
}

// changesFileName is the file a plan command's resource changes wait in, in
// the run's logs/ beside the command's log.
func changesFileName(logName string) string {
	return logName + ".changes.json"
}

// UploadLog uploads the log RunTerraform spooled for a command, from a
// sandbox that sees only the spool and the artifact store.
func (activities *TemplateRunActivities) UploadLog(ctx context.Context, input domain.UploadLogActivityInput) (domain.TemplateRunLog, error) {
	if !domain.ValidLogName(input.LogName) {
		return domain.TemplateRunLog{}, fmt.Errorf("invalid log name %q", input.LogName)
	}
	logsDir, err := activities.workspaces.LogsDir(string(input.TenantID), string(input.RunID))
	if err != nil {
		return domain.TemplateRunLog{}, err
	}
	mounts, groups := activities.storeAccess()
	data, err := activities.sandbox.Run(ctx, sandbox.Job{
		Activity: bodyUploadLog,
		Request: uploadLogRequest{
			Store: activities.store, TenantID: input.TenantID, RunID: input.RunID,
			Phase: input.LogName, LogsDir: logsDir, FileName: logsink.FileName(input.LogName),
			ChangesFileName: changesFileName(input.LogName),
		},
		Mounts: append([]sandbox.Mount{{Path: logsDir, ReadOnly: true}}, mounts...),
		Groups: groups,
	}, io.Discard)
	if err != nil {
		return domain.TemplateRunLog{}, fmt.Errorf("upload terraform log: %w", err)
	}
	var log domain.TemplateRunLog
	if err := json.Unmarshal(data, &log); err != nil {
		return domain.TemplateRunLog{}, fmt.Errorf("decode the uploaded log: %w", err)
	}
	return log, nil
}

// UploadPlan encrypts the saved plan and its lock file and stores them for
// the apply phase, from a sandbox that sees the workspace read-only.
func (activities *TemplateRunActivities) UploadPlan(ctx context.Context, input domain.PlanArtifactActivityInput) error {
	return activities.transferPlan(ctx, input, bodyUploadPlan, true)
}

// DownloadPlan puts a run's saved plan and lock file back before init, so
// init installs the providers the plan expects.
func (activities *TemplateRunActivities) DownloadPlan(ctx context.Context, input domain.PlanArtifactActivityInput) error {
	return activities.transferPlan(ctx, input, bodyDownloadPlan, false)
}

func (activities *TemplateRunActivities) transferPlan(ctx context.Context, input domain.PlanArtifactActivityInput, body string, readOnly bool) error {
	var key []byte
	if err := activities.keys.Open(domain.RunKeyID(input.TenantID, input.RunID), input.SealedPlanKey, &key); err != nil {
		return fmt.Errorf("open plan key: %w", err)
	}
	workspace, err := activities.workspaces.Workspace(string(input.TenantID), string(input.RunID))
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.TerraformPath) == "" {
		return errors.New("terraform path is required")
	}
	path, err := within(workspace, input.TerraformPath)
	if err != nil {
		return err
	}
	mounts, groups := activities.storeAccess()
	_, err = activities.sandbox.Run(ctx, sandbox.Job{
		Activity: body,
		Request:  planRequest{Store: activities.store, TenantID: input.TenantID, RunID: input.RunID, TerraformPath: path, PlanKey: key},
		Mounts:   append([]sandbox.Mount{{Path: workspace, ReadOnly: readOnly}}, mounts...),
		Groups:   groups,
	}, io.Discard)
	return err
}

// storeAccess is what a storage sandbox needs beyond its request: on the
// filesystem store, the shared artifacts directory and its group.
func (activities *TemplateRunActivities) storeAccess() ([]sandbox.Mount, []uint32) {
	if activities.store.Kind != config.ArtifactStoreFilesystem {
		return nil, nil
	}
	return []sandbox.Mount{{Path: activities.store.FilesystemRoot}}, []uint32{artifactsGroup}
}

// within returns path, cleaned, if it is the workspace or inside it. Paths in
// activity inputs come from the workflow; checked here, a body is never
// pointed at a path its sandbox should not use.
func within(workspace, path string) (string, error) {
	clean := filepath.Clean(path)
	rel, err := filepath.Rel(workspace, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q is outside the run's workspace", path)
	}
	return clean, nil
}

// ReleaseRunKey drops a finished run's sealing key, so nothing sealed to it can
// be opened afterwards.
func (activities *TemplateRunActivities) ReleaseRunKey(_ context.Context, input domain.ReleaseRunKeyActivityInput) error {
	activities.keys.Forget(domain.RunKeyID(input.TenantID, input.RunID))
	return nil
}

// recordActivityHeartbeat reports to Temporal when there is an activity to
// report to. RunTerraform is exercised directly by tests holding an ordinary
// context, where recording a heartbeat would panic.
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

// startHeartbeat reports liveness to Temporal until the returned stop function
// is called.
//
// The ticker is what keeps a long apply alive: Temporal fails an activity that
// misses its heartbeat timeout, so a command that runs for the better part of
// an hour has to say so on its own, and only the executor knows it is still
// there. Stopping is synchronous -- the goroutine has ended before stop
// returns -- so no heartbeat is recorded after the activity has returned.
func (activities *TemplateRunActivities) startHeartbeat(ctx context.Context) (stop func()) {
	heartbeat := activities.heartbeat
	if heartbeat == nil {
		heartbeat = recordActivityHeartbeat
	}
	interval := activities.heartbeatInterval
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

// redactedValues lists the values to scrub from Terraform logs, longest first
// so a value that contains another is replaced whole: every non-empty value
// except those whose name is marked plain.
func redactedValues(credentials domain.RunCredentials) []string {
	secrets := make([]string, 0, len(credentials.Environment))
	for name, value := range credentials.Environment {
		if value != "" && !slices.Contains(credentials.Plain, name) && !slices.Contains(secrets, value) {
			secrets = append(secrets, value)
		}
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return secrets
}

// resolveCredentialEnvironment decrypts inherited Stack credentials and applies
// StackTemplate overrides. The entry that wins a name decides whether that
// name is plain, so an override changes sensitivity along with the value.
func resolveCredentialEnvironment(ctx context.Context, reader CredentialReader, decryptor CredentialDecryptor, tenantID domain.TenantID, stackTemplateID domain.StackTemplateID) (domain.RunCredentials, error) {
	credentials, err := reader.ListCredentialsForStackTemplate(ctx, tenantID, stackTemplateID)
	if err != nil {
		return domain.RunCredentials{}, err
	}
	environment := make(map[string]string, len(credentials))
	sensitive := make(map[string]bool, len(credentials))
	// Stack entries first, then template entries over them.
	for _, fromTemplate := range []bool{false, true} {
		for _, credential := range credentials {
			if (credential.StackTemplateID != "") != fromTemplate {
				continue
			}
			value, err := decryptor.Decrypt(credential.Ciphertext)
			if err != nil {
				return domain.RunCredentials{}, fmt.Errorf("decrypt %q: %w", credential.Name, err)
			}
			environment[credential.Name] = value
			sensitive[credential.Name] = credential.Sensitive
		}
	}
	var plain []string
	for name, isSensitive := range sensitive {
		if !isSensitive {
			plain = append(plain, name)
		}
	}
	slices.Sort(plain)
	return domain.RunCredentials{Environment: environment, Plain: plain}, nil
}

func terraformPath(input domain.RunTerraformActivityInput) string {
	if strings.TrimSpace(input.TerraformPath) != "" {
		return input.TerraformPath
	}
	return input.WorkspacePath
}

package main

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/sandbox"
	"github.com/vishu42/openplan/internal/temporal"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"
)

func TestRunRequiresTemporalAddress(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), func(string) string {
		return ""
	})
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
}

func TestRunWiresTemporalWorker(t *testing.T) {
	t.Parallel()

	deps := newRecordingExecutorDependencies(t)
	if err := runWithDependencies(context.Background(), executorTestEnv, deps.executorDependencies); err != nil {
		t.Fatalf("runWithDependencies returned error: %v", err)
	}

	if deps.temporalConfig.Address != "localhost:7233" {
		t.Fatalf("temporal address = %q, want localhost:7233", deps.temporalConfig.Address)
	}
	if deps.temporalConfig.Namespace != "openplan" {
		t.Fatalf("temporal namespace = %q, want openplan", deps.temporalConfig.Namespace)
	}
	// The API owns the control queue; this process must never poll it.
	if deps.workerTaskQueue != domain.ExecutionTaskQueue {
		t.Fatalf("worker task queue = %q, want %q", deps.workerTaskQueue, domain.ExecutionTaskQueue)
	}
	if !deps.workerOptions.EnableSessionWorker {
		t.Fatal("session worker was not enabled")
	}
	if deps.workerOptions.MaxConcurrentSessionExecutionSize != config.DefaultExecutorMaxSessions {
		t.Fatalf("max concurrent sessions = %d, want %d", deps.workerOptions.MaxConcurrentSessionExecutionSize, config.DefaultExecutorMaxSessions)
	}
	if !reflect.DeepEqual(deps.calls, []string{"newSandbox", "dialLiveLogs", "dialTemporal"}) {
		t.Fatalf("startup order = %v, want the sandbox readied and Valkey dialed before Temporal", deps.calls)
	}
	if deps.valkeyURL != "redis://localhost:6379" {
		t.Fatalf("dialLiveLogs got %q, want VALKEY_URL", deps.valkeyURL)
	}
	if deps.execution.live != deps.liveLogs {
		t.Fatal("activities did not get the client dialLiveLogs returned")
	}
	if deps.sandboxConfig.Runsc != config.DefaultExecutorSandboxRunsc {
		t.Fatalf("newSandbox got runsc %q, want %q", deps.sandboxConfig.Runsc, config.DefaultExecutorSandboxRunsc)
	}
	if deps.sandboxRunRoot != "/tmp/openplan-executor-test" {
		t.Fatalf("newSandbox got run root %q, want /tmp/openplan-executor-test", deps.sandboxRunRoot)
	}
	if deps.execution.runner != deps.runner {
		t.Fatal("activities did not get the runner newSandbox returned")
	}
	if deps.execution.keys == nil {
		t.Fatal("activities were not given a key ring")
	}
	if deps.execution.plans != deps.planStore {
		t.Fatal("activity plan store was not wired")
	}
	if deps.execution.workspaces.Root != "/tmp/openplan-executor-test" {
		t.Fatalf("activity workspaces root = %q, want /tmp/openplan-executor-test", deps.execution.workspaces.Root)
	}
	if deps.execution.store.FilesystemRoot != "/tmp/openplan-executor-artifacts" {
		t.Fatalf("activity artifact store root = %q, want /tmp/openplan-executor-artifacts", deps.execution.store.FilesystemRoot)
	}
	if deps.artifactStoreConfig.Kind != config.ArtifactStoreFilesystem {
		t.Fatalf("artifact store kind = %q, want filesystem", deps.artifactStoreConfig.Kind)
	}
	if deps.artifactStoreConfig.FilesystemRoot != "/tmp/openplan-executor-artifacts" {
		t.Fatalf("artifact store root = %q, want /tmp/openplan-executor-artifacts", deps.artifactStoreConfig.FilesystemRoot)
	}
	if !deps.worker.ran {
		t.Fatal("worker was not run")
	}
	if !deps.temporalClient.closed {
		t.Fatal("temporal client was not closed")
	}
	if err := deps.liveLogs.Ping(context.Background()).Err(); !errors.Is(err, redis.ErrClosed) {
		t.Fatalf("live log client ping after run = %v, want it closed", err)
	}
}

func TestDefaultExecutorDependenciesRegisterOnlyExecutionActivities(t *testing.T) {
	t.Parallel()

	worker := &recordingTemporalWorker{}
	deps := defaultExecutorDependencies()

	if err := deps.registerActivities(worker, execution{runner: sandbox.InProcess{}, keys: runseal.NewKeyRing(), workspaces: sandbox.Workspaces{Root: t.TempDir()}}); err != nil {
		t.Fatalf("registerActivities returned error: %v", err)
	}

	want := map[string]bool{
		domain.PrepareWorkspaceActivityName: true,
		domain.FetchSourceActivityName:      true,
		domain.RunTerraformActivityName:     true,
		domain.UploadLogActivityName:        true,
		domain.ReleaseRunKeyActivityName:    true,
		domain.CleanupWorkspaceActivityName: true,
		domain.UploadPlanActivityName:       true,
		domain.DownloadPlanActivityName:     true,
	}
	if !reflect.DeepEqual(worker.registeredActivities, want) {
		t.Fatalf("registered activities = %v, want %v", worker.registeredActivities, want)
	}
}

// An executor that cannot sandbox must not run template code: it stops before
// it ever polls for work.
func TestRunStopsWhenItCannotSandbox(t *testing.T) {
	t.Parallel()

	checkErr := errors.New("cgroup2 is read-only")
	deps := newRecordingExecutorDependencies(t)
	deps.sandboxErr = checkErr

	err := runWithDependencies(context.Background(), executorTestEnv, deps.executorDependencies)
	if !errors.Is(err, checkErr) || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("error = %v, want checkErr wrapped as sandbox", err)
	}
	if slices.Contains(deps.calls, "dialTemporal") {
		t.Fatalf("calls = %v, want no Temporal dial", deps.calls)
	}
}

// An executor that cannot publish live output would run commands whose output
// nobody sees until they end, so it does not start at all.
func TestRunFailsWhenValkeyIsUnreachable(t *testing.T) {
	t.Parallel()

	dialErr := errors.New("ping valkey localhost:6379: connection refused")
	deps := newRecordingExecutorDependencies(t)
	deps.liveLogsErr = dialErr

	err := runWithDependencies(context.Background(), executorTestEnv, deps.executorDependencies)
	if !errors.Is(err, dialErr) || !strings.Contains(err.Error(), "live logs") {
		t.Fatalf("error = %v, want dialErr wrapped as live logs", err)
	}
	if slices.Contains(deps.calls, "dialTemporal") {
		t.Fatalf("calls = %v, want no Temporal dial", deps.calls)
	}
}

func TestRunWrapsTemporalDialFailure(t *testing.T) {
	t.Parallel()

	dialErr := errors.New("dial failed")
	deps := newRecordingExecutorDependencies(t)
	deps.dialErr = dialErr

	err := runWithDependencies(context.Background(), executorTestEnv, deps.executorDependencies)
	if !errors.Is(err, dialErr) {
		t.Fatalf("error = %v, want dialErr", err)
	}
	if !strings.Contains(err.Error(), "dial temporal") {
		t.Fatalf("error = %q, want dial temporal", err)
	}
}

func TestRunWrapsWorkerRunFailure(t *testing.T) {
	t.Parallel()

	runErr := errors.New("worker failed")
	deps := newRecordingExecutorDependencies(t)
	deps.worker.runErr = runErr

	err := runWithDependencies(context.Background(), executorTestEnv, deps.executorDependencies)
	if !errors.Is(err, runErr) {
		t.Fatalf("error = %v, want runErr", err)
	}
	if !strings.Contains(err.Error(), "run worker") {
		t.Fatalf("error = %q, want run worker", err)
	}
}

func executorTestEnv(key string) string {
	switch key {
	case "TEMPORAL_ADDRESS":
		return "localhost:7233"
	case "VALKEY_URL":
		return "redis://localhost:6379"
	case "TEMPORAL_NAMESPACE":
		return "openplan"
	case "EXECUTOR_RUN_ROOT":
		return "/tmp/openplan-executor-test"
	case "ARTIFACT_STORE_KIND":
		return "filesystem"
	case "ARTIFACT_STORE_FILESYSTEM_ROOT":
		return "/tmp/openplan-executor-artifacts"
	default:
		return ""
	}
}

type recordingExecutorDependencies struct {
	executorDependencies
	temporalClient      *recordingWorkerTemporalClient
	worker              *recordingTemporalWorker
	temporalConfig      temporal.Config
	workerTaskQueue     string
	workerOptions       temporalworker.Options
	artifactStoreConfig config.ArtifactStoreConfig
	execution           execution
	planStore           recordingWorkerPlanStore
	dialErr             error
	// runner and sandboxErr are what newSandbox returns, and sandboxConfig
	// and sandboxRunRoot what it was given.
	runner         sandbox.Runner
	sandboxErr     error
	sandboxConfig  config.SandboxConfig
	sandboxRunRoot string
	// liveLogs and liveLogsErr are what dialLiveLogs returns, and
	// valkeyURL what it was given.
	liveLogs    *redis.Client
	liveLogsErr error
	valkeyURL   string
	// calls records the startup steps in the order they ran.
	calls []string
}

func newRecordingExecutorDependencies(t *testing.T) *recordingExecutorDependencies {
	t.Helper()

	deps := &recordingExecutorDependencies{
		temporalClient: &recordingWorkerTemporalClient{},
		worker:         &recordingTemporalWorker{},
		// A pointer, so the test can compare it: InProcess holds a map.
		runner: &sandbox.InProcess{},
	}
	server := miniredis.RunT(t)
	deps.liveLogs = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = deps.liveLogs.Close() })
	deps.executorDependencies = executorDependencies{
		dialTemporal: func(_ context.Context, cfg temporal.Config) (client.Client, error) {
			deps.calls = append(deps.calls, "dialTemporal")
			deps.temporalConfig = cfg
			if deps.dialErr != nil {
				return nil, deps.dialErr
			}
			return deps.temporalClient, nil
		},
		newWorker: func(temporalClient client.Client, taskQueue string, options temporalworker.Options) temporalWorker {
			if temporalClient != deps.temporalClient {
				t.Fatalf("newWorker temporalClient = %p, want %p", temporalClient, deps.temporalClient)
			}
			deps.workerTaskQueue = taskQueue
			deps.workerOptions = options
			return deps.worker
		},
		registerActivities: func(worker temporalWorker, exec execution) error {
			if worker != deps.worker {
				t.Fatalf("registerActivities worker = %p, want %p", worker, deps.worker)
			}
			deps.execution = exec
			return nil
		},
		newPlanStore: func(cfg config.ArtifactStoreConfig) (activities.PlanArtifactStore, error) {
			deps.artifactStoreConfig = cfg
			return deps.planStore, nil
		},
		interruptCh: func() <-chan any {
			ch := make(chan any)
			close(ch)
			return ch
		},
		dialLiveLogs: func(_ context.Context, url string) (*redis.Client, error) {
			deps.calls = append(deps.calls, "dialLiveLogs")
			deps.valkeyURL = url
			if deps.liveLogsErr != nil {
				return nil, deps.liveLogsErr
			}
			return deps.liveLogs, nil
		},
		newSandbox: func(cfg config.SandboxConfig, runRoot string) (sandbox.Runner, error) {
			deps.calls = append(deps.calls, "newSandbox")
			deps.sandboxConfig, deps.sandboxRunRoot = cfg, runRoot
			if deps.sandboxErr != nil {
				return nil, deps.sandboxErr
			}
			return deps.runner, nil
		},
	}
	return deps
}

type recordingWorkerPlanStore struct{}

func (recordingWorkerPlanStore) PutPlan(context.Context, domain.TenantID, domain.TemplateRunID, []byte) error {
	return nil
}

func (recordingWorkerPlanStore) GetPlan(context.Context, domain.TenantID, domain.TemplateRunID) ([]byte, error) {
	return nil, nil
}

func (recordingWorkerPlanStore) DeletePlan(context.Context, domain.TenantID, domain.TemplateRunID) error {
	return nil
}

type recordingWorkerTemporalClient struct {
	client.Client
	closed bool
}

func (temporalClient *recordingWorkerTemporalClient) Close() {
	temporalClient.closed = true
}

type recordingTemporalWorker struct {
	registeredActivities map[string]bool
	ran                  bool
	runErr               error
}

func (worker *recordingTemporalWorker) RegisterActivityWithOptions(_ any, options activity.RegisterOptions) {
	if worker.registeredActivities == nil {
		worker.registeredActivities = make(map[string]bool)
	}
	worker.registeredActivities[options.Name] = true
}

func (worker *recordingTemporalWorker) Run(<-chan any) error {
	worker.ran = true
	return worker.runErr
}

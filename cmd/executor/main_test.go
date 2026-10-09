package main

import (
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/runuser"
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
	// Each session holds a pool user; the SDK's default is 1,000 sessions.
	if deps.workerOptions.MaxConcurrentSessionExecutionSize != config.DefaultExecutorMaxSessions {
		t.Fatalf("max concurrent sessions = %d, want %d", deps.workerOptions.MaxConcurrentSessionExecutionSize, config.DefaultExecutorMaxSessions)
	}
	if !reflect.DeepEqual(deps.calls, []string{"newIsolation", "checkIsolation", "dialTemporal"}) {
		t.Fatalf("startup order = %v, want isolation checked before dialing Temporal", deps.calls)
	}
	if !reflect.DeepEqual(deps.activityIsolation, deps.isolation) {
		t.Fatalf("activities got isolation %+v, want the one newIsolation returned", deps.activityIsolation)
	}
	if deps.activityKeys == nil {
		t.Fatal("activities were not given a key ring")
	}
	if deps.activityRunRoot != "/tmp/openplan-executor-test" {
		t.Fatalf("activity run root = %q, want /tmp/openplan-executor-test", deps.activityRunRoot)
	}
	if deps.artifactStoreConfig.Kind != config.ArtifactStoreFilesystem {
		t.Fatalf("artifact store kind = %q, want filesystem", deps.artifactStoreConfig.Kind)
	}
	if deps.artifactStoreConfig.FilesystemRoot != "/tmp/openplan-executor-artifacts" {
		t.Fatalf("artifact store root = %q, want /tmp/openplan-executor-artifacts", deps.artifactStoreConfig.FilesystemRoot)
	}
	if deps.activityStores.logs != deps.logStore {
		t.Fatal("activity log store was not wired")
	}
	if deps.activityStores.plans != deps.planStore {
		t.Fatal("activity plan store was not wired")
	}
	if !deps.worker.ran {
		t.Fatal("worker was not run")
	}
	if !deps.temporalClient.closed {
		t.Fatal("temporal client was not closed")
	}
}

func TestDefaultExecutorDependenciesRegisterOnlyExecutionActivities(t *testing.T) {
	t.Parallel()

	worker := &recordingTemporalWorker{}
	deps := defaultExecutorDependencies()

	isolation := activities.Isolation{Users: runuser.DevelopmentUsers(runuser.PoolSize), Helper: noopWorkspaceHelper{}}
	if err := deps.registerActivities(context.Background(), worker, t.TempDir(), artifactStores{logs: recordingWorkerLogStore{}, plans: recordingWorkerPlanStore{}}, runseal.NewKeyRing(), isolation); err != nil {
		t.Fatalf("registerActivities returned error: %v", err)
	}

	want := map[string]bool{
		domain.PrepareWorkspaceActivityName: true,
		domain.FetchSourceActivityName:      true,
		domain.RunTerraformActivityName:     true,
		domain.ReleaseRunKeyActivityName:    true,
		domain.CleanupWorkspaceActivityName: true,
		domain.UploadPlanActivityName:       true,
		domain.DownloadPlanActivityName:     true,
	}
	if !reflect.DeepEqual(worker.registeredActivities, want) {
		t.Fatalf("registered activities = %v, want %v", worker.registeredActivities, want)
	}
}

// An executor that cannot keep branches apart must not run template code: it
// stops before it ever polls for work.
func TestRunStopsWhenIsolationFails(t *testing.T) {
	t.Parallel()

	checkErr := errors.New("CapEff is wrong")
	deps := newRecordingExecutorDependencies(t)
	deps.checkErr = checkErr

	err := runWithDependencies(context.Background(), executorTestEnv, deps.executorDependencies)
	if !errors.Is(err, checkErr) || !strings.Contains(err.Error(), "subprocess isolation") {
		t.Fatalf("error = %v, want checkErr wrapped as subprocess isolation", err)
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
	activityRunRoot     string
	activityStores      artifactStores
	activityKeys        *runseal.KeyRing
	activityIsolation   activities.Isolation
	logStore            recordingWorkerLogStore
	planStore           recordingWorkerPlanStore
	dialErr             error
	// isolation is what newIsolation returns; checkErr is what checkIsolation
	// returns.
	isolation activities.Isolation
	checkErr  error
	// calls records the startup steps in the order they ran.
	calls []string
}

func newRecordingExecutorDependencies(t *testing.T) *recordingExecutorDependencies {
	t.Helper()

	deps := &recordingExecutorDependencies{
		temporalClient: &recordingWorkerTemporalClient{},
		worker:         &recordingTemporalWorker{},
		isolation:      activities.Isolation{Users: runuser.DevelopmentUsers(runuser.PoolSize)},
	}
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
		registerActivities: func(_ context.Context, worker temporalWorker, runRoot string, stores artifactStores, keys *runseal.KeyRing, isolation activities.Isolation) error {
			if worker != deps.worker {
				t.Fatalf("registerActivities worker = %p, want %p", worker, deps.worker)
			}
			deps.activityRunRoot = runRoot
			deps.activityStores = stores
			deps.activityKeys = keys
			deps.activityIsolation = isolation
			return nil
		},
		newArtifactStores: func(cfg config.ArtifactStoreConfig) (artifactStores, error) {
			deps.artifactStoreConfig = cfg
			return artifactStores{logs: deps.logStore, plans: deps.planStore}, nil
		},
		interruptCh: func() <-chan any {
			ch := make(chan any)
			close(ch)
			return ch
		},
		newIsolation: func() (activities.Isolation, error) {
			deps.calls = append(deps.calls, "newIsolation")
			return deps.isolation, nil
		},
		checkIsolation: func(config.ExecutorConfig, []runuser.User) error {
			deps.calls = append(deps.calls, "checkIsolation")
			return deps.checkErr
		},
	}
	return deps
}

// noopWorkspaceHelper stands in for the helper where nothing runs in a
// workspace: registration only sweeps an empty run root.
type noopWorkspaceHelper struct{}

func (noopWorkspaceHelper) Kill(context.Context, runuser.User) error { return nil }

func (noopWorkspaceHelper) Reclaim(context.Context, runuser.User, string) error { return nil }

func (noopWorkspaceHelper) Pack(context.Context, runuser.User, string) ([]byte, error) {
	return nil, nil
}

func (noopWorkspaceHelper) Unpack(context.Context, runuser.User, string, []byte) error { return nil }

func (noopWorkspaceHelper) CheckRoot(context.Context, runuser.User, string) error { return nil }

type recordingWorkerLogStore struct{}

func (recordingWorkerLogStore) PutTemplateRunLog(context.Context, domain.TenantID, domain.TemplateRunID, string, io.Reader) (domain.TemplateRunLog, error) {
	return domain.TemplateRunLog{}, nil
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

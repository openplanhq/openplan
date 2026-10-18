package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/redis/go-redis/v9"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/artifacts"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/logsink"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/sandbox"
	"github.com/vishu42/openplan/internal/temporal"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"
)

// cgroupFS is the container's cgroup2 mount, and guestRoot the read-only root
// every sandbox boots from (Dockerfile.executor).
const (
	cgroupFS  = "/sys/fs/cgroup"
	guestRoot = "/opt/openplan/guest"
)

type temporalWorker interface {
	RegisterActivityWithOptions(any, activity.RegisterOptions)
	Run(<-chan any) error
}

// execution is what the execution activities are built from.
type execution struct {
	plans      activities.PlanArtifactStore
	keys       *runseal.KeyRing
	runner     sandbox.Runner
	workspaces sandbox.Workspaces
	store      config.ArtifactStoreConfig
	// live is where each command's output is published as it runs.
	live logsink.Stream
}

type executorDependencies struct {
	// dialLiveLogs connects to the Valkey that commands' output is published
	// to as they run.
	dialLiveLogs func(ctx context.Context, url string) (*redis.Client, error)
	// dialTemporal connects to the Temporal namespace where the worker polls for tasks.
	dialTemporal func(context.Context, temporal.Config) (client.Client, error)
	// newWorker creates the Temporal worker bound to the execution task queue.
	newWorker func(client.Client, string, temporalworker.Options) temporalWorker
	// registerActivities attaches the execution activities to the worker.
	registerActivities func(worker temporalWorker, exec execution) error
	// newPlanStore builds the store CleanupWorkspace deletes applied plans
	// from.
	newPlanStore func(config.ArtifactStoreConfig) (activities.PlanArtifactStore, error)
	// interruptCh provides the shutdown signal consumed by the Temporal worker run loop.
	interruptCh func() <-chan any
	// newSandbox readies the executor to start sandboxes, and fails when it
	// can't: there is no way to run an activity outside one.
	newSandbox func(cfg config.SandboxConfig, runRoot string) (sandbox.Runner, error)
}

func main() {
	// A sandbox's process: this binary, run inside gVisor as one activity's
	// body. It is not the executor and must not run its startup.
	if len(os.Args) > 1 && os.Args[1] == sandbox.ActivitySubcommand {
		os.Exit(sandbox.Serve(context.Background(), activities.NewBodies().Handlers(), os.Args[2:], os.Stdin, os.Stdout))
	}
	if err := run(context.Background(), os.Getenv); err != nil {
		log.Fatal(err)
	}
}

func defaultExecutorDependencies() executorDependencies {
	return executorDependencies{
		dialLiveLogs: logsink.Dial,
		dialTemporal: temporal.Dial,
		newWorker: func(temporalClient client.Client, taskQueue string, options temporalworker.Options) temporalWorker {
			return temporalworker.New(temporalClient, taskQueue, options)
		},
		registerActivities: registerActivities,
		newPlanStore: func(cfg config.ArtifactStoreConfig) (activities.PlanArtifactStore, error) {
			store, err := artifacts.NewObjectStore(cfg)
			if err != nil {
				return nil, err
			}
			return artifacts.NewPlanStore(store), nil
		},
		interruptCh: temporalworker.InterruptCh,
		newSandbox: func(cfg config.SandboxConfig, runRoot string) (sandbox.Runner, error) {
			if err := (sandbox.Workspaces{Root: runRoot}).Check(); err != nil {
				return nil, err
			}
			cgroups, err := sandbox.DelegateCgroups(cgroupFS)
			if err != nil {
				return nil, err
			}
			network, err := sandbox.SetupNetwork(cfg.State, cfg.Subnet)
			if err != nil {
				return nil, err
			}
			return sandbox.NewRuntime(cfg.Runsc, cfg.State, guestRoot, cgroupFS, cgroups, sandbox.Limits{
				MemoryMiB: cfg.MemoryMiB, CPUs: cfg.CPUs, Pids: cfg.Pids,
			}, network)
		},
	}
}

func registerActivities(worker temporalWorker, exec execution) error {
	templateRun := activities.NewTemplateRunActivities(exec.runner, exec.workspaces, exec.store, exec.plans, exec.keys).WithLiveLogs(exec.live)
	for name, fn := range map[string]any{
		domain.PrepareWorkspaceActivityName: templateRun.PrepareWorkspace,
		domain.FetchSourceActivityName:      templateRun.FetchSource,
		domain.RunTerraformActivityName:     templateRun.RunTerraform,
		domain.UploadLogActivityName:        templateRun.UploadLog,
		domain.ReleaseRunKeyActivityName:    templateRun.ReleaseRunKey,
		domain.CleanupWorkspaceActivityName: templateRun.CleanupWorkspace,
		domain.UploadPlanActivityName:       templateRun.UploadPlan,
		domain.DownloadPlanActivityName:     templateRun.DownloadPlan,
	} {
		worker.RegisterActivityWithOptions(fn, activity.RegisterOptions{Name: name})
	}
	return nil
}

func run(ctx context.Context, getenv func(string) string) error {
	return runWithDependencies(ctx, getenv, defaultExecutorDependencies())
}

// runWithDependencies runs the executor: a Temporal worker on the execution
// queue that runs each activity's body in a gVisor sandbox. It opens no
// database connection and parses no key; what a run needs arrives sealed to a
// key this process makes for it.
func runWithDependencies(ctx context.Context, getenv func(string) string, deps executorDependencies) error {
	cfg, err := config.LoadExecutorConfig(getenv)
	if err != nil {
		return fmt.Errorf("load executor config: %w", err)
	}
	// Before Temporal: an executor that cannot sandbox must never take work.
	runner, err := deps.newSandbox(cfg.Sandbox, cfg.RunRoot)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	plans, err := deps.newPlanStore(cfg.ArtifactStore)
	if err != nil {
		return fmt.Errorf("wire the plan store: %w", err)
	}
	// Before Temporal too: an executor whose output nobody can watch should
	// fail at start, not take work and leave its runs' logs dark.
	liveLogs, err := deps.dialLiveLogs(ctx, cfg.LiveLogURL)
	if err != nil {
		return fmt.Errorf("live logs: %w", err)
	}
	defer liveLogs.Close()
	temporalClient, err := deps.dialTemporal(ctx, temporal.Config{Address: cfg.TemporalAddress, Namespace: cfg.TemporalNamespace})
	if err != nil {
		return fmt.Errorf("dial temporal: %w", err)
	}
	defer temporalClient.Close()

	// Sessions pin a run's activities, its workspace and its sealing key to
	// this process.
	worker := deps.newWorker(temporalClient, domain.ExecutionTaskQueue, temporalworker.Options{
		EnableSessionWorker:               true,
		MaxConcurrentSessionExecutionSize: cfg.MaxSessions,
	})
	if err := deps.registerActivities(worker, execution{
		plans: plans, keys: runseal.NewKeyRing(), runner: runner,
		workspaces: sandbox.Workspaces{Root: cfg.RunRoot}, store: cfg.ArtifactStore, live: liveLogs,
	}); err != nil {
		return err
	}
	if err := worker.Run(deps.interruptCh()); err != nil {
		return fmt.Errorf("run worker: %w", err)
	}
	return nil
}

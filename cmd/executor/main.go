package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/vishu42/openplan/internal/activities"
	"github.com/vishu42/openplan/internal/artifacts"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/runhelper"
	"github.com/vishu42/openplan/internal/runseal"
	"github.com/vishu42/openplan/internal/runuser"
	"github.com/vishu42/openplan/internal/temporal"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"
)

type temporalWorker interface {
	RegisterActivityWithOptions(any, activity.RegisterOptions)
	Run(<-chan any) error
}

type executorDependencies struct {
	// dialTemporal connects to the Temporal namespace where the worker polls for tasks.
	dialTemporal func(context.Context, temporal.Config) (client.Client, error)
	// newWorker creates the Temporal worker bound to the execution task queue.
	newWorker func(client.Client, string, temporalworker.Options) temporalWorker
	// registerActivities sweeps what an earlier process left, then attaches the
	// execution activities to the worker.
	registerActivities func(ctx context.Context, worker temporalWorker, runRoot string, stores artifactStores, keys *runseal.KeyRing, isolation activities.Isolation) error
	// newArtifactStores builds the artifact-backed stores for phase logs and
	// saved plans.
	newArtifactStores func(config.ArtifactStoreConfig) (artifactStores, error)
	// interruptCh provides the shutdown signal consumed by the Temporal worker run loop.
	interruptCh func() <-chan any
	// newIsolation returns the pool users and the helper that keep branches apart.
	newIsolation func() (activities.Isolation, error)
	// checkIsolation fails closed when the process cannot keep branches apart.
	checkIsolation func(config.ExecutorConfig, []runuser.User) error
}

// artifactStores are the executor's two uses of the artifact store: the log of
// each Terraform phase, and each run's saved plan.
type artifactStores struct {
	logs  activities.TemplateRunLogStore
	plans activities.PlanArtifactStore
}

func main() {
	// The helper: this binary re-run as a session's pool user, to work inside
	// its workspace. It is not the executor and must not run its startup.
	if len(os.Args) > 1 && os.Args[1] == runhelper.Subcommand {
		os.Exit(runhelper.Main(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
	}
	// First, before config is read or any subprocess exists. Fail closed: an
	// executor that cannot hide its own process must not run template code.
	if err := setNotDumpable(); err != nil {
		log.Fatalf("set not dumpable: %v", err)
	}
	if err := run(context.Background(), os.Getenv); err != nil {
		log.Fatal(err)
	}
}

func defaultExecutorDependencies() executorDependencies {
	return executorDependencies{
		dialTemporal: temporal.Dial,
		newWorker: func(temporalClient client.Client, taskQueue string, options temporalworker.Options) temporalWorker {
			return temporalworker.New(temporalClient, taskQueue, options)
		},
		registerActivities: func(ctx context.Context, worker temporalWorker, runRoot string, stores artifactStores, keys *runseal.KeyRing, isolation activities.Isolation) error {
			templateRunActivities := activities.NewTemplateRunActivities(runRoot, stores.logs, stores.plans, keys, isolation)
			// Before the worker takes sessions: what an earlier process left
			// behind holds no valid lease.
			if err := templateRunActivities.Sweep(ctx); err != nil {
				return fmt.Errorf("sweep run directories: %w", err)
			}
			worker.RegisterActivityWithOptions(templateRunActivities.PrepareWorkspace, activity.RegisterOptions{
				Name: domain.PrepareWorkspaceActivityName,
			})
			worker.RegisterActivityWithOptions(templateRunActivities.FetchSource, activity.RegisterOptions{
				Name: domain.FetchSourceActivityName,
			})
			worker.RegisterActivityWithOptions(templateRunActivities.RunTerraform, activity.RegisterOptions{
				Name: domain.RunTerraformActivityName,
			})
			worker.RegisterActivityWithOptions(templateRunActivities.ReleaseRunKey, activity.RegisterOptions{
				Name: domain.ReleaseRunKeyActivityName,
			})
			worker.RegisterActivityWithOptions(templateRunActivities.CleanupWorkspace, activity.RegisterOptions{
				Name: domain.CleanupWorkspaceActivityName,
			})
			worker.RegisterActivityWithOptions(templateRunActivities.UploadPlan, activity.RegisterOptions{
				Name: domain.UploadPlanActivityName,
			})
			worker.RegisterActivityWithOptions(templateRunActivities.DownloadPlan, activity.RegisterOptions{
				Name: domain.DownloadPlanActivityName,
			})
			return nil
		},
		newArtifactStores: func(cfg config.ArtifactStoreConfig) (artifactStores, error) {
			store, err := artifacts.NewObjectStore(cfg)
			if err != nil {
				return artifactStores{}, err
			}
			return artifactStores{logs: artifacts.NewLogStore(store), plans: artifacts.NewPlanStore(store)}, nil
		},
		interruptCh:    temporalworker.InterruptCh,
		newIsolation:   newIsolation,
		checkIsolation: checkIsolation,
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	return runWithDependencies(ctx, getenv, defaultExecutorDependencies())
}

// runWithDependencies runs the executor: a Temporal worker on the execution
// queue, next to tenant Terraform. It deliberately opens no database connection
// and parses no key. Everything secret a run needs arrives sealed to a key this
// process generates for that run, and everything a run produces goes back
// through Temporal to the control plane.
func runWithDependencies(ctx context.Context, getenv func(string) string, deps executorDependencies) error {
	cfg, err := config.LoadExecutorConfig(getenv)
	if err != nil {
		return fmt.Errorf("load executor config: %w", err)
	}

	isolation, err := deps.newIsolation()
	if err != nil {
		return fmt.Errorf("set up isolation: %w", err)
	}
	if err := deps.checkIsolation(cfg, isolation.Users); err != nil {
		return fmt.Errorf("subprocess isolation: %w", err)
	}

	stores, err := deps.newArtifactStores(cfg.ArtifactStore)
	if err != nil {
		return fmt.Errorf("wire artifact stores: %w", err)
	}

	temporalClient, err := deps.dialTemporal(ctx, temporal.Config{
		Address:   cfg.TemporalAddress,
		Namespace: cfg.TemporalNamespace,
	})
	if err != nil {
		return fmt.Errorf("dial temporal: %w", err)
	}
	defer temporalClient.Close()

	// Sessions pin a run's workspace activities, and so its sealing key, to this
	// process.
	worker := deps.newWorker(temporalClient, domain.ExecutionTaskQueue, temporalworker.Options{
		EnableSessionWorker: true,
		// Each session holds a pool user (#331).
		MaxConcurrentSessionExecutionSize: cfg.MaxSessions,
	})
	if err := deps.registerActivities(ctx, worker, cfg.RunRoot, stores, runseal.NewKeyRing(), isolation); err != nil {
		return err
	}
	if err := worker.Run(deps.interruptCh()); err != nil {
		return fmt.Errorf("run worker: %w", err)
	}

	return nil
}

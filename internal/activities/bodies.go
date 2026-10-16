package activities

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/vishu42/openplan/internal/artifacts"
	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/planbundle"
	"github.com/vishu42/openplan/internal/runner"
	"github.com/vishu42/openplan/internal/sandbox"
)

// The bodies a sandbox runs: openplan-executor activity <name>.
const (
	bodyFetchSource  = "fetch-source"
	bodyRunTerraform = "run-terraform"
	bodyUploadLog    = "upload-log"
	bodyUploadPlan   = "upload-plan"
	bodyDownloadPlan = "download-plan"
)

type fetchSourceRequest struct {
	RepoURL       string
	CommitSHA     string
	Ref           string
	SourcePath    string
	TerraformPath string
	// Token is scoped to this repository, read-only: what it reaches, the
	// sandbox's tofu gets anyway.
	Token string
}

// runTerraformRequest carries the run's cloud credentials and nothing else
// secret: the sandbox it goes to runs tenant code.
type runTerraformRequest struct {
	TerraformPath string
	WorkspaceName string
	Command       domain.TerraformCommandType
	// omitempty: a nil config would otherwise cross as null, which the runner
	// refuses as not an object, where nil means no variables.
	ConfigJSON  json.RawMessage `json:",omitempty"`
	Environment map[string]string
}

type runTerraformResult struct {
	HasChanges bool
	Summary    domain.PlanSummary
}

// uploadLogRequest carries storage access: its sandbox runs only our code.
type uploadLogRequest struct {
	Store    config.ArtifactStoreConfig
	TenantID domain.TenantID
	RunID    domain.TemplateRunID
	Phase    string
	LogsDir  string
	FileName string
}

// planRequest carries storage access and the plan key: its sandbox runs only
// our code.
type planRequest struct {
	Store         config.ArtifactStoreConfig
	TenantID      domain.TenantID
	RunID         domain.TemplateRunID
	TerraformPath string
	PlanKey       []byte
}

// Bodies are the activities' bodies, the part that runs inside a sandbox.
type Bodies struct {
	terraform *runner.LocalTerraformRunner
	git       *runner.LocalGitRunner
	// logs and plans, when set, stand in for the stores a request names.
	// Tests set them; in a sandbox they stay nil.
	logs  TemplateRunLogStore
	plans PlanArtifactStore
}

// NewBodies returns the bodies as a sandbox runs them: git and tofu as plain
// children, the stores the requests name.
func NewBodies() *Bodies {
	executor := runner.ProcessExecutor{}
	return &Bodies{
		terraform: runner.NewLocalTerraformRunnerWithExecutor(executor),
		git:       runner.NewLocalGitRunnerWithExecutor(executor),
	}
}

// Handlers maps each body's name to it.
func (b *Bodies) Handlers() map[string]sandbox.Handler {
	return map[string]sandbox.Handler{
		bodyFetchSource:  handle(b.fetchSource),
		bodyRunTerraform: handle(b.runTerraform),
		bodyUploadLog:    handle(b.uploadLog),
		bodyUploadPlan:   handle(b.uploadPlan),
		bodyDownloadPlan: handle(b.downloadPlan),
	}
}

// handle adapts a typed body to a sandbox.Handler.
func handle[Request any, Result any](body func(context.Context, Request, io.Writer) (Result, error)) sandbox.Handler {
	return func(ctx context.Context, raw json.RawMessage, output io.Writer) (any, error) {
		var request Request
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, fmt.Errorf("decode the request: %w", err)
		}
		return body(ctx, request, output)
	}
}

func (b *Bodies) fetchSource(ctx context.Context, request fetchSourceRequest, _ io.Writer) (struct{}, error) {
	var credential runner.GitCredential
	if request.Token != "" {
		credential = runner.NewGitCredential(request.Token)
	}
	if request.CommitSHA != "" {
		if err := b.git.CheckoutCommit(ctx, request.RepoURL, request.CommitSHA, request.SourcePath, credential); err != nil {
			return struct{}{}, fmt.Errorf("checkout source commit %s: %w", request.CommitSHA, err)
		}
	} else if err := b.git.Clone(ctx, request.RepoURL, request.Ref, request.SourcePath, credential); err != nil {
		return struct{}{}, fmt.Errorf("clone source: %w", err)
	}
	if err := sandbox.CheckDir(request.TerraformPath); err != nil {
		return struct{}{}, fmt.Errorf("source root: %w", err)
	}
	return struct{}{}, nil
}

func (b *Bodies) runTerraform(ctx context.Context, request runTerraformRequest, output io.Writer) (runTerraformResult, error) {
	result, err := b.terraform.Run(ctx, runner.TerraformCommand{
		WorkspacePath: request.TerraformPath,
		WorkspaceName: request.WorkspaceName,
		Command:       request.Command,
		ConfigJSON:    request.ConfigJSON,
		Environment:   request.Environment,
		Stdout:        output,
		Stderr:        output,
	})
	return runTerraformResult{HasChanges: result.HasChanges, Summary: result.Summary}, err
}

func (b *Bodies) uploadLog(ctx context.Context, request uploadLogRequest, _ io.Writer) (domain.TemplateRunLog, error) {
	logs, _, err := b.stores(request.Store)
	if err != nil {
		return domain.TemplateRunLog{}, err
	}
	files, err := sandbox.ReadFiles(request.LogsDir, []string{request.FileName})
	if err != nil {
		return domain.TemplateRunLog{}, err
	}
	content, ok := files[request.FileName]
	if !ok {
		return domain.TemplateRunLog{}, fmt.Errorf("no log %s", request.FileName)
	}
	return logs.PutTemplateRunLog(ctx, request.TenantID, request.RunID, request.Phase, bytes.NewReader(content))
}

func (b *Bodies) uploadPlan(ctx context.Context, request planRequest, _ io.Writer) (struct{}, error) {
	_, plans, err := b.stores(request.Store)
	if err != nil {
		return struct{}{}, err
	}
	files, err := sandbox.ReadFiles(request.TerraformPath, planbundle.Files)
	if err != nil {
		return struct{}{}, fmt.Errorf("read saved plan: %w", err)
	}
	bundle, err := planbundle.Pack(files)
	if err != nil {
		return struct{}{}, fmt.Errorf("pack saved plan: %w", err)
	}
	sealed, err := planbundle.Seal(request.PlanKey, bundle, domain.RunKeyID(request.TenantID, request.RunID))
	if err != nil {
		return struct{}{}, fmt.Errorf("seal saved plan: %w", err)
	}
	return struct{}{}, plans.PutPlan(ctx, request.TenantID, request.RunID, sealed)
}

func (b *Bodies) downloadPlan(ctx context.Context, request planRequest, _ io.Writer) (struct{}, error) {
	_, plans, err := b.stores(request.Store)
	if err != nil {
		return struct{}{}, err
	}
	sealed, err := plans.GetPlan(ctx, request.TenantID, request.RunID)
	if err != nil {
		return struct{}{}, err
	}
	bundle, err := planbundle.Open(request.PlanKey, sealed, domain.RunKeyID(request.TenantID, request.RunID))
	if err != nil {
		return struct{}{}, err
	}
	files, err := planbundle.Unpack(bundle)
	if err != nil {
		return struct{}{}, err
	}
	if err := sandbox.WriteFiles(request.TerraformPath, files); err != nil {
		return struct{}{}, fmt.Errorf("write saved plan: %w", err)
	}
	return struct{}{}, nil
}

// stores opens the stores cfg names, unless a test set its own.
func (b *Bodies) stores(cfg config.ArtifactStoreConfig) (TemplateRunLogStore, PlanArtifactStore, error) {
	if b.logs != nil || b.plans != nil {
		return b.logs, b.plans, nil
	}
	store, err := artifacts.NewObjectStore(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("open the artifact store: %w", err)
	}
	return artifacts.NewLogStore(store), artifacts.NewPlanStore(store), nil
}

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vishu42/openplan/internal/domain"
	"github.com/vishu42/openplan/internal/planchanges"
)

type TerraformCommand struct {
	WorkspacePath string
	WorkspaceName string
	Command       domain.TerraformCommandType
	ConfigJSON    json.RawMessage
	// Environment contains resolved provider credentials for this subprocess only.
	Environment map[string]string
	Stdout      io.Writer
	Stderr      io.Writer
}

// LocalTerraformRunner runs OpenTofu as a subprocess via the shared
// CommandExecutor boundary.
type LocalTerraformRunner struct {
	executor CommandExecutor
}

const terraformExecutable = domain.TerraformExecutable

// NewLocalTerraformRunnerWithExecutor returns a runner backed by executor.
//
// Commands run in the requested workspace directory, inherit the process
// context for cancellation, and write to the command's configured output
// writers or to the process stdout/stderr when no writers are configured.
//
// Tests and alternate adapters can provide a custom executor to observe command
// construction or to avoid spawning real OpenTofu subprocesses.
func NewLocalTerraformRunnerWithExecutor(executor CommandExecutor) *LocalTerraformRunner {
	return &LocalTerraformRunner{executor: executor}
}

// PlanFileName is the saved plan inside the Terraform root module directory.
// The plan phase writes it, the executor uploads it, and the apply phase puts
// it back at the same path before applying it.
const PlanFileName = "tfplan"

// Result reports what a command found out. A plan and every apply fill it in.
type Result struct {
	// HasChanges is whether the plan would change anything, read from
	// `-detailed-exitcode`: 0 means no changes, 2 means changes. For an
	// apply, whether it changed anything.
	HasChanges bool
	// Summary counts the changes: a plan's from `tofu show -json` on the saved
	// plan, an apply's from its closing "Apply complete!" or "Destroy
	// complete!" line. Zero when there are none.
	Summary domain.PlanSummary
	// Changes lists a plan's resource changes, read from the same JSON as
	// Summary. Nil for an apply, a plan with no changes, or a plan whose
	// changes could not be read: listing them is for the reviewer's eyes,
	// so it never fails the plan.
	Changes *domain.PlanChanges
}

// Run validates input and executes the requested Terraform-compatible command.
//
// The runner owns the concrete OpenTofu CLI arguments for each supported
// command type. Workspace selection creates a missing workspace with
// `tofu workspace select -or-create`.
//
// A plan saves itself to PlanFileName. Apply and destroy both apply that saved
// plan and nothing else: what was approved is exactly what runs, the variables
// come from inside the plan, and tofu refuses the file outright if the state
// has moved since it was written. An auto-approved apply has no saved plan, so
// it plans and applies in one command, with the run's variables. Every apply
// reports what it did, counted from its closing line.
func (runner *LocalTerraformRunner) Run(ctx context.Context, input TerraformCommand) (Result, error) {
	if strings.TrimSpace(input.WorkspacePath) == "" {
		return Result{}, fmt.Errorf("workspace path is required")
	}
	if strings.TrimSpace(input.WorkspaceName) == "" {
		return Result{}, fmt.Errorf("workspace name is required")
	}

	switch input.Command {
	case domain.TerraformCommandInit:
		return Result{}, runner.run(ctx, input, sortedEnvironment(input.Environment), "init", "-input=false", "-no-color")
	case domain.TerraformCommandSelectWorkspace:
		return Result{}, runner.selectWorkspace(ctx, input)
	case domain.TerraformCommandPlan, domain.TerraformCommandPlanDestroy:
		return runner.plan(ctx, input)
	case domain.TerraformCommandApply, domain.TerraformCommandDestroy:
		return runner.apply(input, func(input TerraformCommand) error {
			return runner.run(ctx, input, sortedEnvironment(input.Environment), "apply", "-input=false", "-auto-approve", "-no-color", PlanFileName)
		})
	case domain.TerraformCommandApplyAutoApprove:
		return runner.apply(input, func(input TerraformCommand) error {
			return runner.runWithTerraformVariables(ctx, input, "apply", "-input=false", "-auto-approve", "-no-color")
		})
	default:
		return Result{}, fmt.Errorf("unsupported terraform command %q", input.Command)
	}
}

// plan runs `tofu plan` into PlanFileName and, when it has changes, counts
// them. With -detailed-exitcode tofu exits 2 for a successful plan that has
// changes, so exit 2 is success here, not failure.
func (runner *LocalTerraformRunner) plan(ctx context.Context, input TerraformCommand) (Result, error) {
	args := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out=" + PlanFileName}
	if input.Command == domain.TerraformCommandPlanDestroy {
		args = append(args, "-destroy")
	}
	err := runner.runWithTerraformVariables(ctx, input, args...)
	if err == nil {
		return Result{}, nil
	}
	var exitErr interface{ ExitCode() int }
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		return Result{}, err
	}

	summary, changes, err := runner.summarize(ctx, input)
	if err != nil {
		return Result{}, err
	}
	return Result{HasChanges: true, Summary: summary, Changes: changes}, nil
}

// apply runs an apply and counts what it did from its closing line. The
// output still goes to the command's writers; the counts are read from a copy.
func (runner *LocalTerraformRunner) apply(input TerraformCommand, run func(TerraformCommand) error) (Result, error) {
	var output bytes.Buffer
	stdout, _ := outputWriters(input)
	input.Stdout = io.MultiWriter(stdout, &output)
	if err := run(input); err != nil {
		return Result{}, err
	}
	summary := summarizeApplyOutput(output.String())
	return Result{HasChanges: summary != domain.PlanSummary{}, Summary: summary}, nil
}

// applyCompleteLine is tofu's closing line for a successful apply, such as
// "Apply complete! Resources: 1 imported, 2 added, 0 changed, 1 destroyed.",
// or "Destroy complete! Resources: 3 destroyed." when it prints that one for
// a destroy.
var applyCompleteLine = regexp.MustCompile(`(?:Apply|Destroy) complete! Resources: ([^\n]*)`)

// applyCount is one "N verb" count on that line.
var applyCount = regexp.MustCompile(`(\d+) (added|changed|destroyed)`)

// summarizeApplyOutput counts an apply's changes from its "Apply complete!"
// line. Counts it does not track, such as imported, are ignored, and output
// without the line counts as no changes.
func summarizeApplyOutput(output string) domain.PlanSummary {
	var summary domain.PlanSummary
	line := applyCompleteLine.FindStringSubmatch(output)
	if line == nil {
		return summary
	}
	for _, count := range applyCount.FindAllStringSubmatch(line[1], -1) {
		n, err := strconv.Atoi(count[1])
		if err != nil {
			continue
		}
		switch count[2] {
		case "added":
			summary.Add = n
		case "changed":
			summary.Change = n
		case "destroyed":
			summary.Destroy = n
		}
	}
	return summary
}

// summarize reads the saved plan back as JSON, counts its resource changes
// and lists them. The JSON holds variable values and sensitive attributes in
// plaintext, so it is parsed here and only the counts and the masked list
// leave the executor; it is never written to the log.
//
// The counts are the plan's: failing to read them fails the plan. The list is
// the reviewer's: failing to read it, such as a plan format version this
// openplan doesn't know, leaves it nil and the plan stands.
func (runner *LocalTerraformRunner) summarize(ctx context.Context, input TerraformCommand) (domain.PlanSummary, *domain.PlanChanges, error) {
	var stdout, stderr bytes.Buffer
	if err := runner.executor.Run(ctx, input.WorkspacePath, sortedEnvironment(input.Environment), &stdout, &stderr, terraformExecutable, "show", "-json", "-no-color", PlanFileName); err != nil {
		return domain.PlanSummary{}, nil, &CommandError{Command: input.Command, Err: fmt.Errorf("show saved plan: %w: %s", err, strings.TrimSpace(stderr.String()))}
	}
	summary, changes, err := planchanges.Read(stdout.Bytes())
	if err != nil {
		return domain.PlanSummary{}, nil, &CommandError{Command: input.Command, Err: err}
	}
	return summary, changes, nil
}

// selectWorkspace selects the requested workspace, creating it on the stack's
// first run. It is one command: a select that fails and falls back to new
// leaves an expected "doesn't exist" error first in the log, which a failed
// run would then lead with instead of the error that stopped it (issue 522).
func (runner *LocalTerraformRunner) selectWorkspace(ctx context.Context, input TerraformCommand) error {
	return runner.run(ctx, input, sortedEnvironment(input.Environment),
		"workspace", "select", "-or-create=true", "-no-color", input.WorkspaceName)
}

// run executes an OpenTofu command with shared error wrapping.
//
// A failure is wrapped in CommandError (using input.Command) so callers can
// identify which command failed via errors.As instead of parsing the error
// text. args are the exact OpenTofu CLI arguments passed to the command
// executor.
func (runner *LocalTerraformRunner) run(ctx context.Context, input TerraformCommand, env []string, args ...string) error {
	stdout, stderr := outputWriters(input)
	err := runner.executor.Run(ctx, input.WorkspacePath, env, stdout, stderr, terraformExecutable, args...)
	if err != nil {
		return &CommandError{Command: input.Command, Err: err}
	}
	return nil
}

// runWithTerraformVariables runs a command that plans, handing it the run's
// variables as a -var-file rather than TF_VAR_ entries. tofu reads a TF_VAR of
// a non-primitive type as HCL, so `${` in a string fails, and a template's own
// *.auto.tfvars silently outranks it; a JSON var file takes strings
// literally and outranks every file the template ships.
func (runner *LocalTerraformRunner) runWithTerraformVariables(ctx context.Context, input TerraformCommand, args ...string) error {
	path, err := writeVariableFile(input.ConfigJSON)
	if err != nil {
		return &CommandError{Command: input.Command, Err: fmt.Errorf("variables: %w", err)}
	}
	if path != "" {
		// Removed however the command ends. Inside a sandbox the file would
		// go with its /tmp anyway; on any other host it would not.
		defer func() { _ = os.Remove(path) }()
		args = append(args, "-var-file="+path)
	}
	return runner.run(ctx, input, sortedEnvironment(input.Environment), args...)
}

// writeVariableFile writes the run's config, byte for byte as stored, to a new
// .tfvars.json file and returns its path, or "" when there are no variables.
//
// The file goes in TMPDIR, which in a sandbox is the sandbox's own tmpfs,
// outside the template tree. os.CreateTemp picks a random name, opens it with
// O_EXCL and mode 0600, so nothing a hostile template planted ahead of time,
// such as a symlink, can redirect the write.
func writeVariableFile(config json.RawMessage) (string, error) {
	if len(config) == 0 {
		return "", nil
	}
	var variables map[string]json.RawMessage
	if err := json.Unmarshal(config, &variables); err != nil {
		return "", fmt.Errorf("decode config JSON: %w", err)
	}
	if variables == nil {
		return "", fmt.Errorf("config must be a JSON object")
	}
	if len(variables) == 0 {
		return "", nil
	}

	file, err := os.CreateTemp("", "openplan-*.tfvars.json")
	if err != nil {
		return "", fmt.Errorf("create var file: %w", err)
	}
	_, writeErr := file.Write(config)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		_ = os.Remove(file.Name())
		return "", fmt.Errorf("write var file: %w", err)
	}
	return file.Name(), nil
}

// sortedEnvironment converts credential variables into deterministic NAME=value process entries.
func sortedEnvironment(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

// outputWriters returns the command-specific output writers with process defaults.
//
// Callers can pass writers to capture OpenTofu output in logs. When either
// writer is nil, the runner preserves the CLI-like default of writing to the
// current process stdout or stderr.
func outputWriters(input TerraformCommand) (io.Writer, io.Writer) {
	stdout := input.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := input.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	return stdout, stderr
}

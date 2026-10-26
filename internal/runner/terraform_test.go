package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
)

func TestLocalTerraformRunnerRunsTerraformPlan(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := []recordedCommand{
		{
			dir:  "/tmp/openplan/runs/tenant_123/run_123",
			name: "tofu",
			args: []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out=tfplan"},
		},
	}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
}

// TestLocalTerraformRunnerInjectsCredentialEnvironment verifies resolved credentials reach the subprocess.
func TestLocalTerraformRunnerInjectsCredentialEnvironment(t *testing.T) {
	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
		Environment:   map[string]string{"AWS_ACCESS_KEY_ID": "key", "AWS_SECRET_ACCESS_KEY": "secret"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !reflect.DeepEqual(executor.commands[0].env, []string{"AWS_ACCESS_KEY_ID=key", "AWS_SECRET_ACCESS_KEY=secret"}) {
		t.Fatalf("environment = %#v", executor.commands[0].env)
	}
}

// TestSortedEnvironmentReturnsSortedNameValueEntries verifies that environment
// variables are formatted for a subprocess in deterministic key order.
func TestSortedEnvironmentReturnsSortedNameValueEntries(t *testing.T) {
	got := sortedEnvironment(map[string]string{
		"ZED_TOKEN":  "token",
		"AWS_REGION": "us-east-1",
		"A_KEY":      "key",
	})

	want := []string{
		"AWS_REGION=us-east-1",
		"A_KEY=key",
		"ZED_TOKEN=token",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedEnvironment() = %#v, want %#v", got, want)
	}
}

func TestLocalTerraformRunnerRunsTerraformApply(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandApply,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := []recordedCommand{
		{
			dir:  "/tmp/openplan/runs/tenant_123/run_123",
			name: "tofu",
			args: []string{"apply", "-input=false", "-auto-approve", "-no-color", "tfplan"},
		},
	}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
}

// TestLocalTerraformRunnerRunsTerraformDestroy verifies that a destroy run's
// apply phase applies its saved plan too: the plan was made with -destroy, so
// applying it is the destroy, and running `tofu destroy` would plan again.
func TestLocalTerraformRunnerRunsTerraformDestroy(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandDestroy,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := []recordedCommand{
		{
			dir:  "/tmp/openplan/runs/tenant_123/run_123",
			name: "tofu",
			args: []string{"apply", "-input=false", "-auto-approve", "-no-color", "tfplan"},
		},
	}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
}

// Variables go to whatever plans and nowhere after it: a saved plan carries
// the values it was made with, and applying it must not be able to change
// them. An auto-approved apply plans as it applies, so it takes them. They
// arrive as a -var-file holding the config exactly as stored, never as
// TF_VAR_ entries in the environment.
func TestLocalTerraformRunnerSetsTerraformVariablesOnlyWhenPlanning(t *testing.T) {
	t.Parallel()

	const config = `{"enabled":true,"region":"us-east-1","replicas":3,"tags":{"env":"prod"},"zones":["us-east-1a","us-east-1b"]}`
	tests := []struct {
		name      string
		command   domain.TerraformCommandType
		args      []string
		variables bool
	}{
		{
			name:      "plan",
			command:   domain.TerraformCommandPlan,
			args:      []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out=tfplan"},
			variables: true,
		},
		{
			name:      "destroy plan",
			command:   domain.TerraformCommandPlanDestroy,
			args:      []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out=tfplan", "-destroy"},
			variables: true,
		},
		{
			name:    "apply",
			command: domain.TerraformCommandApply,
			args:    []string{"apply", "-input=false", "-auto-approve", "-no-color", "tfplan"},
		},
		{
			name:    "destroy",
			command: domain.TerraformCommandDestroy,
			args:    []string{"apply", "-input=false", "-auto-approve", "-no-color", "tfplan"},
		},
		{
			name:      "auto-approved apply",
			command:   domain.TerraformCommandApplyAutoApprove,
			args:      []string{"apply", "-input=false", "-auto-approve", "-no-color"},
			variables: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingCommandExecutor{}
			runner := NewLocalTerraformRunnerWithExecutor(executor)

			_, err := runner.Run(context.Background(), TerraformCommand{
				Binary:        domain.TerraformBinaryTofu,
				WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
				WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
				Command:       tt.command,
				ConfigJSON:    []byte(config),
			})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}

			if len(executor.commands) != 1 {
				t.Fatalf("ran %d commands, want 1", len(executor.commands))
			}
			got := executor.commands[0]
			if got.env != nil {
				t.Fatalf("env = %#v, want none: variables travel in the var file", got.env)
			}
			if !tt.variables {
				if !reflect.DeepEqual(got.args, tt.args) || got.varFile != nil {
					t.Fatalf("args = %#v, want %#v and no var file", got.args, tt.args)
				}
				return
			}
			if got.varFile == nil {
				t.Fatalf("args = %#v, want a -var-file", got.args)
			}
			wantArgs := append(append([]string(nil), tt.args...), "-var-file="+got.varFile.path)
			if !reflect.DeepEqual(got.args, wantArgs) {
				t.Fatalf("args = %#v, want %#v", got.args, wantArgs)
			}
			if got.varFile.contents != config {
				t.Fatalf("var file = %q, want the config as given", got.varFile.contents)
			}
			if got.varFile.mode != 0o600 {
				t.Fatalf("var file mode = %v, want 0600", got.varFile.mode)
			}
			if !filepath.IsAbs(got.varFile.path) || strings.HasPrefix(got.varFile.path, "/tmp/openplan/runs/") || !strings.HasSuffix(got.varFile.path, ".tfvars.json") {
				t.Fatalf("var file path = %q, want an absolute .tfvars.json outside the workspace", got.varFile.path)
			}
			if _, err := os.Stat(got.varFile.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("var file still there after the command: stat error = %v", err)
			}
		})
	}
}

// Strings a TF_VAR would have parsed as templates (${, %{) reach tofu as the
// stored bytes, in the file.
func TestLocalTerraformRunnerWritesTemplateLikeStringsVerbatim(t *testing.T) {
	t.Parallel()

	const config = `{"greeting":"a ${b} %{ if c }d%{ endif } \\/ e"}`
	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	if _, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
		ConfigJSON:    []byte(config),
	}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := executor.commands[0].varFile; got == nil || got.contents != config {
		t.Fatalf("var file = %#v, want %q", got, config)
	}
}

// No variables, no file and no flag: tofu reads the template's defaults.
func TestLocalTerraformRunnerPassesNoVarFileForAnEmptyConfig(t *testing.T) {
	t.Parallel()

	for _, config := range []string{"", "{}", " { } "} {
		executor := &recordingCommandExecutor{}
		runner := NewLocalTerraformRunnerWithExecutor(executor)

		if _, err := runner.Run(context.Background(), TerraformCommand{
			Binary:        domain.TerraformBinaryTofu,
			WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
			WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
			Command:       domain.TerraformCommandPlan,
			ConfigJSON:    []byte(config),
		}); err != nil {
			t.Fatalf("config %q: Run returned error: %v", config, err)
		}
		want := []string{"plan", "-input=false", "-no-color", "-detailed-exitcode", "-out=tfplan"}
		if got := executor.commands[0]; !reflect.DeepEqual(got.args, want) || got.varFile != nil {
			t.Fatalf("config %q: args = %#v, want %#v and no var file", config, got.args, want)
		}
	}
}

// A failed plan leaves no var file behind either.
func TestLocalTerraformRunnerRemovesTheVarFileWhenTheCommandFails(t *testing.T) {
	t.Parallel()

	commandErr := errors.New("exit status 1")
	executor := &recordingCommandExecutor{errs: []error{commandErr}}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
		ConfigJSON:    []byte(`{"region":"us-east-1"}`),
	})
	if !errors.Is(err, commandErr) {
		t.Fatalf("error = %v, want the command's", err)
	}
	varFile := executor.commands[0].varFile
	if varFile == nil {
		t.Fatal("plan ran without a var file")
	}
	if _, err := os.Stat(varFile.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("var file still there after a failed command: stat error = %v", err)
	}
}

// A config that isn't a JSON object fails the command before tofu runs.
func TestLocalTerraformRunnerRefusesAConfigThatIsNotAnObject(t *testing.T) {
	t.Parallel()

	for _, config := range []string{"null", "[]", `"x"`, "{"} {
		executor := &recordingCommandExecutor{}
		runner := NewLocalTerraformRunnerWithExecutor(executor)

		_, err := runner.Run(context.Background(), TerraformCommand{
			Binary:        domain.TerraformBinaryTofu,
			WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
			WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
			Command:       domain.TerraformCommandPlan,
			ConfigJSON:    []byte(config),
		})
		var cmdErr *CommandError
		if !errors.As(err, &cmdErr) || !strings.Contains(err.Error(), "variables") {
			t.Fatalf("config %q: error = %v, want a variables CommandError", config, err)
		}
		if len(executor.commands) != 0 {
			t.Fatalf("config %q: ran %#v, want nothing", config, executor.commands)
		}
	}
}

// An auto-approved apply has no saved plan to count, so its counts come from
// the "Apply complete!" line, and its output still reaches the log.
func TestLocalTerraformRunnerCountsAnAutoApprovedApplyFromItsOutput(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{stdout: "aws_s3_bucket.logs: Creating...\n\nApply complete! Resources: 1 imported, 2 added, 1 changed, 3 destroyed.\n"}
	runner := NewLocalTerraformRunnerWithExecutor(executor)
	var log bytes.Buffer

	result, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandApplyAutoApprove,
		Stdout:        &log,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := Result{HasChanges: true, Summary: domain.PlanSummary{Add: 2, Change: 1, Destroy: 3}}
	if result != want {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
	if log.String() != executor.stdout {
		t.Fatalf("log = %q, want the apply output", log.String())
	}
}

func TestSummarizeApplyOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   domain.PlanSummary
	}{
		{name: "changes", output: "Apply complete! Resources: 2 added, 1 changed, 0 destroyed.\n", want: domain.PlanSummary{Add: 2, Change: 1}},
		{name: "no changes", output: "No changes.\n\nApply complete! Resources: 0 added, 0 changed, 0 destroyed.\n"},
		{name: "no summary line", output: "Error: something\n"},
		{name: "counts after the line are not its", output: "Apply complete! Resources: 0 added, 0 changed, 0 destroyed.\nOutputs:\nx = \"5 added\"\n"},
		{name: "destroy", output: "Destroy complete! Resources: 3 destroyed.\n", want: domain.PlanSummary{Destroy: 3}},
	}
	for _, tt := range tests {
		if got := summarizeApplyOutput(tt.output); got != tt.want {
			t.Fatalf("%s: summarizeApplyOutput = %#v, want %#v", tt.name, got, tt.want)
		}
	}
}

// exitCodeError stands in for *exec.ExitError, whose exit code is how
// -detailed-exitcode reports a plan with changes.
type exitCodeError struct{ code int }

func (err exitCodeError) Error() string { return fmt.Sprintf("exit status %d", err.code) }
func (err exitCodeError) ExitCode() int { return err.code }

// Exit 2 from a -detailed-exitcode plan is a plan with changes, not a failure.
// The runner then reads the saved plan back and counts what it would do.
func TestLocalTerraformRunnerCountsTheChangesOfAPlanThatHasThem(t *testing.T) {
	t.Parallel()

	executor := &scriptedCommandExecutor{steps: []scriptedStep{
		{err: exitCodeError{code: 2}},
		{stdout: `{"resource_changes":[
			{"change":{"actions":["create"]}},
			{"change":{"actions":["create"]}},
			{"change":{"actions":["update"]}},
			{"change":{"actions":["delete","create"]}},
			{"change":{"actions":["read"]}},
			{"change":{"actions":["no-op"]}}
		]}`},
	}}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	result, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
		Environment:   map[string]string{"AWS_REGION": "us-east-1"},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := Result{HasChanges: true, Summary: domain.PlanSummary{Add: 3, Change: 1, Destroy: 1}}
	if result != want {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
	show := executor.commands[1]
	if !reflect.DeepEqual(show.args, []string{"show", "-json", "-no-color", "tfplan"}) {
		t.Fatalf("show args = %#v", show.args)
	}
	if !reflect.DeepEqual(show.env, []string{"AWS_REGION=us-east-1"}) {
		t.Fatalf("show env = %#v, want the credentials and no variables", show.env)
	}
}

func TestLocalTerraformRunnerReportsAPlanWithoutChangesAsSuch(t *testing.T) {
	t.Parallel()

	executor := &scriptedCommandExecutor{steps: []scriptedStep{{}}}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	result, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result != (Result{}) {
		t.Fatalf("result = %#v, want no changes", result)
	}
	if len(executor.commands) != 1 {
		t.Fatalf("commands = %#v, want only the plan", executor.commands)
	}
}

// Any exit code but 0 and 2 is a failed plan, even though 2 is not.
func TestLocalTerraformRunnerFailsAPlanThatExitsOne(t *testing.T) {
	t.Parallel()

	executor := &scriptedCommandExecutor{steps: []scriptedStep{{err: exitCodeError{code: 1}}}}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
	})
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Command != domain.TerraformCommandPlan {
		t.Fatalf("error = %v, want a plan CommandError", err)
	}
}

// The binary is the command's: a deployment configured for Terraform runs the
// same arguments under terraform.
func TestLocalTerraformRunnerRunsTheCommandsBinary(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTerraform,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandInit,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	want := []recordedCommand{{
		dir:  "/tmp/openplan/runs/tenant_123/run_123",
		name: "terraform",
		args: []string{"init", "-input=false", "-no-color"},
	}}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
}

// The binary's name crosses into the sandbox as data, so only the two known
// names run; anything else, an empty value included, is refused before any
// command.
func TestLocalTerraformRunnerRefusesAnUnknownBinary(t *testing.T) {
	t.Parallel()

	for _, binary := range []domain.TerraformBinary{"", "tf", "/usr/local/bin/tofu", "sh"} {
		executor := &recordingCommandExecutor{}
		runner := NewLocalTerraformRunnerWithExecutor(executor)

		_, err := runner.Run(context.Background(), TerraformCommand{
			Binary:        binary,
			WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
			WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
			Command:       domain.TerraformCommandInit,
		})
		if err == nil || !strings.Contains(err.Error(), "not tofu or terraform") {
			t.Fatalf("binary %q: error = %v, want the binary refused", binary, err)
		}
		if len(executor.commands) != 0 {
			t.Fatalf("binary %q: commands = %#v, want none", binary, executor.commands)
		}
	}
}

// A deployment that says terraform on an executor image that was never given
// one fails its first command; the error says so and where the binary goes,
// rather than the bare "executable file not found".
func TestLocalTerraformRunnerNamesAMissingBinary(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{errs: []error{&exec.Error{Name: "terraform", Err: exec.ErrNotFound}}}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTerraform,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandInit,
	})
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Command != domain.TerraformCommandInit {
		t.Fatalf("error = %v, want an init CommandError", err)
	}
	for _, want := range []string{"terraform is not in the executor image", "/opt/openplan/guest/usr/local/bin", "README"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to say %q", err, want)
		}
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v, want it to keep exec.ErrNotFound", err)
	}
}

func TestLocalTerraformRunnerSelectsExistingWorkspace(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandSelectWorkspace,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := []recordedCommand{
		{
			dir:  "/tmp/openplan/runs/tenant_123/run_123",
			name: "tofu",
			args: []string{"workspace", "select", "-no-color", "mtp_acme_prod_vpc_a13f9c"},
		},
	}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
}

func TestLocalTerraformRunnerCreatesMissingWorkspace(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{
		errs: []error{errors.New("workspace does not exist")},
	}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandSelectWorkspace,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	want := []recordedCommand{
		{
			dir:  "/tmp/openplan/runs/tenant_123/run_123",
			name: "tofu",
			args: []string{"workspace", "select", "-no-color", "mtp_acme_prod_vpc_a13f9c"},
		},
		{
			dir:  "/tmp/openplan/runs/tenant_123/run_123",
			name: "tofu",
			args: []string{"workspace", "new", "-no-color", "mtp_acme_prod_vpc_a13f9c"},
		},
	}
	if !reflect.DeepEqual(executor.commands, want) {
		t.Fatalf("commands = %#v, want %#v", executor.commands, want)
	}
}

func TestLocalTerraformRunnerWrapsWorkspaceCreationErrorWithTofuContext(t *testing.T) {
	t.Parallel()

	createErr := errors.New("create failed")
	executor := &recordingCommandExecutor{
		errs: []error{errors.New("workspace does not exist"), createErr},
	}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandSelectWorkspace,
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("error = %v, want createErr", err)
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %v, want *CommandError", err)
	}
	if cmdErr.Command != domain.TerraformCommandSelectWorkspace {
		t.Fatalf("cmdErr.Command = %q, want %q", cmdErr.Command, domain.TerraformCommandSelectWorkspace)
	}
}

func TestLocalTerraformRunnerWrapsCommandErrors(t *testing.T) {
	t.Parallel()

	commandErr := errors.New("terraform failed")
	executor := &recordingCommandExecutor{errs: []error{commandErr}}
	runner := NewLocalTerraformRunnerWithExecutor(executor)

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
	})
	if !errors.Is(err, commandErr) {
		t.Fatalf("error = %v, want commandErr", err)
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %v, want *CommandError", err)
	}
	if cmdErr.Command != domain.TerraformCommandPlan {
		t.Fatalf("cmdErr.Command = %q, want %q", cmdErr.Command, domain.TerraformCommandPlan)
	}
}

func TestLocalTerraformRunnerPassesOutputWritersToExecutor(t *testing.T) {
	t.Parallel()

	executor := &recordingCommandExecutor{
		stdout: "terraform stdout\n",
		stderr: "terraform stderr\n",
	}
	runner := NewLocalTerraformRunnerWithExecutor(executor)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
		Stdout:        &stdout,
		Stderr:        &stderr,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if stdout.String() != "terraform stdout\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "terraform stderr\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLocalTerraformRunnerRequiresWorkspacePath(t *testing.T) {
	t.Parallel()

	runner := NewLocalTerraformRunnerWithExecutor(&recordingCommandExecutor{})

	_, err := runner.Run(context.Background(), TerraformCommand{
		Binary:        domain.TerraformBinaryTofu,
		WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
		Command:       domain.TerraformCommandPlan,
	})
	if err == nil {
		t.Fatal("Run returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "workspace path") {
		t.Fatalf("error = %q, want workspace path context", err.Error())
	}
}

type recordingCommandExecutor struct {
	commands []recordedCommand
	errs     []error
	stdout   string
	stderr   string
}

func (executor *recordingCommandExecutor) Run(_ context.Context, dir string, env []string, stdout io.Writer, stderr io.Writer, name string, args ...string) error {
	executor.commands = append(executor.commands, recordedCommand{
		dir:     dir,
		env:     append([]string(nil), env...),
		name:    name,
		args:    append([]string(nil), args...),
		varFile: readVarFile(args),
	})
	if executor.stdout != "" {
		if _, err := io.WriteString(stdout, executor.stdout); err != nil {
			return err
		}
	}
	if executor.stderr != "" {
		if _, err := io.WriteString(stderr, executor.stderr); err != nil {
			return err
		}
	}
	if len(executor.errs) == 0 {
		return nil
	}
	err := executor.errs[0]
	executor.errs = executor.errs[1:]
	return err
}

// scriptedCommandExecutor plays back one step per command, for commands whose
// outputs differ: a plan's exit code and then its saved plan's JSON.
type scriptedCommandExecutor struct {
	steps    []scriptedStep
	commands []recordedCommand
}

type scriptedStep struct {
	stdout string
	err    error
}

func (executor *scriptedCommandExecutor) Run(_ context.Context, dir string, env []string, stdout io.Writer, _ io.Writer, name string, args ...string) error {
	executor.commands = append(executor.commands, recordedCommand{dir: dir, env: append([]string(nil), env...), name: name, args: append([]string(nil), args...)})
	if len(executor.steps) == 0 {
		return nil
	}
	step := executor.steps[0]
	executor.steps = executor.steps[1:]
	if step.stdout != "" {
		if _, err := io.WriteString(stdout, step.stdout); err != nil {
			return err
		}
	}
	return step.err
}

type recordedCommand struct {
	dir     string
	env     []string
	name    string
	args    []string
	varFile *recordedVarFile
}

// recordedVarFile is the -var-file a command was given, read while it ran:
// the runner removes it once the command returns.
type recordedVarFile struct {
	path     string
	contents string
	mode     os.FileMode
}

func readVarFile(args []string) *recordedVarFile {
	for _, arg := range args {
		path, ok := strings.CutPrefix(arg, "-var-file=")
		if !ok {
			continue
		}
		recorded := &recordedVarFile{path: path}
		if info, err := os.Stat(path); err == nil {
			recorded.mode = info.Mode().Perm()
		}
		if contents, err := os.ReadFile(path); err == nil {
			recorded.contents = string(contents)
		}
		return recorded
	}
	return nil
}

// An approved apply and a destroy apply a saved plan, and count what they did
// from the closing line, as an auto-approved apply does, whichever of tofu's
// two lines it prints.
func TestLocalTerraformRunnerCountsASavedPlanApplyFromItsOutput(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		command domain.TerraformCommandType
		stdout  string
		want    domain.PlanSummary
	}{
		{name: "apply", command: domain.TerraformCommandApply, stdout: "Apply complete! Resources: 2 added, 1 changed, 0 destroyed.\n", want: domain.PlanSummary{Add: 2, Change: 1}},
		{name: "destroy", command: domain.TerraformCommandDestroy, stdout: "Destroy complete! Resources: 3 destroyed.\n", want: domain.PlanSummary{Destroy: 3}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := &recordingCommandExecutor{stdout: testCase.stdout}
			runner := NewLocalTerraformRunnerWithExecutor(executor)
			var log bytes.Buffer

			result, err := runner.Run(context.Background(), TerraformCommand{
				Binary:        domain.TerraformBinaryTofu,
				WorkspacePath: "/tmp/openplan/runs/tenant_123/run_123",
				WorkspaceName: "mtp_acme_prod_vpc_a13f9c",
				Command:       testCase.command,
				Stdout:        &log,
			})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if want := (Result{HasChanges: true, Summary: testCase.want}); result != want {
				t.Fatalf("result = %#v, want %#v", result, want)
			}
			if log.String() != testCase.stdout {
				t.Fatalf("log = %q, want the apply output", log.String())
			}
		})
	}
}

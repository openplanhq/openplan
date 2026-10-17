package app

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFieldErrorMatchesItsSentinel(t *testing.T) {
	t.Parallel()

	err := fieldError(ErrInvalidCommand, IssueRequired, "name is required", "name")
	if !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("errors.Is(%v, ErrInvalidCommand) = false", err)
	}
	if got, want := err.Error(), "invalid command: name is required"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// The service wraps what a validator returns, and a handler may join it with
// something else; the field errors must survive both or the API would answer
// 500 for bad input.
func TestFieldErrorsFindsEveryJoinedError(t *testing.T) {
	t.Parallel()

	name := fieldError(ErrInvalidCommand, IssueRequired, "name is required", "name")
	region := fieldError(ErrStackTemplateConfigInvalid, IssueRequired, "region is required", "config", "region")
	err := errors.Join(fmt.Errorf("create stack: %w", errors.Join(name, region)), errors.New("unrelated"))

	found := FieldErrors(err)
	if len(found) != 2 {
		t.Fatalf("FieldErrors = %v, want two", found)
	}
	if found[0].Message != "name is required" || strings.Join(found[1].Path, "/") != "config/region" {
		t.Fatalf("FieldErrors = %+v, %+v, want name then config/region", found[0], found[1])
	}
}

func TestFieldErrorsOfAPlainErrorIsEmpty(t *testing.T) {
	t.Parallel()

	if found := FieldErrors(fmt.Errorf("get stack: %w", ErrNotFound)); len(found) != 0 {
		t.Fatalf("FieldErrors = %v, want none", found)
	}
	if found := FieldErrors(nil); len(found) != 0 {
		t.Fatalf("FieldErrors(nil) = %v, want none", found)
	}
}

// Sentinels is what the API's coverage test iterates, so a sentinel missing
// from it would escape that test and could reach a client as an unmapped 500.
// Reading the source is the only way to know every exported error var.
func TestSentinelsListsEveryExportedError(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	fileSet := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(fileSet, file, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range valueSpec.Names {
					if name.IsExported() && strings.HasPrefix(name.Name, "Err") {
						declared = append(declared, name.Name)
					}
				}
			}
		}
	}

	listed := map[string]bool{}
	for _, sentinel := range Sentinels() {
		listed[sentinelNames[sentinel]] = true
	}
	for _, name := range declared {
		if !listed[name] {
			t.Errorf("%s is not in Sentinels()", name)
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no exported Err vars; the parse is broken")
	}
}

// sentinelNames lets the test above name what Sentinels returns.
var sentinelNames = map[error]string{
	ErrUnauthenticated:                    "ErrUnauthenticated",
	ErrInvalidCommand:                     "ErrInvalidCommand",
	ErrInvalidQuery:                       "ErrInvalidQuery",
	ErrDuplicateStackSlug:                 "ErrDuplicateStackSlug",
	ErrDuplicateStackTemplateComponentKey: "ErrDuplicateStackTemplateComponentKey",
	ErrNotFound:                           "ErrNotFound",
	ErrTemplateNotInstallable:             "ErrTemplateNotInstallable",
	ErrStackTemplateConfigInvalid:         "ErrStackTemplateConfigInvalid",
	ErrStackTemplateUpgradeInvalid:        "ErrStackTemplateUpgradeInvalid",
	ErrStackTemplateNotEditable:           "ErrStackTemplateNotEditable",
	ErrStackTemplateNotRunnable:           "ErrStackTemplateNotRunnable",
	ErrStackTemplatePlanStale:             "ErrStackTemplatePlanStale",
	ErrTemplateRunInFlight:                "ErrTemplateRunInFlight",
	ErrRunNotApprovable:                   "ErrRunNotApprovable",
	ErrRunNotDiscardable:                  "ErrRunNotDiscardable",
	ErrLastOwner:                          "ErrLastOwner",
	ErrCredentialNameInvalid:              "ErrCredentialNameInvalid",
	ErrDuplicateCredentialName:            "ErrDuplicateCredentialName",
	ErrCredentialEncryptionUnavailable:    "ErrCredentialEncryptionUnavailable",
	ErrTemplateRunLogLive:                 "ErrTemplateRunLogLive",
	ErrTemplateRunLogLost:                 "ErrTemplateRunLogLost",
	ErrForbidden:                          "ErrForbidden",
	ErrUserNotProvisioned:                 "ErrUserNotProvisioned",
}

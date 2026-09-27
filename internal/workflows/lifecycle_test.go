package workflows

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// statusWriters are the activities that write a status, and the helpers that
// schedule one, each with the functions allowed to name it. BeginApply and
// FinishPlan are here because they move a run's status as part of their own
// work; setStatus and failOnError because they write it for the run's two
// lifecycles.
var statusWriters = map[string][]string{
	"RecordTemplateRunStatusActivityName":          {"(*run).setStatus"},
	"setStatus":                                    {"(*planWorkflow).lifecycle", "(*applyWorkflow).lifecycle", "(*run).failOnError"},
	"failOnError":                                  {"(*planWorkflow).lifecycle", "(*applyWorkflow).lifecycle"},
	"BeginApplyActivityName":                       {"(*applyWorkflow).lifecycle"},
	"FinishPlanActivityName":                       {"(*planWorkflow).lifecycle"},
	"RecordTemplateRegistrationStatusActivityName": {"(*registration).lifecycle"},
}

// TestOnlyLifecycleWritesStatus pins that a workflow's status is written in
// its lifecycles: every activity that writes it is named only there, or in a
// helper only they call, so reading the lifecycles is reading every status
// change. It also fails when a guarded name appears nowhere, so a renamed
// activity cannot leave it passing while guarding nothing.
func TestOnlyLifecycleWritesStatus(t *testing.T) {
	t.Parallel()

	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			owner := "package scope"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner = funcName(fn)
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				sel, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				want, ok := statusWriters[sel.Sel.Name]
				if !ok {
					return true
				}
				seen[sel.Sel.Name] = true
				if !slices.Contains(want, owner) {
					t.Errorf("%s: %s names %s; only %v may write status", fset.Position(sel.Pos()), owner, sel.Sel.Name, want)
				}
				return true
			})
		}
	}
	for name := range statusWriters {
		if !seen[name] {
			t.Errorf("nothing names %s: the guard is out of date", name)
		}
	}
}

// funcName names a function as the guard reports it: (*planWorkflow).lifecycle
// for a method, lifecycle for a function.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	switch recv := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := recv.X.(*ast.Ident); ok {
			return "(*" + ident.Name + ")." + fn.Name.Name
		}
	case *ast.Ident:
		return recv.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

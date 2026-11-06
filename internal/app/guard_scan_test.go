package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// unguardedMethods is every exported Service method that doesn't start with a
// guard. Reads are here because they write no audit event. A new method fails
// TestEveryServiceMethodIsGuardedOrListed until someone decides which it is,
// which is the point: forgetting to audit a refusal should not compile green.
var unguardedMethods = []string{
	"GetStack",
	"GetTemplate",
	"GetTemplateRevisionVariables",
	"GetTemplateRun",
	"GetTemplateRunByNumber",
	"GetTemplateRunLog",
	"GetTemplateRunLogMetadata",
	"ListAttention",
	"ListCredentials",
	"ListPlatformGrants",
	"ListStackGrants",
	"ListStackSummaries",
	"ListStacks",
	"ListTemplateRevisionsForTemplate",
	"ListTemplateRunLogs",
	"ListTemplateRunResourceChanges",
	"ListTemplateRuns",
	"ListTemplates",
	"PlatformCapabilities",
	// Writes only the caller's own sign-in, so there's nothing to refuse.
	"RecordSignIn",
	"SearchUsers",
	"TemplateRunLogState",
}

func TestEveryServiceMethodIsGuardedOrListed(t *testing.T) {
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
			method, ok := decl.(*ast.FuncDecl)
			if !ok || !method.Name.IsExported() || !isServiceMethod(method) {
				continue
			}
			seen[method.Name.Name] = true
			guarded := callsAGuard(method)
			listed := slices.Contains(unguardedMethods, method.Name.Name)
			switch {
			case !guarded && !listed:
				t.Errorf("%s doesn't start with a guard: if it's a write, call guard*Write first; if it isn't, add it to unguardedMethods", method.Name.Name)
			case guarded && listed:
				t.Errorf("%s calls a guard and is in unguardedMethods: remove it from the list", method.Name.Name)
			}
		}
	}
	// A renamed method can't leave the list passing while it guards nothing.
	for _, name := range unguardedMethods {
		if !seen[name] {
			t.Errorf("unguardedMethods names %s, which isn't an exported Service method", name)
		}
	}
}

func isServiceMethod(method *ast.FuncDecl) bool {
	if method.Recv == nil || len(method.Recv.List) != 1 {
		return false
	}
	star, ok := method.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "Service"
}

func callsAGuard(method *ast.FuncDecl) bool {
	found := false
	ast.Inspect(method, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			name := selector.Sel.Name
			if strings.HasPrefix(name, "guard") && strings.HasSuffix(name, "Write") {
				found = true
			}
		}
		return !found
	})
	return found
}

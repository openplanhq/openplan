package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// snakeCase is the one casing the API's JSON uses, in both directions.
var snakeCase = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

// TestWireFieldsAreSnakeCase guards the casing of every JSON field the API
// can send or accept. Wire shapes are not confined to this package: app and
// domain types are serialized as they are, which is how camelCase fields
// crept in before. So it reads the tags from source rather than reflecting
// over a list of response types that would have to be kept complete by hand.
//
// Packages that speak someone else's format (OIDC in authentication, runsc's
// output in sandbox) are left out on purpose: their names are not ours.
func TestWireFieldsAreSnakeCase(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{".", "../app", "../domain"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			checkJSONTags(t, path)
		}
	}
}

func checkJSONTags(t *testing.T, path string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if !ok || field.Tag == nil {
			return true
		}
		raw, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			t.Fatalf("%s: unquote tag: %v", fset.Position(field.Pos()), err)
		}
		value, tagged := reflect.StructTag(raw).Lookup("json")
		name, _, _ := strings.Cut(value, ",")
		// "-" drops the field. An empty name is not skipped: it puts the Go
		// field name on the wire, which is never snake_case.
		if !tagged || name == "-" {
			return true
		}
		if !snakeCase.MatchString(name) {
			t.Errorf("%s: json field %q is not snake_case", fset.Position(field.Pos()), name)
		}
		return true
	})
}

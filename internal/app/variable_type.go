package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/gocty"
	ctyjson "github.com/zclconf/go-cty/cty/json"

	"github.com/vishu42/openplan/internal/domain"
)

// checkVariableValue reports whether raw fits the variable's declared type,
// read the way Terraform reads a .tfvars.json value: JSON's own shape, then
// Terraform's conversion, so "3" fits a number and "true" a bool, at any
// depth. Catching a misshapen value here puts the error under its field at
// save, instead of in a plan log minutes later.
func checkVariableValue(variable domain.TemplateVariable, raw json.RawMessage) error {
	wantType, ok := variableType(variable.TypeExpression)
	if !ok {
		return nil
	}

	impliedType, err := ctyjson.ImpliedType(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", variable.Name, err)
	}
	value, err := ctyjson.Unmarshal(raw, impliedType)
	if err != nil {
		return fmt.Errorf("%s: %w", variable.Name, err)
	}
	// Defaults are not applied: they only fill nulls, so they can't make a
	// misshapen value fit.
	if _, err := convert.Convert(value, wantType); err != nil {
		return errors.New(describeConversionError(variable.Name, err))
	}
	return nil
}

// variableType parses a variable's type expression. An untyped variable is
// any, as in Terraform. A type openplan can't parse is reported as not ok,
// and the value goes unchecked: tofu is the authority on its own syntax and
// will say what's wrong at plan, which beats refusing a value tofu accepts.
func variableType(typeExpression string) (cty.Type, bool) {
	if strings.TrimSpace(typeExpression) == "" {
		return cty.DynamicPseudoType, true
	}
	expression, diags := hclsyntax.ParseExpression([]byte(typeExpression), "type", hcl.InitialPos)
	if diags.HasErrors() {
		return cty.NilType, false
	}
	wantType, _, diags := typeexpr.TypeConstraintWithDefaults(expression)
	if diags.HasErrors() {
		return cty.NilType, false
	}
	return wantType, true
}

// describeConversionError phrases a conversion failure as name, then the
// path inside the value, then cty's message: "subnets[1]: a number is
// required".
func describeConversionError(name string, err error) string {
	var pathErr cty.PathError
	if !errors.As(err, &pathErr) {
		return name + ": " + err.Error()
	}
	return name + formatValuePath(pathErr.Path) + ": " + pathErr.Error()
}

func formatValuePath(path cty.Path) string {
	var out strings.Builder
	for _, step := range path {
		switch step := step.(type) {
		case cty.GetAttrStep:
			out.WriteString("." + step.Name)
		case cty.IndexStep:
			key := step.Key
			switch {
			case !key.IsKnown() || key.IsNull():
				return out.String()
			case key.Type() == cty.String:
				fmt.Fprintf(&out, "[%q]", key.AsString())
			case key.Type() == cty.Number:
				var index int
				if gocty.FromCtyValue(key, &index) != nil {
					return out.String()
				}
				fmt.Fprintf(&out, "[%d]", index)
			default:
				// A set element is keyed by its own value; naming it adds
				// nothing the reader can act on.
				return out.String()
			}
		}
	}
	return out.String()
}

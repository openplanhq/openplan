package app

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
)

func typedVariable(name, typeExpression string) domain.TemplateVariable {
	return domain.TemplateVariable{Name: name, TypeExpression: typeExpression}
}

func TestValidateTemplateConfigAcceptsValuesThatFitTheirType(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, typeExpression, value string
	}{
		{"list", "list(string)", `["a","b"]`},
		{"set", "set(string)", `["a","b"]`},
		{"tuple", "tuple([string, number])", `["a",1]`},
		{"map", "map(string)", `{"k":"v"}`},
		{"empty map", "map(string)", `{}`},
		{"object", "object({ cidr = string, azs = list(string) })", `{"cidr":"10.0.0.0/16","azs":["a"]}`},
		{"optional attribute left out", "object({ cidr = string, tags = optional(map(string)) })", `{"cidr":"10.0.0.0/16"}`},
		{"optional with default left out", `object({ cidr = optional(string, "10.0.0.0/16") })`, `{}`},
		{"any list", "any", `[1,"a"]`},
		{"untyped object", "", `{"a":[1]}`},
		{"string", "string", `"text"`},
		{"string given for number", "number", `"3"`},
		{"string given for bool", "bool", `"true"`},
		{"string given for a number inside an object", "object({ size = number })", `{"size":"3"}`},
		{"number given for string", "string", `3`},
		{"multi-line type", "object({\n  cidr = string\n})", `{"cidr":"x"}`},
		// openplan can't read this type; tofu is the authority and says so at plan.
		{"unparseable type", "list(", `"anything"`},
		{"unknown type keyword", "lisst(string)", `"anything"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := json.RawMessage(`{"v":` + tc.value + `}`)
			if _, err := validateTemplateConfig(raw, []domain.TemplateVariable{typedVariable("v", tc.typeExpression)}); err != nil {
				t.Fatalf("validateTemplateConfig(%s) as %q returned error: %v", raw, tc.typeExpression, err)
			}
		})
	}
}

// The detail names the variable and where inside it the value is wrong, so
// the form can say which element to fix.
func TestValidateTemplateConfigRefusesValuesThatDoNotFit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, typeExpression, value, detail string
	}{
		{"wrong element type", "list(number)", `[1,"x"]`, "v[1]: a number is required"},
		{"wrong attribute type", "object({ cidr = string, azs = list(string) })", `{"cidr":"x","azs":"a"}`, `v: attribute "azs": list of string required, but have string`},
		{"missing required attribute", "object({ cidr = string, azs = list(string) })", `{"cidr":"x"}`, `v: attribute "azs" is required`},
		{"map given a list", "map(string)", `["a"]`, "v: map of string required"},
		{"string given an object", "string", `{"a":1}`, "v: string required, but have object"},
		{"wrong value in a map", "map(number)", `{"k":"x"}`, `v["k"]: a number is required`},
		{"wrong attribute inside a list", "list(object({ size = number }))", `[{"size":1},{"size":"x"}]`, "v[1].size: a number is required"},
		{"bool given a word", "bool", `"yes"`, "v: a bool is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := json.RawMessage(`{"v":` + tc.value + `}`)
			_, err := validateTemplateConfig(raw, []domain.TemplateVariable{typedVariable("v", tc.typeExpression)})
			if !errors.Is(err, ErrStackTemplateConfigInvalid) {
				t.Fatalf("error = %v, want ErrStackTemplateConfigInvalid", err)
			}
			assertFieldIssues(t, err, "config/v invalid")
			if got := FieldErrors(err)[0].Message; got != tc.detail {
				t.Fatalf("detail = %q, want %q", got, tc.detail)
			}
		})
	}
}

func TestValidateTemplateConfigReportsEveryMisshapenVariableInOrder(t *testing.T) {
	t.Parallel()

	variables := []domain.TemplateVariable{
		{Name: "req", Required: true},
		typedVariable("b", "list(string)"),
		typedVariable("a", "number"),
		typedVariable("ok", "map(string)"),
	}
	_, err := validateTemplateConfig(json.RawMessage(`{"b":"x","a":"many","ok":{},"z":1}`), variables)
	assertFieldIssues(t, err,
		"config/a invalid",
		"config/b invalid",
		"config/z unknown",
		"config/req required",
	)
}

// The value reaches the run as sent, never as openplan converted it: "3" for
// a number stays a string, and the JSON is only compacted.
func TestValidateTemplateConfigStoresTheValueAsSent(t *testing.T) {
	t.Parallel()

	variables := []domain.TemplateVariable{
		typedVariable("size", "number"),
		typedVariable("network", "object({ cidr = string, tags = optional(map(string)) })"),
	}
	got, err := validateTemplateConfig(json.RawMessage(`{ "size": "3", "network": { "cidr": "10.0.0.0/16" } }`), variables)
	if err != nil {
		t.Fatalf("validateTemplateConfig returned error: %v", err)
	}
	if want := `{"network":{"cidr":"10.0.0.0/16"},"size":"3"}`; string(got) != want {
		t.Fatalf("stored config = %s, want %s", got, want)
	}
}

// Upgrade re-checks carried values against the target revision, so a value
// that fit the old type but not the new one is refused under its field
// instead of failing the next plan.
func TestUpgradeStackTemplateRefusesACarriedValueTheNewTypeRejects(t *testing.T) {
	t.Parallel()

	stackTemplates := &recordingStackTemplateRepository{
		stackTemplate: domain.StackTemplate{
			ID:                        domain.StackTemplateID("stack_template_123"),
			TenantID:                  domain.TenantID("tenant_123"),
			SourceTemplateID:          domain.SourceTemplateID("source_template_vpc"),
			DesiredTemplateRevisionID: domain.TemplateRevisionID("template_rev_1"),
			DesiredConfigJSON:         json.RawMessage(`{"azs":"us-east-1a"}`),
			Lifecycle:                 domain.StackTemplateActive,
		},
	}
	templates := &recordingTemplateRepository{
		template: domain.TemplateRevision{
			ID:               domain.TemplateRevisionID("template_rev_2"),
			TenantID:         domain.TenantID("tenant_123"),
			SourceTemplateID: domain.SourceTemplateID("source_template_vpc"),
			Status:           domain.TemplateRevisionActive,
		},
		variables: []domain.TemplateVariable{{Name: "azs", Required: true, TypeExpression: "list(string)"}},
	}
	service := NewService(Service{
		TemplateRuns:             &recordingTemplateRunRepository{},
		Authorization:            testPlatformAuthorizer(t),
		StackTemplates:           stackTemplates,
		TemplateRevisionMetadata: templates,
		TemplateRevisions:        templates,
	})

	_, err := service.UpgradeStackTemplate(authenticatedContext(), UpgradeStackTemplateCommand{
		TenantID:                 domain.TenantID("tenant_123"),
		StackTemplateID:          domain.StackTemplateID("stack_template_123"),
		TargetTemplateRevisionID: domain.TemplateRevisionID("template_rev_2"),
	})
	if !errors.Is(err, ErrStackTemplateConfigInvalid) {
		t.Fatalf("error = %v, want ErrStackTemplateConfigInvalid", err)
	}
	assertFieldIssues(t, err, "config/azs invalid")
	if stackTemplates.gotDesiredTemplateRevisionID != "" {
		t.Fatalf("desired revision updated to %q, want no update", stackTemplates.gotDesiredTemplateRevisionID)
	}
}

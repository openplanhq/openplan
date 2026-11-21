package postgres

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
)

// json.Marshal writes a nil slice as null and a nil map as null. These
// columns are read by the API, which serves what they hold, so none is
// stored as null, and a row stored as null before that is read as empty.

func TestEncodeJSONColumnsStoreNoneAsEmpty(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		encode func() ([]byte, error)
		want   string
	}{
		{"nil array", func() ([]byte, error) { return encodeJSONArray[string](nil) }, "[]"},
		{"empty array", func() ([]byte, error) { return encodeJSONArray([]string{}) }, "[]"},
		{"array", func() ([]byte, error) { return encodeJSONArray([]string{"network", "aws"}) }, `["network","aws"]`},
		{"typed array", func() ([]byte, error) { return encodeJSONArray([]domain.CredentialID{"cred_1"}) }, `["cred_1"]`},
		{"nil object", func() ([]byte, error) { return encodeJSONObject[string](nil) }, "{}"},
		{"empty object", func() ([]byte, error) { return encodeJSONObject(map[string]string{}) }, "{}"},
		{"object", func() ([]byte, error) { return encodeJSONObject(map[string]string{"env": "prod"}) }, `{"env":"prod"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.encode()
			if err != nil {
				t.Fatalf("encode returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("encode = %s, want %s", got, tc.want)
			}
		})
	}
}

// The decoded value of a column stored as null or empty is served as empty,
// not null, for every field these columns back.
func TestDecodeJSONColumnsReadNullAsEmpty(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"null", "[]", "{}"} {
		array := stored != "{}"
		if array {
			tags, err := decodeJSONArray[string]([]byte(stored))
			if err != nil || tags == nil || len(tags) != 0 {
				t.Fatalf("decodeJSONArray[string](%s) = %#v, %v; want an empty non-nil slice", stored, tags, err)
			}
			ids, err := decodeJSONArray[domain.CredentialID]([]byte(stored))
			if err != nil || ids == nil || len(ids) != 0 {
				t.Fatalf("decodeJSONArray[CredentialID](%s) = %#v, %v; want an empty non-nil slice", stored, ids, err)
			}
			assertServed(t, domain.TemplateRevision{Tags: tags}, `"tags":[]`)
			assertServed(t, domain.Stack{DefaultCredentialIDs: ids}, `"default_credential_ids":[]`)
		}
		if stored != "[]" {
			tags, err := decodeJSONObject[string]([]byte(stored))
			if err != nil || tags == nil || len(tags) != 0 {
				t.Fatalf("decodeJSONObject[string](%s) = %#v, %v; want an empty non-nil map", stored, tags, err)
			}
			assertServed(t, domain.Stack{Tags: tags}, `"tags":{}`)
		}
	}

	ids, err := decodeJSONArray[domain.CredentialID]([]byte(`["cred_1"]`))
	if err != nil || len(ids) != 1 || ids[0] != "cred_1" {
		t.Fatalf("decodeJSONArray = %#v, %v; want [cred_1]", ids, err)
	}
	tags, err := decodeJSONObject[string]([]byte(`{"env":"prod"}`))
	if err != nil || len(tags) != 1 || tags["env"] != "prod" {
		t.Fatalf("decodeJSONObject = %#v, %v; want env=prod", tags, err)
	}
}

func assertServed(t *testing.T, value any, want string) {
	t.Helper()
	served, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}
	if !strings.Contains(string(served), want) {
		t.Fatalf("served %s, want %s", served, want)
	}
}

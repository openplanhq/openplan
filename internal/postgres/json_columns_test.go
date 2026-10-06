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

func TestEncodeTemplateRevisionTagsStoresNoTagsAsAnEmptyArray(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tags []string
		want string
	}{
		{name: "nil", tags: nil, want: "[]"},
		{name: "empty", tags: []string{}, want: "[]"},
		{name: "tags", tags: []string{"network", "aws"}, want: `["network","aws"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeTemplateRevisionTags(tc.tags)
			if err != nil {
				t.Fatalf("encodeTemplateRevisionTags returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("encodeTemplateRevisionTags(%#v) = %s, want %s", tc.tags, got, tc.want)
			}
		})
	}
}

func TestDecodeTemplateRevisionTagsReadsNullAsNoTags(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"null", "[]"} {
		tags, err := decodeTemplateRevisionTags([]byte(stored))
		if err != nil {
			t.Fatalf("decodeTemplateRevisionTags(%s) returned error: %v", stored, err)
		}
		if tags == nil || len(tags) != 0 {
			t.Fatalf("decodeTemplateRevisionTags(%s) = %#v, want an empty non-nil slice", stored, tags)
		}
		// What the API serves for it.
		served, err := json.Marshal(domain.TemplateRevision{Tags: tags})
		if err != nil {
			t.Fatalf("marshal template revision: %v", err)
		}
		if !strings.Contains(string(served), `"tags":[]`) {
			t.Fatalf("served %s, want \"tags\":[]", served)
		}
	}

	tags, err := decodeTemplateRevisionTags([]byte(`["network"]`))
	if err != nil {
		t.Fatalf("decodeTemplateRevisionTags returned error: %v", err)
	}
	if len(tags) != 1 || tags[0] != "network" {
		t.Fatalf("decodeTemplateRevisionTags = %#v, want [network]", tags)
	}
}

func TestEncodeStackTagsStoresNoTagsAsAnEmptyObject(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		tags map[string]string
		want string
	}{
		{name: "nil", tags: nil, want: "{}"},
		{name: "empty", tags: map[string]string{}, want: "{}"},
		{name: "tags", tags: map[string]string{"env": "prod"}, want: `{"env":"prod"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeStackTags(tc.tags)
			if err != nil {
				t.Fatalf("encodeStackTags returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("encodeStackTags(%#v) = %s, want %s", tc.tags, got, tc.want)
			}
		})
	}
}

func TestDecodeStackTagsReadsNullAsNoTags(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"null", "{}"} {
		tags, err := decodeStackTags([]byte(stored))
		if err != nil {
			t.Fatalf("decodeStackTags(%s) returned error: %v", stored, err)
		}
		if tags == nil || len(tags) != 0 {
			t.Fatalf("decodeStackTags(%s) = %#v, want an empty non-nil map", stored, tags)
		}
		served, err := json.Marshal(domain.Stack{Tags: tags})
		if err != nil {
			t.Fatalf("marshal stack: %v", err)
		}
		if !strings.Contains(string(served), `"tags":{}`) {
			t.Fatalf("served %s, want \"tags\":{}", served)
		}
	}

	tags, err := decodeStackTags([]byte(`{"env":"prod"}`))
	if err != nil {
		t.Fatalf("decodeStackTags returned error: %v", err)
	}
	if tags["env"] != "prod" {
		t.Fatalf("decodeStackTags = %#v, want env=prod", tags)
	}
}

func TestEncodeStackCredentialIDsStoresNoneAsAnEmptyArray(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		ids  []domain.CredentialSetID
		want string
	}{
		{name: "nil", ids: nil, want: "[]"},
		{name: "empty", ids: []domain.CredentialSetID{}, want: "[]"},
		{name: "ids", ids: []domain.CredentialSetID{"cred_1", "cred_2"}, want: `["cred_1","cred_2"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeStackCredentialIDs(tc.ids)
			if err != nil {
				t.Fatalf("encodeStackCredentialIDs returned error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("encodeStackCredentialIDs(%#v) = %s, want %s", tc.ids, got, tc.want)
			}
		})
	}
}

func TestDecodeStackCredentialIDsReadsNullAsNone(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"null", "[]"} {
		ids, err := decodeStackCredentialIDs([]byte(stored))
		if err != nil {
			t.Fatalf("decodeStackCredentialIDs(%s) returned error: %v", stored, err)
		}
		if ids == nil || len(ids) != 0 {
			t.Fatalf("decodeStackCredentialIDs(%s) = %#v, want an empty non-nil slice", stored, ids)
		}
		served, err := json.Marshal(domain.Stack{DefaultCredentialIDs: ids})
		if err != nil {
			t.Fatalf("marshal stack: %v", err)
		}
		if !strings.Contains(string(served), `"default_credential_ids":[]`) {
			t.Fatalf("served %s, want \"default_credential_ids\":[]", served)
		}
	}

	ids, err := decodeStackCredentialIDs([]byte(`["cred_1"]`))
	if err != nil {
		t.Fatalf("decodeStackCredentialIDs returned error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "cred_1" {
		t.Fatalf("decodeStackCredentialIDs = %#v, want [cred_1]", ids)
	}
}

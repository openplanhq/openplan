package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vishu42/openplan/internal/domain"
)

// A value comes back only when the request said `sensitive: false`; a request
// that leaves the field out is sensitive (#497). The key's absence is the
// contract, so these tests read raw JSON rather than decode into a struct
// whose zero value would hide a stray empty "value".
func TestCreateCredentialReturnsValueOnlyWhenNotSensitive(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name          string
		body          string
		wantSensitive bool
	}{
		{name: "left out", body: `{"name":"AWS_REGION","value":"us-east-1"}`, wantSensitive: true},
		{name: "sensitive", body: `{"name":"AWS_REGION","value":"us-east-1","sensitive":true}`, wantSensitive: true},
		{name: "not sensitive", body: `{"name":"AWS_REGION","value":"us-east-1","sensitive":false}`, wantSensitive: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, path := range []string{
				"/v1/tenants/tenant_123/stacks/stack_123/credentials",
				"/v1/tenants/tenant_123/stack-templates/stack_template_123/credentials",
			} {
				server, credentials := newCredentialTestServer(t)

				response := httptest.NewRecorder()
				server.ServeHTTP(response, authenticatedRequest(http.MethodPost, path, strings.NewReader(test.body)))
				require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
				assertCredentialValue(t, decodeRawObject(t, response.Body.Bytes()), test.wantSensitive)

				require.Len(t, credentials.created, 1)
				assert.Equal(t, test.wantSensitive, credentials.created[0].Sensitive, path)

				list := httptest.NewRecorder()
				server.ServeHTTP(list, authenticatedRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, list.Code, list.Body.String())
				items := decodeItems[json.RawMessage](t, list)
				require.Len(t, items, 1)
				assertCredentialValue(t, decodeRawObject(t, items[0]), test.wantSensitive)
			}
		})
	}
}

func assertCredentialValue(t *testing.T, credential map[string]json.RawMessage, wantSensitive bool) {
	t.Helper()
	assert.JSONEq(t, strconv.FormatBool(wantSensitive), string(credential["sensitive"]))
	value, present := credential["value"]
	if wantSensitive {
		assert.False(t, present, "a sensitive credential carries no value key, got %s", value)
		return
	}
	assert.JSONEq(t, `"us-east-1"`, string(value))
}

func decodeRawObject(t *testing.T, body []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &object), string(body))
	return object
}

func newCredentialTestServer(t *testing.T) (*Server, *memoryCredentialRepository) {
	t.Helper()
	deps := newAPITestDependencies(t)
	deps.stackTemplates.stackTemplate.StackID = "stack_123"
	service := deps.service()
	credentials := &memoryCredentialRepository{}
	service.Credentials = credentials
	service.CredentialCipher = prefixingCredentialCipher{}
	return NewServer(service, configuredTenantID), credentials
}

// memoryCredentialRepository lists back what was created, so a test sees the
// list read open the same row the create stored.
type memoryCredentialRepository struct {
	mu      sync.Mutex
	created []domain.CredentialSet
}

func (repository *memoryCredentialRepository) CreateCredential(_ context.Context, credential domain.CredentialSet) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.created = append(repository.created, credential)
	return nil
}

func (repository *memoryCredentialRepository) ListCredentials(context.Context, domain.TenantID, domain.CredentialScope, string) ([]domain.CredentialSet, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return append([]domain.CredentialSet(nil), repository.created...), nil
}

func (*memoryCredentialRepository) DeleteCredential(context.Context, domain.TenantID, domain.CredentialSetID) error {
	return nil
}

func (*memoryCredentialRepository) ListCredentialsForStackTemplate(context.Context, domain.TenantID, domain.StackTemplateID) ([]domain.CredentialSet, error) {
	return nil, nil
}

type prefixingCredentialCipher struct{}

func (prefixingCredentialCipher) Encrypt(value string) (string, error) {
	return "sealed:" + value, nil
}

func (prefixingCredentialCipher) Decrypt(value string) (string, error) {
	return strings.TrimPrefix(value, "sealed:"), nil
}

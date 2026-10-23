package activities

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/vishu42/openplan/internal/domain"
)

type testCredentialReader struct {
	credentials []domain.CredentialSet
}

// TestRedactingWriterRemovesCredentialValues verifies that command output never exposes exact secret values.
func TestRedactingWriterRemovesCredentialValues(t *testing.T) {
	var output bytes.Buffer
	writer := newRedactingWriter(nopWriteCloser{Buffer: &output}, []string{"secret"})
	if _, err := writer.Write([]byte("key=secret")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if output.String() != "key=******" || bytes.Contains(output.Bytes(), []byte("secret")) {
		t.Fatalf("output = %q", output.String())
	}
}

type nopWriteCloser struct{ *bytes.Buffer }

// Close implements io.WriteCloser for the in-memory test buffer.
func (writer nopWriteCloser) Close() error { return nil }

// ListCredentialsForStackTemplate returns fixture credentials to the resolver under test.
func (reader testCredentialReader) ListCredentialsForStackTemplate(context.Context, domain.TenantID, domain.StackTemplateID) ([]domain.CredentialSet, error) {
	return reader.credentials, nil
}

type testCredentialDecryptor struct{}

// Decrypt provides deterministic plaintext for resolver tests without using real key material.
func (testCredentialDecryptor) Decrypt(value string) (string, error) {
	return "decrypted:" + value, nil
}

// TestResolveCredentialEnvironmentTemplateOverridesStack verifies inheritance and template precedence
// regardless of the order the store returns credentials in.
func TestResolveCredentialEnvironmentTemplateOverridesStack(t *testing.T) {
	stackRegion := domain.CredentialSet{StackID: "stack_123", Name: "CLOUD_REGION", Ciphertext: "stack-region"}
	stackToken := domain.CredentialSet{StackID: "stack_123", Name: "CLOUD_TOKEN", Ciphertext: "stack-token"}
	templateToken := domain.CredentialSet{StackTemplateID: "template_123", Name: "CLOUD_TOKEN", Ciphertext: "template-token"}

	for name, credentials := range map[string][]domain.CredentialSet{
		"template last":  {stackRegion, stackToken, templateToken},
		"template first": {templateToken, stackRegion, stackToken},
	} {
		t.Run(name, func(t *testing.T) {
			resolved, err := resolveCredentialEnvironment(context.Background(), testCredentialReader{credentials: credentials}, testCredentialDecryptor{}, "tenant_123", "template_123")
			if err != nil {
				t.Fatalf("resolveCredentialEnvironment returned error: %v", err)
			}
			if resolved.Environment["CLOUD_REGION"] != "decrypted:stack-region" {
				t.Fatalf("region = %q", resolved.Environment["CLOUD_REGION"])
			}
			if resolved.Environment["CLOUD_TOKEN"] != "decrypted:template-token" {
				t.Fatalf("token = %q", resolved.Environment["CLOUD_TOKEN"])
			}
		})
	}
}

// The entry that wins a name decides whether its value is plain: a template
// override replaces the stack entry's sensitivity along with its value, in
// either direction.
func TestResolveCredentialEnvironmentWinningEntryDecidesPlain(t *testing.T) {
	credentials := []domain.CredentialSet{
		{StackID: "stack_123", Name: "CLOUD_REGION", Ciphertext: "stack-region", Sensitive: false},
		{StackID: "stack_123", Name: "CLOUD_TOKEN", Ciphertext: "stack-token", Sensitive: false},
		{StackID: "stack_123", Name: "CLOUD_ZONE", Ciphertext: "stack-zone", Sensitive: true},
		{StackTemplateID: "template_123", Name: "CLOUD_TOKEN", Ciphertext: "template-token", Sensitive: true},
		{StackTemplateID: "template_123", Name: "CLOUD_ZONE", Ciphertext: "template-zone", Sensitive: false},
	}
	resolved, err := resolveCredentialEnvironment(context.Background(), testCredentialReader{credentials: credentials}, testCredentialDecryptor{}, "tenant_123", "template_123")
	if err != nil {
		t.Fatalf("resolveCredentialEnvironment returned error: %v", err)
	}
	if want := []string{"CLOUD_REGION", "CLOUD_ZONE"}; !slices.Equal(resolved.Plain, want) {
		t.Fatalf("plain = %v, want %v", resolved.Plain, want)
	}
}

func TestRedactedValuesSkipOnlyPlainNames(t *testing.T) {
	credentials := domain.RunCredentials{
		Environment: map[string]string{"AWS_REGION": "us-east-1", "AWS_SECRET_ACCESS_KEY": "s3cr3t", "EMPTY": ""},
		Plain:       []string{"AWS_REGION"},
	}
	if got, want := redactedValues(credentials), []string{"s3cr3t"}; !slices.Equal(got, want) {
		t.Fatalf("redacted = %v, want %v", got, want)
	}
}

// Fail safe: a payload that names no plain entries, whether by omission or
// by mistake, redacts every value, as before sensitivity existed.
func TestRedactedValuesWithoutPlainNamesRedactEverything(t *testing.T) {
	credentials := domain.RunCredentials{Environment: map[string]string{"AWS_REGION": "us-east-1", "AWS_SECRET_ACCESS_KEY": "s3cr3t"}}
	if got, want := redactedValues(credentials), []string{"us-east-1", "s3cr3t"}; !slices.Equal(got, want) {
		t.Fatalf("redacted = %v, want %v", got, want)
	}
}

// Redaction is by value, so a sensitive value is hidden even where it also
// appears as the value of a plain entry.
func TestRedactedValuesKeepASensitiveValueSharedWithAPlainOne(t *testing.T) {
	credentials := domain.RunCredentials{
		Environment: map[string]string{"BUCKET": "shared", "TOKEN": "shared"},
		Plain:       []string{"BUCKET"},
	}
	if got, want := redactedValues(credentials), []string{"shared"}; !slices.Equal(got, want) {
		t.Fatalf("redacted = %v, want %v", got, want)
	}
}

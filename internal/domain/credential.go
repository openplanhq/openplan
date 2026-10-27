package domain

// Encrypted credentials and the resources that own them.

import (
	"time"
)

// CredentialScope identifies the resource that owns one encrypted credential.
type CredentialScope string

const (
	CredentialScopeStack         CredentialScope = "stack"
	CredentialScopeStackTemplate CredentialScope = "stack_template"
)

// Credential is one environment entry: a variable name and its encrypted
// value, owned by a stack or overridden by one of its templates.
type Credential struct {
	ID              CredentialID    // Stable identifier used by API deletion and audit references.
	TenantID        TenantID        // Tenant that owns the encrypted credential.
	StackID         StackID         // Stack owner; empty when StackTemplateID is populated.
	StackTemplateID StackTemplateID // Optional template owner for template-specific overrides.
	Name            string          // Provider environment variable name.
	Ciphertext      string          // Encrypted value; plaintext must never be persisted here.
	Sensitive       bool            // A sensitive value is never shown back and is redacted from run logs.
	CreatedAt       time.Time       // Creation timestamp used for metadata display and auditing.
}

// RunCredentials is what the control plane seals to a run's key: the resolved
// environment and the names in it whose values are not sensitive. The
// executor redacts every value whose name is not in Plain, so a payload that
// lists no names redacts everything.
type RunCredentials struct {
	Environment map[string]string
	Plain       []string
}

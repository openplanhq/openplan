package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
	"github.com/vishu42/openplan/internal/encryption"
	"github.com/vishu42/openplan/internal/queue"
)

var ErrNotFound = errors.New("postgres: not found")

type Store struct {
	pool *pgxpool.Pool
	// The two ciphers stay separate: SESSION_ENCRYPTION_KEY is required
	// configuration, while CREDENTIAL_ENCRYPTION_KEY is optional, and a session
	// row must not fail to write because no one configured customer credential
	// encryption.
	credentialCipher storeCipher
	sessionCipher    storeCipher
	queueSpecs       *queue.SpecRegistry
}

// storeCipher is one of the store's ciphers. Until one is configured it
// refuses with its own unavailable error, so the failure names which key is
// missing.
type storeCipher struct {
	cipher      *encryption.Cipher
	unavailable error
}

func (cipher storeCipher) Encrypt(value string) (string, error) {
	if cipher.cipher == nil {
		return "", cipher.unavailable
	}
	return cipher.cipher.Encrypt(value)
}

func (cipher storeCipher) Decrypt(value string) (string, error) {
	if cipher.cipher == nil {
		return "", cipher.unavailable
	}
	return cipher.cipher.Decrypt(value)
}

// Option configures a Store at construction.
type Option func(*Store)

// WithQueueSpecs lets the store resolve a queue.Request into an resource key
// and mode. Specs carry no dependencies, so a producer-only binary registers
// kinds without building the handlers that deliver them. Stores that never
// enqueue can omit it.
func WithQueueSpecs(specs *queue.SpecRegistry) Option {
	return func(store *Store) { store.queueSpecs = specs }
}

// NewStore creates a repository store. The credential cipher is injected by the
// caller from internal/config; the store reads no environment of its own.
func NewStore(pool *pgxpool.Pool, options ...Option) *Store {
	store := &Store{
		pool:             pool,
		credentialCipher: storeCipher{unavailable: app.ErrCredentialEncryptionUnavailable},
		sessionCipher:    storeCipher{unavailable: authentication.ErrSessionEncryptionUnavailable},
	}
	for _, option := range options {
		option(store)
	}
	return store
}

// WithCredentialCipher supplies the process-wide credential encryption cipher.
// Without it, Encrypt and Decrypt return app.ErrCredentialEncryptionUnavailable.
func WithCredentialCipher(cipher *encryption.Cipher) Option {
	return func(store *Store) { store.credentialCipher.cipher = cipher }
}

// WithSessionCipher supplies the process-wide session encryption cipher, used
// only for session rows. Without it, writing or reading a session returns
// authentication.ErrSessionEncryptionUnavailable.
func WithSessionCipher(cipher *encryption.Cipher) Option {
	return func(store *Store) { store.sessionCipher.cipher = cipher }
}

// Encrypt protects a credential value with the configured application cipher before persistence.
func (store *Store) Encrypt(value string) (string, error) {
	return store.credentialCipher.Encrypt(value)
}

// Decrypt opens stored credential ciphertext so the control plane can seal it
// to a run's executor key.
func (store *Store) Decrypt(value string) (string, error) {
	return store.credentialCipher.Decrypt(value)
}

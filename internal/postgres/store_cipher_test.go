package postgres

import (
	"errors"
	"testing"

	"github.com/vishu42/openplan/internal/app"
	"github.com/vishu42/openplan/internal/authentication"
)

// A store built without a key refuses with the error naming that key, never
// with an empty ciphertext and no error.
func TestStoreCiphersRefuseUntilConfigured(t *testing.T) {
	t.Parallel()

	store := NewStore(nil)
	for name, tc := range map[string]struct {
		use  func(string) (string, error)
		want error
	}{
		"credential encrypt": {store.Encrypt, app.ErrCredentialEncryptionUnavailable},
		"credential decrypt": {store.Decrypt, app.ErrCredentialEncryptionUnavailable},
		"session encrypt":    {store.sessionCipher.Encrypt, authentication.ErrSessionEncryptionUnavailable},
		"session decrypt":    {store.sessionCipher.Decrypt, authentication.ErrSessionEncryptionUnavailable},
	} {
		if _, err := tc.use("value"); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

package config

import (
	"bytes"
	neturl "net/url"
	"slices"

	"github.com/vishu42/openplan/internal/encryption"
)

// publishedEncryptionKeys are the encryption keys this repository ships, in
// .env.example and both compose files, so that a checkout or the evaluation
// stack runs without setup. Anyone can read them, so in production they seal
// nothing.
var publishedEncryptionKeys = []string{
	"717cb4d0fd1db07a30442806c2987599580f6d7c6e63b9bddf509bc183a086d3", // CREDENTIAL_ENCRYPTION_KEY
	"b6f1d2c4a8e0937516243b8c5d7e9f0a1b2c3d4e5f60718293a4b5c6d7e8f9a0", // SESSION_ENCRYPTION_KEY
}

// refusePublishedKey fails when key, read from the variable name, is one of
// publishedEncryptionKeys. Keys compare as bytes, so re-encoding a published
// key does not get it past.
func refusePublishedKey(name string, key string) error {
	raw, err := encryption.DecodeKey(key)
	// A key that does not decode is not a published one; the variable's own
	// validation reports it.
	if err == nil && isPublishedKey(raw) {
		return authConfigError("%s is an example key published in the openplan repository and cannot be used in production; generate one with `openssl rand -hex 32`", name)
	}
	return nil
}

func isPublishedKey(raw []byte) bool {
	for _, published := range publishedEncryptionKeys {
		if publishedRaw, err := encryption.DecodeKey(published); err == nil && bytes.Equal(raw, publishedRaw) {
			return true
		}
	}
	return false
}

// publishedValkeyPasswords are the Valkey passwords both compose files and
// .env.example default to. Like the encryption keys, anyone can read them.
var publishedValkeyPasswords = []string{"api-local-only", "executor-local-only"}

// refusePublishedValkeyPassword fails when the URL in the variable name signs
// in with one of publishedValkeyPasswords. Either is refused: the executor's
// on the api would be as public as the api's own. The error never quotes the
// URL, which carries the password.
func refusePublishedValkeyPassword(name, rawURL string) error {
	parsed, err := neturl.Parse(rawURL)
	// A URL that does not parse has no password to judge; dialing it
	// reports the problem.
	if err == nil && isPublishedValkeyPassword(parsed.User) {
		return authConfigError("%s signs in with an example password published in the openplan repository and cannot be used in production; set the Valkey user's password to a fresh one, such as `openssl rand -hex 32`", name)
	}
	return nil
}

func isPublishedValkeyPassword(user *neturl.Userinfo) bool {
	if user == nil {
		return false
	}
	password, ok := user.Password()
	return ok && slices.Contains(publishedValkeyPasswords, password)
}

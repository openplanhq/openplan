package config

import (
	"bytes"

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
func refusePublishedKey(name string, key Secret) error {
	raw, err := encryption.DecodeKey(key.Value())
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

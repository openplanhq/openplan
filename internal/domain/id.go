package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// RandomID mints one identifier: the kind's prefix followed by 16 bytes of
// CSPRNG output in hex, so the shape of an openplan identifier is defined in
// exactly one place.
//
// A failed read falls back to a timestamp rather than failing the call: the
// callers minting these IDs are mid-command with nowhere to report one.
func RandomID[ID ~string](prefix string) ID {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return ID(fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano()))
	}
	return ID(prefix + "_" + hex.EncodeToString(bytes[:]))
}

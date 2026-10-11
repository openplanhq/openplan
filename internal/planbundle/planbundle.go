// Package planbundle packs a run's saved plan for the trip between its plan
// phase and its apply phase, which usually run on different executors.
//
// A bundle is the plan file plus the dependency lock file, so the apply phase
// installs the provider versions the plan was made with: tofu refuses a saved
// plan whose providers changed. It is encrypted before it leaves the executor,
// because a plan file holds variable values and sensitive attributes in
// plaintext, and the key it is encrypted with belongs to the run and never
// crosses Temporal in the clear.
package planbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/vishu42/openplan/internal/runner"
)

// LockFileName is tofu's dependency lock file in the root module directory.
const LockFileName = ".terraform.lock.hcl"

// KeySize is the length of a plan key: AES-256.
const KeySize = 32

// maxFileSize bounds each file unpacked from a bundle. Plan files for large
// configurations run to tens of megabytes; this is well past that and still
// small enough that a hostile bundle cannot fill the executor's disk.
const maxFileSize = 512 << 20

// Files are the files a bundle carries, from the root module directory: the
// saved plan and the dependency lock file. Pack takes nothing else and Unpack
// returns nothing else. The caller reads and writes them; on the executor,
// through the session that owns the directory.
var Files = []string{runner.PlanFileName, LockFileName}

// NewKey returns a fresh random plan key.
func NewKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate plan key: %w", err)
	}
	return key, nil
}

// Pack bundles files, by name. The plan must be there; a module with no
// providers has no lock file.
func Pack(files map[string][]byte) ([]byte, error) {
	if _, ok := files[runner.PlanFileName]; !ok {
		return nil, errors.New("no saved plan to pack")
	}
	for name := range files {
		if !isBundled(name) {
			return nil, fmt.Errorf("%q does not belong in a plan bundle", name)
		}
	}
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(gz)
	for _, name := range Files {
		content, ok := files[name]
		if !ok {
			continue
		}
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content))}); err != nil {
			return nil, fmt.Errorf("pack %s: %w", name, err)
		}
		if _, err := archive.Write(content); err != nil {
			return nil, fmt.Errorf("pack %s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("close plan bundle: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("compress plan bundle: %w", err)
	}
	return buffer.Bytes(), nil
}

// Unpack returns a bundle's files, by name, for the caller to write back into
// the root module directory, replacing any already there: the lock file in
// the fetched source, if it has one, gives way to the one the plan was made
// with.
func Unpack(bundle []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		return nil, fmt.Errorf("open plan bundle: %w", err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read plan bundle: %w", err)
		}
		if !isBundled(header.Name) || header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("plan bundle holds unexpected entry %q", header.Name)
		}
		if header.Size > maxFileSize {
			return nil, fmt.Errorf("plan bundle entry %q is too large", header.Name)
		}
		content, err := io.ReadAll(io.LimitReader(archive, maxFileSize))
		if err != nil {
			return nil, fmt.Errorf("read %s from plan bundle: %w", header.Name, err)
		}
		files[header.Name] = content
	}
	if _, ok := files[runner.PlanFileName]; !ok {
		return nil, errors.New("plan bundle holds no plan file")
	}
	return files, nil
}

func isBundled(name string) bool {
	return slices.Contains(Files, name)
}

// Seal encrypts a bundle with key. context is bound in as additional data, so
// a bundle only opens for the run it was sealed for, even under the right key.
// The nonce is random and prepended.
func Seal(key []byte, bundle []byte, context string) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate plan nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, bundle, []byte(context)), nil
}

// Open decrypts a sealed bundle, failing if it was altered, sealed under
// another key, or sealed for another context.
func Open(key []byte, sealed []byte, context string) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("sealed plan is too short")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	bundle, err := aead.Open(nil, nonce, ciphertext, []byte(context))
	if err != nil {
		return nil, fmt.Errorf("open sealed plan: %w", err)
	}
	return bundle, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("plan key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("plan cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("plan cipher: %w", err)
	}
	return aead, nil
}

package planbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"reflect"
	"testing"
)

func TestBundleRoundTripsThePlanAndLockFile(t *testing.T) {
	t.Parallel()

	files := map[string][]byte{"tfplan": []byte("plan bytes"), ".terraform.lock.hcl": []byte("locked providers")}
	bundle, err := Pack(files)
	if err != nil {
		t.Fatalf("Pack returned error: %v", err)
	}
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey returned error: %v", err)
	}
	sealed, err := Seal(key, bundle, "tenant_123/run_123")
	if err != nil {
		t.Fatalf("Seal returned error: %v", err)
	}
	if bytes.Contains(sealed, []byte("plan bytes")) {
		t.Fatal("sealed bundle contains the plan in plaintext")
	}

	opened, err := Open(key, sealed, "tenant_123/run_123")
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	unpacked, err := Unpack(opened)
	if err != nil {
		t.Fatalf("Unpack returned error: %v", err)
	}
	if !reflect.DeepEqual(unpacked, files) {
		t.Fatalf("Unpack = %q, want %q", unpacked, files)
	}
}

// A module with no providers has no lock file; its plan still bundles.
func TestPackWithoutALockFile(t *testing.T) {
	t.Parallel()

	bundle, err := Pack(map[string][]byte{"tfplan": []byte("plan bytes")})
	if err != nil {
		t.Fatalf("Pack returned error: %v", err)
	}
	unpacked, err := Unpack(bundle)
	if err != nil {
		t.Fatalf("Unpack returned error: %v", err)
	}
	if !reflect.DeepEqual(unpacked, map[string][]byte{"tfplan": []byte("plan bytes")}) {
		t.Fatalf("Unpack = %q", unpacked)
	}
}

func TestPackRequiresThePlanAndNothingElse(t *testing.T) {
	t.Parallel()

	if _, err := Pack(map[string][]byte{".terraform.lock.hcl": []byte("lock")}); err == nil {
		t.Fatal("Pack returned nil error without a plan file")
	}
	if _, err := Pack(map[string][]byte{"tfplan": []byte("plan"), "main.tf": []byte("x")}); err == nil {
		t.Fatal("Pack bundled main.tf")
	}
}

// A bundle only opens for the run it was sealed for, and only unaltered.
func TestOpenRejectsAnotherRunTamperingAndTheWrongKey(t *testing.T) {
	t.Parallel()

	key, _ := NewKey()
	other, _ := NewKey()
	sealed, err := Seal(key, []byte("bundle"), "tenant_123/run_123")
	if err != nil {
		t.Fatalf("Seal returned error: %v", err)
	}

	if _, err := Open(key, sealed, "tenant_123/run_456"); err == nil {
		t.Fatal("Open accepted a bundle sealed for another run")
	}
	if _, err := Open(other, sealed, "tenant_123/run_123"); err == nil {
		t.Fatal("Open accepted the wrong key")
	}
	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := Open(key, tampered, "tenant_123/run_123"); err == nil {
		t.Fatal("Open accepted a tampered bundle")
	}
}

// Unpack returns only the two files a bundle may carry, so a bundle cannot be
// used to drop anything else, anywhere else, in the session's workspace.
func TestUnpackRefusesAnythingButThePlanAndLockFile(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"main.tf", "../escape", "/etc/passwd"} {
		var buffer bytes.Buffer
		gz := gzip.NewWriter(&buffer)
		archive := tar.NewWriter(gz)
		_ = archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: 1})
		_, _ = archive.Write([]byte("x"))
		_ = archive.Close()
		_ = gz.Close()

		if _, err := Unpack(buffer.Bytes()); err == nil {
			t.Fatalf("Unpack accepted entry %q", name)
		}
	}
}

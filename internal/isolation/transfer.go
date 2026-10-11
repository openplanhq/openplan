package isolation

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

// maxFileSize bounds each file that crosses between the executor and the
// helper, and maxTransferSize all of them together. maxFileSize is
// planbundle's own bound on a bundled file, so a plan too large to bundle
// fails when it is read, before anyone approves it.
const (
	maxFileSize     = 512 << 20
	maxTransferSize = 1 << 30
)

// encodeFiles carries files between the executor and the helper as an
// uncompressed tar of regular files, in name order.
func encodeFiles(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := tar.NewWriter(&buffer)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if err := checkName(name); err != nil {
			return nil, err
		}
		content := files[name]
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			return nil, fmt.Errorf("encode %s: %w", name, err)
		}
		if _, err := archive.Write(content); err != nil {
			return nil, fmt.Errorf("encode %s: %w", name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("encode files: %w", err)
	}
	return buffer.Bytes(), nil
}

// decodeFiles reads what encodeFiles wrote, as it streams in, so each file is
// held once and no more than maxTransferSize of them in all. Each side
// decodes what the other sent, and the helper's side may have been tampered
// with, so it accepts only regular files with plain names, each once.
func decodeFiles(r io.Reader) (map[string][]byte, error) {
	archive := tar.NewReader(r)
	files := map[string][]byte{}
	var total int64
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return files, nil
		}
		if err != nil {
			return nil, fmt.Errorf("decode files: %w", err)
		}
		if err := checkName(header.Name); err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%q is not a regular file", header.Name)
		}
		if _, seen := files[header.Name]; seen {
			return nil, fmt.Errorf("%q appears twice", header.Name)
		}
		// Checked before reading: the tar reader yields exactly Size bytes.
		total += header.Size
		if header.Size > maxFileSize || total > maxTransferSize {
			return nil, fmt.Errorf("%q takes the files past their size limit", header.Name)
		}
		content, err := io.ReadAll(archive)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", header.Name, err)
		}
		files[header.Name] = content
	}
}

// checkName accepts only a single path component, so a name can never reach
// outside the directory it is read from or written to.
func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("file name %q is not a single path component", name)
	}
	return nil
}

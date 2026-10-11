package isolation

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

// maxTransferSize bounds the files that cross between the executor and the
// helper, in either direction: comfortably above a saved plan and its lock
// file, which planbundle caps at 512 MiB each.
const maxTransferSize = 1 << 30

// encodeFiles carries files between the executor and the helper as an
// uncompressed tar of regular files, in name order.
func encodeFiles(files map[string][]byte) ([]byte, error) {
	var buffer bytes.Buffer
	archive := tar.NewWriter(&buffer)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
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

// decodeFiles reads what encodeFiles wrote. Each side decodes what the other
// sent, and the helper's side may have been tampered with, so it accepts
// only regular files with plain names, each once.
func decodeFiles(data []byte) (map[string][]byte, error) {
	archive := tar.NewReader(bytes.NewReader(data))
	files := map[string][]byte{}
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
		// data is already bounded by maxTransferSize, so ReadAll is too.
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

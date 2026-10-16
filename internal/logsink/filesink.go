package logsink

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileSink struct {
	dir string
}

// NewFileSink returns a local filesystem-backed sink that writes <dir>/<file>.
// dir must exist. On the executor it is a run's logs/ spool: root's, mounted
// read-only into UploadLog's sandbox, which reads it as the sandbox user.
func NewFileSink(dir string) FileSink {
	return FileSink{dir: dir}
}

// LogFileExtension ends every log file name. A log's name, the one it is
// stored and read under, is its file name without it.
const LogFileExtension = ".log"

// Open opens the append-only log file with this name, such as plan-init.log.
func (sink FileSink) Open(fileName string) (io.WriteCloser, error) {
	if strings.TrimSpace(sink.dir) == "" {
		return nil, fmt.Errorf("log directory is required")
	}
	if !safePathComponent(fileName) {
		return nil, fmt.Errorf("log file name must be a safe path component")
	}

	path := filepath.Join(sink.dir, fileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // UploadLog's sandbox reads it as the sandbox user
	if err != nil {
		return nil, fmt.Errorf("open phase log: %w", err)
	}
	return file, nil
}

// FileName is the spool file for the log named name.
func FileName(name string) string {
	return name + LogFileExtension
}

// safePathComponent reports whether component can be used as one path segment.
func safePathComponent(component string) bool {
	if strings.TrimSpace(component) == "" {
		return false
	}
	if filepath.IsAbs(component) {
		return false
	}
	cleaned := filepath.Clean(component)
	return cleaned == component && component != "." && component != ".." && filepath.Base(component) == component
}

//go:build !linux

package main

// setNotDumpable is a no-op off Linux. The dumpable flag is Linux's, and the
// executor runs tenant Terraform only in its Linux image; other platforms are
// for development.
func setNotDumpable() error { return nil }

package runhelper

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vishu42/openplan/internal/planbundle"
)

// Subcommand is the hidden first argument that turns the executor binary into
// the helper.
const Subcommand = "helper"

// maxBundleSize bounds what unpack reads from the executor: comfortably above
// a compressed bundle of planbundle's largest files.
const maxBundleSize = 1 << 30

// Main runs one helper operation and returns the process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := dispatch(args, stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "helper: %v\n", err)
		return 1
	}
	return 0
}

func dispatch(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("no operation")
	}
	op, rest := args[0], args[1:]
	switch {
	case op == "kill" && len(rest) == 0:
		return killAll()
	case op == "reclaim" && len(rest) <= 1:
		if err := killAll(); err != nil {
			return err
		}
		if err := ClearScratch(uint32(os.Getuid()), ScratchDirs); err != nil { //nolint:gosec // the kernel stores uids in 32 bits
			return err
		}
		if len(rest) == 1 {
			return EmptyDir(rest[0])
		}
		return nil
	case op == "pack" && len(rest) == 1:
		bundle, err := planbundle.Pack(rest[0])
		if err != nil {
			return err
		}
		_, err = stdout.Write(bundle)
		return err
	case op == "unpack" && len(rest) == 1:
		bundle, err := io.ReadAll(io.LimitReader(stdin, maxBundleSize+1))
		if err != nil {
			return err
		}
		if len(bundle) > maxBundleSize {
			return errors.New("plan bundle is too large")
		}
		return planbundle.Unpack(bundle, rest[0])
	case op == "check-root" && len(rest) == 1:
		return CheckRoot(rest[0])
	}
	return fmt.Errorf("unknown operation %q", strings.Join(args, " "))
}

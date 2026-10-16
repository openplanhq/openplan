package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// guestPath is PATH inside the guest root.
const guestPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// forwardedVariables are the executor's own settings a sandbox's commands
// need: corporate egress, and the CA it trusts, whose paths trustMounts
// brings into the sandbox.
var forwardedVariables = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	"http_proxy", "https_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR",
}

// baseEnvironment is every sandbox's environment, built from scratch. The
// executor's own environment holds the storage keys and the Temporal settings,
// so nothing of it passes but forwardedVariables. HOME and TMPDIR are the
// sandbox's private /tmp. An activity's credentials are not here: they arrive
// over stdin and go only to the commands that need them.
func baseEnvironment(lookup func(string) (string, bool)) []string {
	env := []string{"PATH=" + guestPath, "HOME=/tmp", "TMPDIR=/tmp", "TF_IN_AUTOMATION=1"}
	for _, name := range forwardedVariables {
		if value, ok := lookup(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// trustMounts are the CA file and directories the forwarded SSL_CERT_FILE and
// SSL_CERT_DIR name, each to be mounted read-only into every sandbox at that
// path. They are the executor's files, which the guest root doesn't have:
// forwarded alone, the variables would name nothing, and a sandbox would
// quietly trust only the guest's own bundle, failing every fetch through a
// TLS-inspecting proxy. A path that is relative or missing fails the
// executor's start rather than every sandbox's create.
func trustMounts(lookup func(string) (string, bool)) ([]Mount, error) {
	type named struct{ variable, path string }
	var paths []named
	// Empty is unset, as Go's crypto/x509 reads them.
	if file, _ := lookup("SSL_CERT_FILE"); file != "" {
		paths = append(paths, named{"SSL_CERT_FILE", file})
	}
	if dirs, _ := lookup("SSL_CERT_DIR"); dirs != "" {
		// A colon-separated list, to Go and OpenSSL alike.
		for dir := range strings.SplitSeq(dirs, ":") {
			if dir != "" {
				paths = append(paths, named{"SSL_CERT_DIR", dir})
			}
		}
	}
	mounts := make([]Mount, 0, len(paths))
	for _, p := range paths {
		if !filepath.IsAbs(p.path) {
			return nil, fmt.Errorf("%s names %q, which isn't an absolute path a sandbox can be given", p.variable, p.path)
		}
		if _, err := os.Stat(p.path); err != nil {
			return nil, fmt.Errorf("%s names %s, which a sandbox can't be given: %w", p.variable, p.path, err)
		}
		mounts = append(mounts, Mount{Path: p.path, ReadOnly: true})
	}
	return mounts, nil
}

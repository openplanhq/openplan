package sandbox

// guestPath is PATH inside the guest root.
const guestPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// forwardedVariables are the executor's own settings a sandbox's commands
// need: corporate egress, and the CA it trusts.
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

// Package sandbox runs each executor activity's body in a gVisor sandbox of
// its own (#245). The worker side, Runtime, starts the sandbox and relays; the
// sandbox side, Serve, is this same binary run inside it as
// "openplan-executor activity <name>". They speak over the sandbox's stdin
// and stdout only: the request in, output and then one result out.
package sandbox

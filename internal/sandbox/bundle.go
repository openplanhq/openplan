package sandbox

import specs "github.com/opencontainers/runtime-spec/specs-go"

// Limits bound one sandbox. They keep one run from starving the others on
// the executor, so a sandbox that cannot get them does not start.
type Limits struct {
	MemoryMiB int64
	CPUs      int64
	Pids      int64
}

// guestBinary is the executor binary inside the guest root. Dockerfile.executor
// copies the same build there, so the body that runs is this version.
const guestBinary = "/usr/local/bin/openplan-executor"

const tmpfs = "tmpfs"

func ociSpec(job Job, guestRoot string, env []string, cgroupsPath string, limits Limits) *specs.Spec {
	memory := limits.MemoryMiB << 20
	period := uint64(100_000)
	quota := limits.CPUs * int64(period)
	mounts := []specs.Mount{
		{Destination: "/proc", Type: "proc", Source: "proc"},
		{Destination: "/dev", Type: tmpfs, Source: tmpfs, Options: []string{"nosuid", "mode=755"}},
		// HOME and TMPDIR: private to this sandbox, gone with it.
		{Destination: "/tmp", Type: tmpfs, Source: tmpfs, Options: []string{"nosuid", "nodev", "mode=1777"}},
	}
	for _, m := range job.Mounts {
		options := []string{"rbind", "rw"}
		if m.ReadOnly {
			options = []string{"rbind", "ro"}
		}
		mounts = append(mounts, specs.Mount{Destination: m.Path, Type: "bind", Source: m.Path, Options: options})
	}
	return &specs.Spec{
		Version: specs.Version,
		Process: &specs.Process{
			Args: []string{guestBinary, ActivitySubcommand, job.Activity},
			Env:  env,
			Cwd:  "/",
			User: specs.User{UID: SandboxUID, GID: SandboxUID, AdditionalGids: job.Groups},
			// No capabilities, and no way to gain any.
			Capabilities:    &specs.LinuxCapabilities{},
			NoNewPrivileges: true,
		},
		Root:     &specs.Root{Path: guestRoot, Readonly: true},
		Hostname: "sandbox",
		Mounts:   mounts,
		Linux: &specs.Linux{
			// No network namespace: the sandbox shares the executor's network,
			// through gVisor's own stack (network isolation comes later).
			Namespaces: []specs.LinuxNamespace{
				{Type: specs.PIDNamespace}, {Type: specs.IPCNamespace},
				{Type: specs.UTSNamespace}, {Type: specs.MountNamespace},
			},
			CgroupsPath: cgroupsPath,
			Resources: &specs.LinuxResources{
				// Swap is memory plus swap here, so equal to the limit means
				// none: otherwise a sandbox pages past its limit onto the
				// host's swap, slowing every run instead of failing its own.
				Memory: &specs.LinuxMemory{Limit: &memory, Swap: &memory},
				Pids:   &specs.LinuxPids{Limit: limits.Pids},
				CPU:    &specs.LinuxCPU{Quota: &quota, Period: &period},
			},
		},
	}
}

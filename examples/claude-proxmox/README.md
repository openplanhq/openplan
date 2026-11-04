# claude-proxmox

A VM on Proxmox where Claude Code runs a maintainer's own setup, with a Remote
Control session for each repository, so agents keep working on openplan while
the laptop sleeps. A homelab example, not a supported topology. Two openplan
templates like the Proxmox cluster's (#548), applied by the same manager
(#560):

| Root path | What it owns | Changes |
|---|---|---|
| [`examples/claude-proxmox/vm`](vm/README.md) | the VM, its first-boot setup and the key `sessions/` connects with | rarely: any change is a rebuild |
| [`examples/claude-proxmox/sessions`](sessions/README.md) | each repository's clone and its Remote Control service, on the running VM | whenever a repository is added or removed |

`sessions/` never replaces or edits anything `vm/` owns, so adding a
repository never costs the VM or the work on it.

## Order

1. Install `vm/`. Its setup takes a few minutes after the apply ends.
2. On the VM, once (`ssh debian@<address>`):
   - `claude auth login`, the full claude.ai login Remote Control needs;
   - trust each repository `sessions/` will clone, by running `claude` in it
     once. A parent folder's trust doesn't count, and a service in a folder
     that isn't trusted yet retries every 15 seconds until it is.
3. Install `sessions/` with `vm_workspace` set to the workspace in `vm/`'s run
   log.

Destroy in reverse: `sessions/`, then `vm/`. `sessions/` reaches the VM over
SSH with `vm/`'s key, so it can't stop its services once the VM is gone, and a
rebuilt VM has a new key. To rebuild the VM, destroy `sessions/` first and
install it again after.

## Credentials

Both keep their state in one bucket, and `sessions/` reads `vm/`'s from it, so
both take the state credentials in [`vm/`'s list](vm/README.md#credentials),
with the same values: `TF_CLI_ARGS_init` and the three `AWS_*`. `vm/` takes
the Proxmox and token credentials besides.

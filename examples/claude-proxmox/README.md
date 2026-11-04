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
2. Install `sessions/` with `vm_workspace` set to the workspace in `vm/`'s run
   log. It clones each repository, marks it trusted for Claude Code and starts
   its service.
3. On the VM (`ssh debian@<address>`), run `claude auth login`: the full
   claude.ai login Remote Control needs. Until then each service exits ("You
   must be logged in") and retries every 15 seconds, so it connects on the
   next try after. Steps 2 and 3 can go in either order.

   It stays a step by hand, because Claude Code can't sign in unattended. The
   login prints a URL that you open in a browser elsewhere, and you paste the
   code it shows back into the VM. A `claude setup-token` token can only make
   model requests, not Remote Control sessions. Copying a login from another
   machine isn't documented, and one machine's refresh can revoke the other's
   copy. `claude auth status` shows when the login expires (`loginExpiresAt`,
   a month out on first sign-in); if it lapses, the sessions stop until you
   run this again.

Destroy in reverse: `sessions/`, then `vm/`. A rebuilt VM has a new key, so
the next apply of `sessions/` puts its unit, trust and services back; it needs
step 3 again.

## Credentials

Both keep their state in one bucket, and `sessions/` reads `vm/`'s from it, so
both take the state credentials in [`vm/`'s list](vm/README.md#credentials),
with the same values: `TF_CLI_ARGS_init` and the three `AWS_*`. `vm/` takes
the Proxmox credentials and the GitHub token besides.

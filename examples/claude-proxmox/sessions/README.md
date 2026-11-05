# claude-proxmox/sessions

The repositories on the [`vm/`](../vm/README.md) VM: for each, a clone at
`~/projects/github/<owner>/<repo>`, its `CLAUDE.local.md`, and a Remote
Control session as a systemd user service, `claude-rc@<owner>/<repo>`
escaped. Register it with `examples/claude-proxmox/sessions` as its root path,
and install it in the [top-level README](../README.md)'s order: after `vm/`.
Its sessions connect once `claude auth login` has been run on the VM, before
or after the install.

It works over SSH, as `vm/`'s user with `vm/`'s automation key, both read from
`vm/`'s state. So openplan's executor needs a route to the VM's address, as it
has to the bridge for `vm/`.

## Credentials

Only the state's, with `vm/`'s values: `TF_CLI_ARGS_init`,
`AWS_ENDPOINT_URL_S3`, `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`.

## Variables

| Variable | Default | |
|---|---|---|
| `vm_workspace` | | the workspace of the `vm/` template, shown in its run log ("Switched to workspace") |
| `state_bucket` | | the bucket in `TF_CLI_ARGS_init` |
| `repositories` | `["openplanhq/openplan", "openplanhq/ai-os"]` | as owner/name, each with its own session |

A clone reaches only what `gh` on the VM can reach: the GitHub token `vm/`
logged it in with, or the login that replaced it.

## What an apply does

- **The first apply** writes the shared unit,
  `~/.config/systemd/user/claude-rc@.service`. Then for each repository it
  clones it if it's missing, writes the `CLAUDE.local.md` that points it at
  its entry in the setup repository's `claude/repos/` (no entry, no file),
  marks the clone trusted and its imports from outside the repository
  approved in `~/.claude.json`, and enables and starts its service. A
  service has no terminal for Claude's questions, so the apply also answers
  "Enable Remote Control?" in that file, once per VM. Without the approval, a
  session would silently drop the entry `CLAUDE.local.md` imports.
- **Adding a repository** does that for the new one only. The others keep
  running.
- **Removing one** disables and stops its service. Its clone stays: it may
  hold unpushed work.
- **A change to the unit or to the start script** restarts every session, so
  a running one picks it up. A session in progress is cut off.
- **A rebuilt VM,** with its new key, gets the unit, clones and services back.
- **Destroy** disables every service and removes the unit. The clones stay.
  It's best effort: with the VM gone or unreachable, it finishes anyway.

A session shows in Claude's Remote Control list under its repository's name.
On the VM, `systemctl --user status 'claude-rc@*'` and
`journalctl --user -u 'claude-rc@*'` show how each is doing.

## Tests

```bash
tofu init -backend=false && tofu test
```

They plan only, with `vm/`'s state stubbed: they check the unit and the
scripts each session runs, not a real VM.

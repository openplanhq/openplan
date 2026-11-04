# claude-proxmox/vm

The VM of [claude-proxmox](../README.md): register it with
`examples/claude-proxmox/vm` as its root path, and install it before
[`sessions/`](../sessions/README.md).

One Debian 13 VM from the cloud image. Cloud-init sets it up at first boot:
git, `gh`, Docker and `make` for openplan's `make check`, Claude Code, and a
clone of the setup repository (`openplanhq/ai-os` by default) with its
`bootstrap.sh` run, where the setup expects it:
`~/projects/github/<owner>/<repo>`. The repositories the agents work on are
`sessions/`'.

Cloud-init runs once. A rotated token, a new key or a changed setup reaches a
new VM only, never the running one, so rebuilding is a destroy and an apply,
then an apply of `sessions/` and `claude auth login` on the VM to bring the
sessions back. That loses only what wasn't pushed, and the login: the
setup's vault sync pushes its notes, and Claude Code and the setup
repository update themselves.

## On the Proxmox host first

The same host settings the cluster template needs; a host that runs it
already has them, and its token and SSH user serve this stack too.

- **A bridge the VM sits on,** such as `vmbr1` on `10.10.10.0/24` with NAT
  through the host at `10.10.10.1`, and a route to it from the machine that
  runs openplan.
- **An API token** for a user whose role can create and configure VMs,
  download the image and allocate datastore space:

  ```bash
  pveum role add OpenplanCluster -privs "VM.Allocate VM.Audit VM.Config.CDROM VM.Config.CPU VM.Config.Cloudinit VM.Config.Disk VM.Config.HWType VM.Config.Memory VM.Config.Network VM.Config.Options VM.PowerMgmt VM.GuestAgent.Audit Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit Sys.AccessNetwork SDN.Use"
  pveum user add openplan@pve
  pveum acl modify / -user openplan@pve -role OpenplanCluster
  pveum user token add openplan@pve tofu --privsep 0
  ```
- **The `import` and `snippets` content types** on the storage the image and
  cloud-init files go to (`local` by default).
- **An SSH user and key** that can write the snippets directory: Proxmox's API
  takes no snippets, so the provider uploads them over SSH.

## Credentials

Set these on the stack. Each value is a single line.

| Credential | Sensitive | Value |
|---|---|---|
| `TF_CLI_ARGS_init` | no | `-backend-config=bucket=<bucket>` |
| `AWS_ENDPOINT_URL_S3` | no | the state bucket's S3 endpoint, such as `https://<account-id>.r2.cloudflarestorage.com` |
| `AWS_ACCESS_KEY_ID` | yes | the bucket token's access key |
| `AWS_SECRET_ACCESS_KEY` | yes | its secret |
| `PROXMOX_VE_ENDPOINT` | no | `https://<host>:8006/` |
| `PROXMOX_VE_API_TOKEN` | yes | `<user>@pve!<token-id>=<secret>` |
| `PROXMOX_VE_INSECURE` | no | `true` while the host has its self-signed certificate |
| `PROXMOX_VE_SSH_USERNAME` | no | the SSH user above |
| `TF_VAR_proxmox_ssh_private_key_data` | yes | its private key, base64: `base64 < key \| tr -d '\n'` |
| `TF_VAR_github_token` | yes | a fine-grained GitHub token, below |

Claude Code takes no credential here. Remote Control needs the full claude.ai
login, which you do once on the VM with `claude auth login`. A
`claude setup-token` token can't start Remote Control sessions.

A stack set up before this still holds `TF_VAR_claude_code_oauth_token`.
Delete it: nothing reads it, and an apply ignores it. The VM that stack built
keeps the old token in `~/.claude-env`, where it outranks the full login in
SSH shells, until the VM is rebuilt.

The GitHub token is what agents push with, unattended, so it reaches only
what they work on. On GitHub, Settings → Developer settings → Fine-grained
tokens: the resource owner that holds the repositories (its settings must
allow fine-grained tokens), only `setup_repository` and the repositories
`sessions/` clones, and read and write on Contents, Pull requests and Issues.
Give it an expiry; a new one means a rebuild.

The token ends up in the cloud-init file on the host's snippets storage
(mode 0600) and in this module's state in the bucket, so keep the bucket's
token to this stack. On the VM it's in cloud-init's copy of that file, on
its cloud-init drive and in `gh`'s own config (a VM has no keyring), all
within reach of `debian`, which has sudo. So the token's reach is the
containment, not where it's stored.

## Variables

| Variable | Default | |
|---|---|---|
| `node_name` | | the Proxmox node |
| `address` | | the VM's address, inside `network_cidr` |
| `ssh_authorized_key` | | a public key that can SSH in as `debian` |
| `git_user_name` | | the name on commits made from the VM |
| `git_user_email` | | the email on them |
| `name` | `claude` | the VM's name and hostname, and part of every file left on the host |
| `setup_repository` | `openplanhq/ai-os` | the repository whose `bootstrap.sh` sets up Claude Code |
| `cores` | `4` | |
| `cpu_units` | `50` | CPU weight against the host's other VMs; below the cluster's, so agents never slow it |
| `memory_mib` | `8192` | fixed: ballooning is off |
| `disk_gib` | `100` | for the repos, Docker images and build caches |
| `bridge` | `vmbr1` | |
| `network_cidr` | `10.10.10.0/24` | |
| `gateway` | `10.10.10.1` | |
| `dns_servers` | `["1.1.1.1", "9.9.9.9"]` | |
| `vm_datastore` | `local-lvm` | |
| `image_datastore` | `local` | needs `import` |
| `snippets_datastore` | `local` | needs `snippets` |
| `image_url` | Debian 13 `genericcloud-amd64`, latest | downloaded once; a newer upstream image doesn't replace the VM |

## Outputs

| Output | |
|---|---|
| `address` | the VM's address |
| `ssh` | `ssh debian@<address>` |
| `user` | `debian` |
| `setup_repository` | as set |
| `automation_private_key` | sensitive: the key `sessions/` connects with |

The automation key is an ed25519 key made in this stack's state and
authorized beside `ssh_authorized_key`. `sessions/` reads it and the rest from
that state, so the key never shows in a log.

## Using it

The Remote Control sessions are `sessions/`'. Those and terminal sessions
both need Claude Code's login: `ssh debian@<address>` and run
`claude auth login`. For a terminal session, run Claude Code in a repository
there, inside `tmux`
so it outlives the connection:

```bash
cd ~/projects/github/openplanhq/openplan && claude
```

The setup takes a few minutes after the apply ends. When something is
missing, `cloud-init status --long` says whether it finished, and
`/var/log/cloud-init-output.log` has the setup's output. A setup stopped by a
failed download can be finished by running it again as `debian`:
`/usr/local/lib/claude-proxmox/setup.sh`.

## Applying from a laptop

To try a change without registering a template, put a local backend in
`backend_override.tf` (gitignored), `terraform { backend "local" {} }`, export
the credentials above and run `tofu init` and `tofu apply`. Destroy it before
the stack in openplan takes the same address.

## Tests

```bash
tofu init -backend=false && tofu test
```

They mock Proxmox: they check the VM's shape and the cloud-init file it boots
with, not a real boot.

# cluster

A k3s cluster on Proxmox: one server VM and one or more agent VMs from a
Debian 13 cloud image, k3s at a pinned version with its own Traefik off (the
`dependencies` module installs one). The module makes the cluster's CAs and a
cluster-admin certificate itself, so the apply ends holding a way into the
cluster, which `dependencies` and `openplan` read from this module's state.
The apply waits for the API to answer `/readyz`, and warns if it never does:
a warning rather than an error, so a cluster that's down can still be
planned and destroyed.

Cloud-init runs once, at a node's first boot, so a changed `k3s_version`,
`ssh_authorized_key` or cloud-init template reaches new nodes only and never
replaces a running one. Upgrading k3s on running nodes is a separate step.

## On the Proxmox host first

These are host settings, not part of a template: a template can't build the
network its own runs travel over.

- **A bridge the VMs sit on.** On a host whose only link is Wi-Fi, guests
  can't be bridged onto the LAN, so they get a private bridge with NAT, for
  example `vmbr1` on `10.10.10.0/24` with the host at `10.10.10.1`:

  ```
  auto vmbr1
  iface vmbr1 inet static
          address 10.10.10.1/24
          bridge-ports none
          bridge-stp off
          bridge-fd 0
          post-up   echo 1 > /proc/sys/net/ipv4/ip_forward
          post-up   iptables -t nat -A POSTROUTING -s '10.10.10.0/24' -o wlp12s0 -j MASQUERADE
          post-down iptables -t nat -D POSTROUTING -s '10.10.10.0/24' -o wlp12s0 -j MASQUERADE
  ```

  The machine running openplan then needs a route to `10.10.10.0/24` via the
  host (macOS: `sudo networksetup -setadditionalroutes "Wi-Fi" 10.10.10.0 255.255.255.0 <host>`).
- **An API token** for a user with a role holding what the module calls
  (downloading the image needs `Sys.AccessNetwork`, deleting files on
  destroy needs `Datastore.Allocate`; on Proxmox 8, use `VM.Monitor` in place
  of `VM.GuestAgent.Audit`):

  ```bash
  pveum role add OpenplanCluster -privs "VM.Allocate VM.Audit VM.Config.CDROM VM.Config.CPU VM.Config.Cloudinit VM.Config.Disk VM.Config.HWType VM.Config.Memory VM.Config.Network VM.Config.Options VM.PowerMgmt VM.GuestAgent.Audit Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit Sys.AccessNetwork SDN.Use"
  pveum user add openplan@pve
  pveum acl modify / -user openplan@pve -role OpenplanCluster
  pveum user token add openplan@pve tofu --privsep 0
  ```
- **The `import` and `snippets` content types** on the storage the image and
  cloud-init files go to (`local` by default).
- **An SSH user and key** that can write the snippets directory
  (`/var/lib/vz/snippets` on `local`): Proxmox's API takes no snippets, so the
  provider uploads them over SSH.

## Credentials

Besides the state bucket's (see the example's README):

| Credential | Sensitive | Value |
|---|---|---|
| `PROXMOX_VE_ENDPOINT` | no | `https://<host>:8006/` |
| `PROXMOX_VE_API_TOKEN` | yes | `<user>@pve!<token-id>=<secret>` |
| `PROXMOX_VE_INSECURE` | no | `true` while the host has its self-signed certificate |
| `PROXMOX_VE_SSH_USERNAME` | no | the SSH user above |
| `TF_VAR_proxmox_ssh_private_key_data` | yes | its private key, base64: `base64 < key \| tr -d '\n'` |

## Variables

| Variable | Default | |
|---|---|---|
| `environment` | | names the VMs (`openplan-<environment>-<index>`) and every file left on the host |
| `node_name` | | the Proxmox node |
| `addresses` | | one per node, the server first; at least two, all inside `network_cidr` |
| `cpu_affinity` | none | host threads, such as `0-2,6-8`, so environments don't share physical cores. Proxmox lets only `root@pam` set it, even through a token, so with the role above leave it empty |
| `cpu_units` | `100` | CPU weight when environments compete; give `prod` more than `bench` (say `200` and `50`) so a benchmark never slows prod |
| `k3s_version` | | such as `v1.36.5+k3s1` |
| `ssh_authorized_key` | | a public key that can SSH into the nodes as `debian` |
| `cores` | `4` | per node |
| `memory_mib` | `6144` | per node, fixed: ballooning is off |
| `disk_gib` | `40` | per node |
| `bridge` | `vmbr1` | |
| `network_cidr` | `10.10.10.0/24` | |
| `gateway` | `10.10.10.1` | |
| `dns_servers` | `["1.1.1.1", "9.9.9.9"]` | |
| `vm_datastore` | `local-lvm` | |
| `image_datastore` | `local` | needs `import` |
| `snippets_datastore` | `local` | needs `snippets` |
| `image_url` | Debian 13 `genericcloud-amd64`, latest | downloaded once; a newer upstream image doesn't replace running VMs |

The cluster's CA keys are in two places: the server's cloud-init file on the
host's snippets storage (mode 0600), and this module's state in the
bucket. Anyone who can read either can make cluster-admin certificates for
ten years, and nothing short of rebuilding the cluster revokes them, so keep
the bucket's token to this stack. The admin certificate itself renews on an
apply in its last 30 days, which changes the outputs once a year.

## Outputs

`api_url`, `cluster_ca_data`, `client_certificate_data`, `client_key_data`
(sensitive) and `node_addresses`. The certificates are base64, as kubeconfig
holds them.

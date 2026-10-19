# openplan on AWS (ECS on EC2)

A working deployment of openplan on AWS, sized to cost as little as ECS
allows: one EC2 instance runs every container, Postgres is the smallest RDS,
and egress goes through a NAT instance instead of a NAT gateway. It is a
starting point for a real install, not a hardened one; the gaps are listed
below.

```
               Route 53: openplan.example.com
                           │
                ALB (ACM certificate, :443)
       /dex/*  ────────────┼───────────── /v1/*, /healthz        /* (default)
          │                │                    │                     │
  ┌───────┴────────────────┴────────────────────┴─────────────────────┴──┐
  │ one EC2 instance (t4g.large), private subnet                         │
  │                                                                      │
  │  control task (awsvpc)                       executor task (awsvpc)  │
  │  db-init, valkey, temporal,  ◄── 7233, 6379 ── gVisor sandbox per    │
  │  dex, api, web on 127.0.0.1                    run; no other ingress │
  └──────────┬───────────────────────────────────────────────┬───────────┘
             │                                               │
      RDS Postgres 16 (db.t4g.micro)          NAT instance (t4g.nano) → internet
      openplan, dex, temporal databases
```

Three root modules, applied in order:

- [`state/`](state): the S3 bucket the other two keep their Terraform state
  in (versioned, encrypted, TLS only), and `backend.hcl`, which points them
  at it. The one module on local state, since it creates the bucket.
- [`network/`](network): VPC across two AZs, public and private subnets, the
  NAT instance, and the Route 53 zone.
- [`op-ecs/`](op-ecs): the certificate, ALB, RDS, S3 bucket, the ECS cluster
  and its instance, and the two tasks.

## Deploy

You need Terraform 1.10+ or OpenTofu 1.10+ (for S3 state locking without
DynamoDB), AWS credentials for the target account, and a domain whose NS
records you can change.

1. Create the state bucket:

   ```bash
   cd state
   terraform init
   terraform apply
   ```

   This writes `../backend.hcl`. Keep `state/terraform.tfstate`: it's small
   and holds no secrets, but it's how this bucket stays managed.

2. Create the network and the zone:

   ```bash
   cd ../network
   terraform init -backend-config=../backend.hcl
   terraform apply -var domain_name=openplan.example.com
   ```

3. Delegate the domain. Set the four `name_servers` from the output as its NS
   records, at the registrar, or in the parent zone for a subdomain. The
   certificate in the next step can't validate until this is done.

4. Deploy openplan, with the same `domain_name` (and the same `name` and
   `region`, if you changed them): op-ecs finds the network by its tags.

   ```bash
   cd ../op-ecs
   terraform init -backend-config=../backend.hcl
   terraform apply -var domain_name=openplan.example.com
   ```

   The apply waits for ACM, then for the services. Temporal applies its
   schema on first boot, so the control task can take a few minutes to go
   healthy.

5. Sign in at the `url` output as the `admin_email` output, with the password
   from:

   ```bash
   terraform output -raw admin_password
   ```

   That user holds openplan's root grant. Add people by adding a connector
   (GitHub, Google, your IdP) to Dex in
   [`templates/dex.yaml.tftpl`](op-ecs/templates/dex.yaml.tftpl), then grant
   them access in openplan.

To tear it all down, `terraform destroy` in `op-ecs/`, then in `network/`
(both with the same `-var domain_name`). RDS skips its final snapshot and the
artifacts bucket is force-destroyed, so this deletes everything, data
included. The state bucket is left for last and on purpose isn't
force-destroyed: empty it of every object version, then `terraform destroy`
in `state/`.

## Why everything is on EC2

The executor starts a gVisor sandbox for every run. That needs `SYS_ADMIN`,
`NET_ADMIN` and five other capabilities, `no-new-privileges`, seccomp off,
`net.ipv4.ip_forward=1` and an exec tmpfs. Fargate allows adding only
`SYS_PTRACE` and supports neither security options nor that sysctl, so the
executor can't run there. Once one instance has to exist for it, putting the
control plane on it too is cheaper than paying Fargate for it as well.

Two tasks rather than one ECS service per component: an awsvpc task takes an
ENI, and t4g instances have three, one of them the instance's own. The control
task's containers share a network namespace, so they find each other on
127.0.0.1 the way they find each other by name under docker compose. The
executor gets its own ENI and security group, which is how it's kept away from
RDS, Dex and the api.

## Cost

us-east-1, on-demand, per month, roughly:

| | $ |
|---|---|
| EC2 t4g.large + 40 GB gp3 | ~52 |
| ALB (hours, ~1 LCU, two public IPv4s) | ~25 |
| RDS db.t4g.micro + 20 GB gp3 | ~14 |
| NAT instance t4g.nano + public IPv4 | ~7 |
| Route 53 zones, CloudWatch logs, SSM, S3 | ~3 |
| **Total** | **~100** |

A one-year Compute Savings Plan takes about a third off the EC2 line, and
the RDS reserved price about the same off RDS.

## Security notes

The Terraform openplan runs is untrusted, so the executor is treated as
hostile:

- **No AWS credentials reach a run.** IMDS requires tokens with a hop limit of
  1, and `ECS_AWSVPC_BLOCK_IMDS=true` blocks it for every awsvpc task, so the
  instance role is out of reach. Neither task has a task role.
- **The executor's network reach.** It accepts no inbound connections. Inside
  the VPC it can reach only Temporal (7233) and Valkey (6379) in the control
  task. RDS admits only the control task.
- **Secrets** are generated by Terraform, kept in SSM Parameter Store, and
  injected by the ECS agent. They are also in op-ecs's Terraform state, so
  whoever can read the state bucket can read them: grant it as narrowly as
  the secrets themselves.

## Known gaps

- **Temporal is unauthenticated** on 7233, and the executor (and so, in
  principle, a sandbox escape) can reach it, exactly as under docker compose.
  Temporal mTLS isn't wired up yet.
- **Static S3 keys.** openplan's S3 adapter doesn't read task-role
  credentials yet, so an IAM user with an access key scoped to the bucket
  stands in.
- **One of everything.** One instance, one RDS in one AZ, one NAT. Losing the
  instance means downtime until the Auto Scaling group replaces it; runs
  survive it, being Temporal workflows. Every deploy also stops the old task
  before starting the new one, since there's no room for both.
- **Temporal's `auto-setup` image** migrates its schema on boot. Use
  `temporalio/server` plus `temporalio/admin-tools` for a real install.
- **Unverified on a live account:** whether ECS accepts
  `seccomp:unconfined` in `dockerSecurityOptions` (the API reference lists
  only no-new-privileges, apparmor, label and credentialspec). If it's
  refused, the fallback is `"seccomp-profile": "unconfined"` in the
  instance's `/etc/docker/daemon.json`, which drops Docker's seccomp profile
  for every container on the instance.

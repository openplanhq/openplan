# The VM comes from vm/'s state in the same bucket: openplan can't hand one
# template's outputs to another, and the automation key, a sensitive output,
# never shows in a log to be copied.
data "terraform_remote_state" "vm" {
  backend   = "s3"
  workspace = var.vm_workspace

  # vm/'s backend; the endpoint and keys come from the same AWS_* credentials
  # this stack's own state uses.
  config = {
    bucket                      = var.state_bucket
    key                         = "claude-proxmox/vm.tfstate"
    region                      = "auto"
    use_path_style              = true
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_s3_checksum            = true
  }

  # A workspace with no state reads as no outputs, not as an error.
  lifecycle {
    postcondition {
      condition     = can(self.outputs.automation_private_key)
      error_message = "No vm/ state in workspace \"${var.vm_workspace}\": install vm/ first, and set vm_workspace to the workspace in its run log."
    }
  }
}

locals {
  vm = data.terraform_remote_state.vm.outputs

  # Everything a provisioner needs, in input: a destroy-time provisioner can
  # read nothing but self.
  connection = {
    host = local.vm.address
    user = local.vm.user
    # terraform_remote_state drops the sensitive mark on outputs it reads.
    private_key = sensitive(local.vm.automation_private_key)
  }

  # One template for every repo, as proven on the VM: %I is the repo, unescaped.
  unit = <<-EOT
    [Unit]
    Description=Claude Remote Control in %I
    # Retry forever: at boot the network may not be up yet.
    StartLimitIntervalSec=0

    [Service]
    WorkingDirectory=%h/projects/github/%I
    Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin
    ExecStart=%h/.local/bin/claude remote-control --name %I
    Restart=always
    RestartSec=15

    [Install]
    WantedBy=default.target
  EOT
}

resource "terraform_data" "unit" {
  triggers_replace = sha256(local.unit)
  input            = merge(local.connection, { unit = local.unit })

  connection {
    type        = "ssh"
    host        = self.input.host
    user        = self.input.user
    private_key = self.input.private_key
  }

  provisioner "remote-exec" {
    inline = [
      "mkdir -p ~/.config/systemd/user",
      "cat > ~/.config/systemd/user/claude-rc@.service <<'UNIT'\n${self.input.unit}UNIT",
      "systemctl --user daemon-reload",
    ]
  }

  provisioner "remote-exec" {
    when   = destroy
    inline = ["rm -f ~/.config/systemd/user/claude-rc@.service", "systemctl --user daemon-reload"]
  }
}

# Keyed by repo, and replaced only when its repo or the unit changes, so adding
# or removing one leaves the others' services running.
resource "terraform_data" "session" {
  for_each = toset(var.repositories)

  triggers_replace = [each.key, terraform_data.unit.triggers_replace]
  input = merge(local.connection, {
    start = templatefile("${path.module}/scripts/session.sh.tftpl", {
      repo  = each.key
      entry = "projects/github/${local.vm.setup_repository}/claude/repos/${split("/", each.key)[1]}.md"
    })
    # The clone stays: it may hold unpushed work.
    stop = "systemctl --user disable --now \"claude-rc@$(systemd-escape \"${each.key}\").service\" || true"
  })

  connection {
    type        = "ssh"
    host        = self.input.host
    user        = self.input.user
    private_key = self.input.private_key
  }

  provisioner "remote-exec" {
    inline = [self.input.start]
  }

  provisioner "remote-exec" {
    when   = destroy
    inline = [self.input.stop]
  }
}

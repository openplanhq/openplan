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

  # A workspace with no state reads as no outputs, not as an error. The
  # defaults let a destroy still plan once vm/ is gone, where this
  # postcondition only warns.
  defaults = {
    address                = ""
    user                   = ""
    setup_repository       = ""
    automation_private_key = ""
  }

  lifecycle {
    postcondition {
      condition     = try(self.outputs.automation_private_key, "") != ""
      error_message = "No vm/ state in workspace \"${var.vm_workspace}\": install vm/ first, and set vm_workspace to the workspace in its run log."
    }
  }
}

locals {
  vm = data.terraform_remote_state.vm.outputs

  # How a provisioner reaches the VM. A destroy-time provisioner can read only
  # self, so it's in triggers_replace: terraform_data copies input, unmarked,
  # into an output a plan prints. And a rebuilt VM, with its new key, gets its
  # unit and sessions back on the next apply.
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
  triggers_replace = merge(local.connection, { unit = sha256(local.unit) })
  input            = { unit = local.unit }

  connection {
    type        = "ssh"
    host        = self.triggers_replace.host
    user        = self.triggers_replace.user
    private_key = self.triggers_replace.private_key
  }

  provisioner "remote-exec" {
    inline = [
      "mkdir -p ~/.config/systemd/user",
      "cat > ~/.config/systemd/user/claude-rc@.service <<'UNIT'\n${self.input.unit}UNIT",
      "systemctl --user daemon-reload",
    ]
  }

  # Best effort, as is a session's: a VM that's gone or rebuilt has nothing to
  # stop, and a destroy that can't finish can't be undone from openplan.
  provisioner "remote-exec" {
    when       = destroy
    on_failure = continue
    inline     = ["rm -f ~/.config/systemd/user/claude-rc@.service", "systemctl --user daemon-reload"]
  }
}

# Keyed by repo, and replaced only when its repo, the unit or the VM changes,
# so adding or removing one leaves the others' services running.
resource "terraform_data" "session" {
  for_each = toset(var.repositories)

  triggers_replace = merge(local.connection, { repo = each.key, unit = terraform_data.unit.triggers_replace.unit })
  input = {
    start = templatefile("${path.module}/scripts/session.sh.tftpl", {
      repo  = each.key
      entry = "projects/github/${local.vm.setup_repository}/claude/repos/${split("/", each.key)[1]}.md"
    })
    # The clone stays: it may hold unpushed work.
    stop = "systemctl --user disable --now \"claude-rc@$(systemd-escape \"${each.key}\").service\" || true"
  }

  connection {
    type        = "ssh"
    host        = self.triggers_replace.host
    user        = self.triggers_replace.user
    private_key = self.triggers_replace.private_key
  }

  provisioner "remote-exec" {
    inline = [self.input.start]
  }

  provisioner "remote-exec" {
    when       = destroy
    on_failure = continue
    inline     = [self.input.stop]
  }
}

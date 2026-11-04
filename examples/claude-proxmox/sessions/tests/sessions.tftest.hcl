# Plans only: an apply would run the provisioners against a real VM.

override_data {
  target = data.terraform_remote_state.vm
  values = {
    outputs = {
      address                = "10.10.10.20"
      user                   = "debian"
      setup_repository       = "openplanhq/ai-os"
      automation_private_key = "KEY"
    }
  }
}

variables {
  vm_workspace = "claude-vm"
  state_bucket = "tfstate"
}

run "one_session_per_repo" {
  command = plan

  assert {
    condition     = toset(keys(terraform_data.session)) == toset(["openplanhq/openplan", "openplanhq/ai-os"])
    error_message = "one session per repo, keyed by the repo, so adding one never touches the others"
  }

  assert {
    condition = alltrue([
      for repo, s in terraform_data.session :
      keys(s.triggers_replace) == ["host", "private_key", "repo", "unit", "user"] &&
      s.triggers_replace.repo == repo && s.triggers_replace.unit == terraform_data.unit.triggers_replace.unit
    ])
    error_message = "a session is replaced only when its repo, the unit or the VM changes"
  }
}

run "start_script" {
  command = plan

  assert {
    condition = alltrue([
      for want in [
        # A folder made by hand, say to trust it, is still cloned into.
        "[ -d \"$HOME/projects/github/openplanhq/ai-os/.git\" ] || gh repo clone \"openplanhq/ai-os\" \"$HOME/projects/github/openplanhq/ai-os\"",
        # A repo with no entry in the setup repo gets no loader, and that's no error.
        "[ -f \"$HOME/projects/github/openplanhq/ai-os/claude/repos/ai-os.md\" ]",
        "systemd-escape \"openplanhq/ai-os\"",
        "systemctl --user enable",
        # restart, not just enable: a changed unit must reach a running service.
        "systemctl --user restart",
      ] : strcontains(terraform_data.session["openplanhq/ai-os"].input.start, want)
    ])
    error_message = "start must clone the repo if it's missing, write its loader when the setup repo has an entry, and enable and restart its instance"
  }

  # A hyphen is escaped in an instance name, so both sides must escape the same string.
  assert {
    condition = (
      strcontains(terraform_data.session["openplanhq/ai-os"].input.stop, "disable --now") &&
      strcontains(terraform_data.session["openplanhq/ai-os"].input.stop, "systemd-escape \"openplanhq/ai-os\"")
    )
    error_message = "stop must disable the instance start enabled"
  }
}

run "unit_change_reaches_sessions" {
  command = plan

  assert {
    condition = alltrue([
      for s in values(terraform_data.session) : s.triggers_replace.unit == terraform_data.unit.triggers_replace.unit
    ]) && terraform_data.unit.triggers_replace.unit == sha256(terraform_data.unit.input.unit)
    error_message = "every session is replaced, and so restarted, when the unit changes"
  }
}

run "unit_is_the_proven_one" {
  command = plan

  assert {
    condition = alltrue([
      for line in [
        "WorkingDirectory=%h/projects/github/%I",
        "Environment=PATH=%h/.local/bin:/usr/local/bin:/usr/bin:/bin",
        "ExecStart=%h/.local/bin/claude remote-control --name %I",
        "Restart=always",
        "RestartSec=15",
        "StartLimitIntervalSec=0",
        "WantedBy=default.target",
      ] : contains(split("\n", terraform_data.unit.input.unit), line)
    ])
    error_message = "the unit must be the one proven on the VM"
  }
}

# The connection is in the triggers: a rebuilt VM, with a new key, gets its
# unit and sessions back on the next apply.
run "connects_with_vm_outputs" {
  command = plan

  assert {
    condition = alltrue([
      for s in concat(values(terraform_data.session), [terraform_data.unit]) :
      s.triggers_replace.host == "10.10.10.20" && s.triggers_replace.user == "debian" && nonsensitive(s.triggers_replace.private_key) == "KEY"
    ])
    error_message = "sessions/ reaches the VM at vm/'s address, as vm/'s user, with vm/'s key"
  }
}

# terraform_data copies input, unmarked, into an output that a destroy or a
# replacement prints in the run log.
run "key_not_in_input" {
  command = plan

  assert {
    condition = alltrue([
      for s in concat(values(terraform_data.session), [terraform_data.unit]) : !can(s.input.private_key)
    ])
    error_message = "the automation key must stay out of input, or a plan prints it"
  }
}

run "bad_repository" {
  command = plan

  variables {
    repositories = ["openplan"]
  }

  expect_failures = [var.repositories]
}

run "missing_vm_workspace" {
  command = plan

  variables {
    vm_workspace = ""
  }

  expect_failures = [var.vm_workspace]
}

# A workspace with no vm/ state reads as no outputs at all.
run "vm_state_missing" {
  command = plan

  override_data {
    target = data.terraform_remote_state.vm
    values = {
      outputs = {}
    }
  }

  expect_failures = [data.terraform_remote_state.vm]
}

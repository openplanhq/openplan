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
      keys(s.triggers_replace) == ["host", "private_key", "repo", "start", "unit", "user"] &&
      s.triggers_replace.repo == repo && s.triggers_replace.unit == terraform_data.unit.triggers_replace.unit
    ])
    error_message = "a session is replaced only when its repo, its start script, the unit or the VM changes"
  }

  # A provisioner runs only on create, so a fix to the start script reaches a
  # running session only by replacing it.
  assert {
    condition = alltrue([
      for s in values(terraform_data.session) : s.triggers_replace.start == sha256(s.input.start)
    ])
    error_message = "a changed start script must replace its session, or the fix never reaches the VM"
  }
}

run "start_script" {
  command = plan

  assert {
    condition = alltrue([
      for want in [
        # A folder made by hand is still cloned into.
        "[ -d \"$HOME/projects/github/openplanhq/openplan/.git\" ] || gh repo clone \"openplanhq/openplan\" \"$HOME/projects/github/openplanhq/openplan\"",
        # A repo with no entry in the setup repo gets no loader, and that's no error.
        "[ -f \"$HOME/projects/github/openplanhq/ai-os/claude/repos/openplan.md\" ]",
        "systemd-escape \"openplanhq/openplan\"",
        "systemctl --user enable",
        # restart, not just enable: a changed unit must reach a running service.
        "systemctl --user restart",
      ] : strcontains(terraform_data.session["openplanhq/openplan"].input.start, want)
    ])
    error_message = "start must clone the repo if it's missing, write its loader when the setup repo has an entry, and enable and restart its instance"
  }

  # sessions/ can be installed straight after vm/, while its first-boot setup
  # still logs gh in and seeds the setup repo this script reads.
  assert {
    condition     = can(regex("(?s)cloud-init status --wait[^\n]*\n.*gh repo clone", terraform_data.session["openplanhq/openplan"].input.start))
    error_message = "start must wait for vm/'s first-boot setup before it clones"
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

# vm/ seeds the setup repo with no .git, as the laptop keeps its git: a clone
# into that folder would fail, and the apply with it. Its session still runs.
run "setup_repository_not_cloned" {
  command = plan

  assert {
    condition     = !strcontains(terraform_data.session["openplanhq/ai-os"].input.start, "gh repo clone")
    error_message = "start must never clone the setup repo: vm/ seeds it"
  }

  assert {
    condition     = strcontains(terraform_data.session["openplanhq/ai-os"].input.start, "systemctl --user restart")
    error_message = "the setup repo still gets its session"
  }

  # With no clone left, a seed that never came would pass unnoticed: a green
  # apply, and a service restarting forever in a folder that isn't there.
  assert {
    condition     = can(regex("(?s)\\[ -d \"\\$HOME/projects/github/openplanhq/ai-os\" \\] \\|\\| \\{[^\n]*exit 1.*systemctl --user restart", terraform_data.session["openplanhq/ai-os"].input.start))
    error_message = "start must fail when vm/ never seeded the setup repo, before its service starts"
  }
}

# A service has no terminal. In an untrusted clone Remote Control exits
# "Workspace not trusted", and with its one-time question unanswered it hangs
# without ever connecting (both seen on the VM, #590).
run "start_trusts_the_clone" {
  command = plan

  assert {
    condition = alltrue([
      for want in [
        # An empty file counts as missing: jq would turn it into nothing.
        "[ -s \"$claude_json\" ] || echo '{}' > \"$claude_json\"",
        "--arg p \"$HOME/projects/github/openplanhq/ai-os\"",
        ".remoteDialogSeen = true",
        ".projects[$p].hasTrustDialogAccepted = true",
        # Imports from outside the repo, like CLAUDE.local.md's entry in the
        # setup repo, wait on a question nobody is there to answer.
        ".projects[$p].hasClaudeMdExternalIncludesApproved = true",
        ".projects[$p].hasClaudeMdExternalIncludesWarningShown = true",
        # Separate lines, so a jq failure stops the script under set -e.
        "> \"$tmp\"\nmv \"$tmp\" \"$claude_json\"",
      ] : strcontains(terraform_data.session["openplanhq/ai-os"].input.start, want)
    ])
    error_message = "start must mark the clone trusted, its outside imports approved and Remote Control's question answered in ~/.claude.json, creating the file if Claude Code has never run"
  }

  # A Claude save, or another repo's script, can rename over our write and
  # drop the entry; nothing re-runs this script, so it must fail instead.
  assert {
    condition     = can(regex("(?s)hasTrustDialogAccepted = true.*\nmv .*jq -e [^\n]*\\.remoteDialogSeen and \\.projects\\[\\$p\\]\\.hasTrustDialogAccepted and \\.projects\\[\\$p\\]\\.hasClaudeMdExternalIncludesApproved.*systemctl --user restart", terraform_data.session["openplanhq/ai-os"].input.start))
    error_message = "start must check the entry survived, after the write and before its service starts, or a lost write leaves the clone untrusted or its imports dropped for good"
  }
}

# ai-os's shared settings deny reading .env, and Claude's Edit tool reads a
# file before it changes it, so the deny blocks edits too. The VM drops it.
run "start_lets_claude_edit_env" {
  command = plan

  assert {
    condition = alltrue([
      for want in [
        "settings=\"$HOME/.claude/settings.json\"",
        # No settings (bootstrap failed) is no reason to fail the session.
        "[ -f \"$settings\" ]",
        "del(.permissions.deny[]? | select(. == \"Read(**/.env)\"))",
      ] : strcontains(terraform_data.session["openplanhq/ai-os"].input.start, want)
    ])
    error_message = "start must drop the shared Read(**/.env) deny from the VM's Claude settings, and only when the file exists"
  }

  assert {
    condition     = can(regex("(?s)Read\\(\\*\\*/\\.env\\).*systemctl --user restart", terraform_data.session["openplanhq/ai-os"].input.start))
    error_message = "the deny must be gone before the service starts, or its sessions start with it"
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
        # Nobody is at the claude.ai end to approve each tool call.
        "ExecStart=%h/.local/bin/claude remote-control --name %I --permission-mode auto",
        "Restart=always",
        "RestartSec=15",
        "StartLimitIntervalSec=0",
        "WantedBy=default.target",
      ] : contains(split("\n", terraform_data.unit.input.unit), line)
    ])
    error_message = "the unit must be the one proven on the VM"
  }
}

# One install per VM, apart from the sessions: a version bump reinstalls
# without restarting them, and adding a repo doesn't reinstall.
run "tools_once_per_vm" {
  command = plan

  assert {
    condition = (
      keys(terraform_data.tools.triggers_replace) == ["host", "private_key", "script", "user"] &&
      terraform_data.tools.triggers_replace.script == sha256(terraform_data.tools.input.script)
    )
    error_message = "the tools are replaced only when their script or the VM changes"
  }
}

run "tools_script" {
  command = plan

  assert {
    condition = alltrue([
      for want in [
        "https://github.com/jgraph/drawio-desktop/releases/download/v32.6.1/drawio-amd64-32.6.1.deb",
        "515a975fbdb982e0d736b6f99248dbbb856a8b07bb72d6d3648a54b850fa33db",
        "@mermaid-js/mermaid-cli@11.17.1",
        "PUPPETEER_SKIP_DOWNLOAD=1",
        # Electron needs a display; -a gives parallel sessions one each.
        "exec xvfb-run -a /opt/drawio/drawio --disable-gpu \"$@\"",
        "PUPPETEER_EXECUTABLE_PATH=/usr/bin/chromium exec /opt/mermaid-cli/bin/mmdc \"$@\"",
      ] : strcontains(terraform_data.tools.input.script, want)
    ])
    error_message = "the script must install the pinned draw.io and mermaid-cli and put both CLIs on PATH, headless"
  }

  # sessions/ can be installed straight after vm/, while cloud-init still
  # holds apt's lock.
  assert {
    condition     = can(regex("(?s)cloud-init status --wait.*apt-get", terraform_data.tools.input.script))
    error_message = "the script must wait for vm/'s first-boot setup before it uses apt"
  }

  # The image's apt-daily and unattended-upgrades can hold apt's locks at any
  # time, and apt fails at once on a held lock unless told to wait.
  assert {
    condition = (
      strcontains(terraform_data.tools.input.script, "until apt-get update") &&
      alltrue([
        for line in split("\n", terraform_data.tools.input.script) :
        !strcontains(line, "apt-get install") || strcontains(line, "DPkg::Lock::Timeout=")
      ]) &&
      length(regexall("(?m)^apt_get install", terraform_data.tools.input.script)) == 2
    )
    error_message = "the script must wait for dpkg's lock on every install and retry the update, or a background apt run fails the apply"
  }

  # Under set -e, a download that doesn't match fails the apply before
  # anything from it is installed.
  assert {
    condition     = can(regex("(?s)^set -eu\n.*sha256sum -c.*apt_get install[^\n]*\\$deb", terraform_data.tools.input.script))
    error_message = "the draw.io package must be checked against its pinned SHA-256 before it is installed"
  }
}

# The connection is in the triggers: a rebuilt VM, with a new key, gets its
# unit, sessions and tools back on the next apply.
run "connects_with_vm_outputs" {
  command = plan

  assert {
    condition = alltrue([
      for s in concat(values(terraform_data.session), [terraform_data.unit, terraform_data.tools]) :
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
      for s in concat(values(terraform_data.session), [terraform_data.unit, terraform_data.tools]) : !can(s.input.private_key)
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

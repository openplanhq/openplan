# bpg checks that a file ID looks like Proxmox's storage:content/file.
mock_provider "proxmox" {
  mock_resource "proxmox_download_file" {
    defaults = { id = "local:import/claude-debian-13.qcow2" }
  }

  mock_resource "proxmox_virtual_environment_file" {
    defaults = { id = "local:snippets/claude-user-data.yaml" }
  }
}

variables {
  node_name                    = "pve"
  address                      = "10.10.10.20"
  ssh_authorized_key           = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest owner"
  proxmox_ssh_private_key_data = "a2V5"
  git_user_name                = "Test Owner"
  git_user_email               = "owner@example.invalid"
  github_token                 = "github_pat_11TEST_abc"
  claude_code_oauth_token      = "sk-ant-oat01-TEST_abc-def"
}

run "packages" {
  assert {
    condition = toset(yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).packages) == toset([
      "qemu-guest-agent", "git", "gh", "jq", "curl", "tmux", "make", "docker.io", "docker-cli", "docker-buildx",
    ])
    error_message = "the VM needs git, gh, jq, curl, tmux, make and Docker with its CLI and buildx (only Recommends of docker.io) for the setup and for make check"
  }
}

# A token pasted with its line ending would reach the VM as a broken login.
run "github_token_with_newline" {
  command = plan

  variables {
    github_token = "github_pat_11TEST_abc\n"
  }

  expect_failures = [var.github_token]
}

run "claude_token_with_space" {
  command = plan

  variables {
    claude_code_oauth_token = "sk-ant-oat01-TEST_abc-def "
  }

  expect_failures = [var.claude_code_oauth_token]
}

run "tokens_round_trip" {
  assert {
    condition = one([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files : f
      if f.path == "/home/debian/.github-token"
      ]) == {
      path        = "/home/debian/.github-token"
      content     = var.github_token
      owner       = "debian:debian"
      permissions = "0600"
      defer       = true
    }
    error_message = "the GitHub token must reach the VM intact, readable by debian only, written after debian exists"
  }

  assert {
    condition = length([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files : f
      if f.path == "/home/debian/.claude-env" && f.owner == "debian:debian" && f.permissions == "0600" && f.defer && length(split(var.claude_code_oauth_token, f.content)) == 2
    ]) == 1
    error_message = "Claude's token must be in .claude-env exactly once, readable by debian only"
  }
}

run "root_runcmd_order" {
  assert {
    condition = alltrue([
      for step in ["usermod -aG docker debian", "loginctl enable-linger debian", "systemctl start user@"] :
      min([
        for i, c in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).runcmd : i
        if strcontains(c, step)
        ]...) < min([
        for i, c in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).runcmd : i
        if strcontains(c, "/usr/local/lib/claude-proxmox/setup.sh")
      ]...)
    ])
    error_message = "debian must be in the docker group, lingering, with its systemd manager up before the setup runs, or make check and the vault-sync timer fail"
  }

  assert {
    condition = anytrue([
      for c in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).runcmd :
      strcontains(c, "runuser -u debian") && strcontains(c, "XDG_RUNTIME_DIR=/run/user/") && strcontains(c, "/usr/local/lib/claude-proxmox/setup.sh")
    ])
    error_message = "the setup runs as debian, with XDG_RUNTIME_DIR so bootstrap.sh reaches its systemd manager"
  }
}

run "setup_script" {
  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      f.permissions == "0755" && alltrue([
        for want in [
          # Logged in already on a re-run, when the token file is gone.
          "gh auth status >/dev/null 2>&1 || gh auth login --with-token < \"$HOME/.github-token\"",
          "rm -f \"$HOME/.github-token\"",
          "gh auth setup-git",
          "gh repo clone \"openplanhq/ai-os\" \"$HOME/projects/github/openplanhq/ai-os\"",
          "https://claude.ai/install.sh",
          # The token logs in, but a session would open on the first-run screens.
          ".hasCompletedOnboarding = true",
          "\"$HOME/projects/github/openplanhq/ai-os/bootstrap.sh\"",
        ] : strcontains(f.content, want)
      ])
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script must log in to gh, drop the token file, clone the setup repo, install Claude Code and run bootstrap.sh"
  }

  # The other repos and their loaders are sessions/', on the running VM.
  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      !strcontains(f.content, "CLAUDE.local.md") && !strcontains(f.content, "openplanhq/openplan")
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script clones only the setup repo and writes no loader"
  }

  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      !strcontains(f.content, var.github_token) && !strcontains(f.content, var.claude_code_oauth_token)
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script is world-readable, so it must carry no token"
  }

  assert {
    condition     = length([for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files : f if f.path == "/usr/local/lib/claude-proxmox/setup.sh"]) == 1
    error_message = "cloud-init must write the setup script"
  }
}

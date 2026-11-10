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
  syncthing_peer               = "AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-HHHHHHH"
}

run "packages" {
  assert {
    condition = toset(yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).packages) == toset([
      "qemu-guest-agent", "git", "gh", "jq", "curl", "tmux", "make", "docker.io", "docker-cli", "docker-buildx", "syncthing",
    ])
    error_message = "the VM needs git, gh, jq, curl, tmux, make and Docker with its CLI and buildx (only Recommends of docker.io) for the setup and for make check, and Syncthing to mirror the setup repo"
  }
}

# A device ID is eight groups of seven base32 characters; anything else would
# leave the VM paired with nothing.
run "syncthing_peer_malformed" {
  command = plan

  variables {
    syncthing_peer = "not-a-device-id"
  }

  expect_failures = [var.syncthing_peer]
}

# A token pasted with its line ending would reach the VM as a broken login.
run "github_token_with_newline" {
  command = plan

  variables {
    github_token = "github_pat_11TEST_abc\n"
  }

  expect_failures = [var.github_token]
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
    condition     = !strcontains(proxmox_virtual_environment_file.user_data.source_raw[0].data, "CLAUDE_CODE_OAUTH_TOKEN")
    error_message = "no Claude token reaches the VM: Remote Control needs the full login, which is done on the VM"
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
    error_message = "debian must be in the docker group, lingering, with its systemd manager up before the setup runs, or make check, Syncthing and the Remote Control services fail"
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
          # A copy with no .git: the laptop is the only machine that commits.
          "gh api \"repos/openplanhq/ai-os/tarball\" | tar -xz",
          "#include .stignore-shared",
          "https://claude.ai/install.sh",
          # A session would otherwise open on the first-run screens.
          ".hasCompletedOnboarding = true",
          # A command run over SSH finds claude: the line goes on top of
          # .bashrc, above Debian's early return for non-interactive shells.
          "line='export PATH=\"$HOME/.local/bin:$PATH\"'",
          "printf '%s\\n' \"$line\" | cat - ~/.bashrc",
          "\"$HOME/projects/github/openplanhq/ai-os/bootstrap.sh\"",
          # No ~/Sync folder: the setup repo is the only thing shared.
          "syncthing generate --no-default-folder",
          "systemctl --user enable --now syncthing.service",
          "syncthing cli config devices add --device-id AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-HHHHHHH",
          "syncthing cli config folders add --id ai-os",
          "syncthing cli config folders ai-os devices add --device-id AAAAAAA-BBBBBBB-CCCCCCC-DDDDDDD-EEEEEEE-FFFFFFF-GGGGGGG-HHHHHHH",
        ] : strcontains(f.content, want)
      ])
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script must log in to gh, drop the token file, seed the setup repo from its tarball with its .stignore, install Claude Code, put ~/.local/bin on PATH at the top of .bashrc, run bootstrap.sh, and share the setup repo with the laptop over Syncthing"
  }

  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      !strcontains(f.content, "gh repo clone")
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup repo is never cloned on the VM: its .git lives on the laptop"
  }

  # Unpacked beside its folder and moved in whole, so a re-run after a failed
  # download never keeps half a repo. Syncthing starts after bootstrap.sh, so
  # its first scan sees the finished folder, its .stignore and its config: a
  # Syncthing started before generate makes ~/Sync and shares it.
  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      can(regex("(?s)mktemp -d.*tar -xz.*mv \"\\$tmp\" \"\\$repo\"", f.content)) &&
      can(regex("(?s)/tarball.*#include \\.stignore-shared.*ai-os/bootstrap\\.sh\".*syncthing generate --no-default-folder.*systemctl --user enable --now syncthing\\.service.*syncthing cli config folders add", f.content))
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script must unpack the tarball into a temporary folder and move it into place, then run bootstrap.sh, and only then start Syncthing"
  }

  # The installer puts claude in ~/.local/bin, which cloud-init's PATH lacks,
  # and bootstrap.sh needs claude to install the plugins and the MCP server.
  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      can(regex("(?ms)^export PATH=\"\\$HOME/\\.local/bin:\\$PATH\"$.*\"\\$HOME/projects/github/openplanhq/ai-os/bootstrap\\.sh\"", f.content))
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script must put ~/.local/bin on PATH before it runs bootstrap.sh"
  }

  # The other repos and their loaders are sessions/', on the running VM.
  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      !strcontains(f.content, "CLAUDE.local.md") && !strcontains(f.content, "openplanhq/openplan")
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script seeds only the setup repo and writes no loader"
  }

  assert {
    condition = alltrue([
      for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files :
      !strcontains(f.content, var.github_token)
      if f.path == "/usr/local/lib/claude-proxmox/setup.sh"
    ])
    error_message = "the setup script is world-readable, so it must carry no token"
  }

  assert {
    condition     = length([for f in yamldecode(proxmox_virtual_environment_file.user_data.source_raw[0].data).write_files : f if f.path == "/usr/local/lib/claude-proxmox/setup.sh"]) == 1
    error_message = "cloud-init must write the setup script"
  }
}

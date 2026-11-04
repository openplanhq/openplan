variable "vm_workspace" {
  description = "The workspace of this stack's vm/ template, shown in its run log (\"Switched to workspace\"). The VM's address and key come from that state."
  type        = string

  validation {
    condition     = trimspace(var.vm_workspace) != ""
    error_message = "Set vm_workspace to the workspace in vm/'s run log (\"Switched to workspace\")."
  }
}

variable "state_bucket" {
  description = "The bucket vm/'s state is in: the one in TF_CLI_ARGS_init."
  type        = string
}

variable "repositories" {
  description = "The GitHub repositories, as owner/name, each cloned on the VM with a Remote Control session of its own. Each with an entry in the setup repository's claude/repos/ gets its CLAUDE.local.md."
  type        = list(string)
  default     = ["openplanhq/openplan", "openplanhq/ai-os"]

  validation {
    condition     = alltrue([for repo in var.repositories : can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", repo))])
    error_message = "Every repository must be owner/name."
  }
}

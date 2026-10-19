variable "secret" {
  type      = string
  sensitive = true
  default   = "hunter3"
}
resource "terraform_data" "replaced" {
  triggers_replace = "v2"
}
resource "terraform_data" "replaced_cbd" {
  triggers_replace = "v2"
  lifecycle {
    create_before_destroy = true
  }
}
resource "terraform_data" "updated" {
  input = "400"
}
resource "terraform_data" "secret" {
  input = var.secret
}
moved {
  from = terraform_data.old_name
  to   = terraform_data.new_name
}
resource "terraform_data" "new_name" {
  input = "moving"
}
removed {
  from = terraform_data.forgotten
  lifecycle {
    destroy = false
  }
}
resource "terraform_data" "created" {
  input = { nested = [1, 2], name = "alarm" }
}

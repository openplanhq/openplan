variable "secret" {
  type      = string
  sensitive = true
  default   = "hunter2"
}
resource "terraform_data" "replaced" {
  triggers_replace = "v1"
}
resource "terraform_data" "replaced_cbd" {
  triggers_replace = "v1"
  lifecycle {
    create_before_destroy = true
  }
}
resource "terraform_data" "destroyed" {
  input = "gone"
}
resource "terraform_data" "updated" {
  input = "200"
}
resource "terraform_data" "secret" {
  input = var.secret
}
resource "terraform_data" "old_name" {
  input = "moving"
}
resource "terraform_data" "forgotten" {
  input = "keep"
}

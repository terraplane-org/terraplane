variable "random_length" {
  type        = number
  description = "Length of the random_string resource. Override with TF_VAR_random_length."
  default     = 16
}

resource "random_string" "random" {
  length  = var.random_length
  special = true
}

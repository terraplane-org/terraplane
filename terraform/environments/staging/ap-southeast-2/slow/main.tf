variable "plan_delay_seconds" {
  type        = number
  default     = 25
  description = "Sleep during plan (and refresh) so streamed PR comments have time to update."
}

variable "apply_delay" {
  type        = string
  default     = "30s"
  description = "time_sleep duration so apply also lasts long enough to stream."
}

# external data runs at plan time. time_sleep only delays apply.
data "external" "plan_delay" {
  program = ["sh", "-c", "sleep ${var.plan_delay_seconds}; printf '{}'"]
}

resource "time_sleep" "apply_delay" {
  create_duration  = var.apply_delay
  destroy_duration = "5s"
  triggers = {
    ran_at = timestamp()
  }
}

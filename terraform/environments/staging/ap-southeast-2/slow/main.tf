variable "step_seconds" {
  type        = number
  default     = 5
  description = "Sleep between progress lines during plan/apply streaming demos."
}

variable "steps" {
  type        = number
  default     = 8
  description = "How many progress steps to emit (plan and apply)."
}

# external data runs at plan time. Progress goes to stderr so stdout stays valid JSON.
data "external" "plan_delay" {
  program = [
    "sh", "-c",
    <<-EOT
      i=1
      while [ "$i" -le "${var.steps}" ]; do
        echo "slow plan progress: step $i/${var.steps}" >&2
        sleep ${var.step_seconds}
        i=$((i + 1))
      done
      printf '{}'
    EOT
  ]
}

# local-exec runs at apply time and prints progress terraform will stream.
resource "terraform_data" "apply_delay" {
  input = timestamp()

  provisioner "local-exec" {
    command = <<-EOT
      i=1
      while [ "$i" -le "${var.steps}" ]; do
        echo "slow apply progress: step $i/${var.steps}"
        sleep ${var.step_seconds}
        i=$((i + 1))
      done
    EOT
  }

  provisioner "local-exec" {
    when    = destroy
    command = "echo 'slow apply destroy complete'"
  }
}

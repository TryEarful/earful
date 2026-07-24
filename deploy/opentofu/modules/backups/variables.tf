variable "backups_project" {
  type = string
}

variable "pro_project" {
  type = string
}

variable "region" {
  type = string
}

variable "sql_instance_name" {
  type = string
}

variable "sql_instance_sa_email" {
  description = "The instance's own service account (serviceAccountEmailAddress) — the identity that writes exports."
  type        = string
}

variable "lock_retention" {
  description = "IRREVERSIBLE once true. The runbook flips this right after the first export + restore drill proves the pipeline (locking a misconfigured bucket would strand it for 30 days)."
  type        = bool
  default     = false
}

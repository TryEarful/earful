variable "project" {
  type = string
}

variable "env_name" {
  description = "stg / pro — display names only."
  type        = string
}

variable "service_name" {
  type    = string
  default = "earful"
}

variable "host" {
  description = "Hostname the uptime check hits (run.app host until cutover, then the custom domain)."
  type        = string
}

variable "sql_instance_name" {
  type = string
}

# Required rather than defaulted. Every alert this module raises is
# delivered here, so a default would quietly send one operator's pages to
# whichever address happened to be in the repository when they cloned it.
variable "alert_email" {
  description = "Where every alert from this environment is delivered."
  type        = string

  validation {
    condition     = can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", var.alert_email))
    error_message = "alert_email must be an email address."
  }
}

variable "enable_purge" {
  description = "Watch the nightly retention job (M8-T2). Match run-service's enable_purge: alerting on a job that does not exist is noise."
  type        = bool
  default     = false
}

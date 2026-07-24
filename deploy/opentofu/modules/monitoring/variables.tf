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

variable "alert_email" {
  type    = string
  default = "support@tryearful.com"
}

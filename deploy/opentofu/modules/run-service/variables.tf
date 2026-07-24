variable "project" {
  type = string
}

variable "region" {
  type = string
}

variable "service_name" {
  type    = string
  default = "earful"
}

variable "image" {
  description = "Seed image only — ignored after first apply (pipeline owns deploys)."
  type        = string
}

variable "service_account_email" {
  description = "Runtime SA (created by the env stack; needs cloudsql.client + per-secret accessor)."
  type        = string
}

variable "sql_connection_name" {
  type = string
}

variable "env" {
  description = "Plain env vars."
  type        = map(string)
  default     = {}
}

variable "secret_env" {
  description = "env var name => Secret Manager secret_id (latest version). Only reference secrets that HAVE a version — Cloud Run refuses to start otherwise."
  type        = map(string)
  default     = {}
}

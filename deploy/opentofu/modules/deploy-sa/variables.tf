variable "project" {
  type = string
}

variable "region" {
  type = string
}

variable "runtime_sa_id" {
  description = "Full resource id (projects/.../serviceAccounts/...) of the runtime SA this deployer may act as."
  type        = string
}

variable "registry_project" {
  type = string
}

variable "registry_repo_id" {
  type = string
}

variable "registry_role" {
  description = "roles/artifactregistry.writer (stg) or .reader (pro)."
  type        = string
}

variable "grant_log_reader" {
  type    = bool
  default = false
}

variable "wif_pool_name" {
  description = "Full workload identity pool resource name from bootstrap."
  type        = string
}

variable "principal_suffix" {
  type = string
}

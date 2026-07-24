variable "project" {
  type = string
}

variable "region" {
  type = string
}

variable "instance_name" {
  type    = string
  default = "earful"
}

variable "tier" {
  description = "db-f1-micro (stg) / db-g1-small (pro)."
  type        = string
}

variable "pitr_enabled" {
  description = "Point-in-time recovery, 7-day WAL retention (M9-T1, pro only)."
  type        = bool
  default     = false
}

variable "deletion_protection" {
  type    = bool
  default = false
}

variable "retained_backups" {
  type    = number
  default = 7
}

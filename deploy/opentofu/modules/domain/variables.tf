variable "project" {
  type = string
}

variable "region" {
  type = string
}

variable "service_name" {
  type = string
}

variable "domain" {
  description = "e.g. stg.tryearful.com; empty = no mapping yet."
  type        = string
  default     = ""
}

variable "dns_project" {
  description = "Ops project holding the zone."
  type        = string
}

variable "dns_zone_name" {
  type = string
}

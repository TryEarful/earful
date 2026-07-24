variable "state_bucket" {
  description = "The bootstrap-created state bucket (earful-tofu-state-<sfx>) — needed to read bootstrap's outputs."
  type        = string
}

variable "custom_domain" {
  description = "stg.tryearful.com — set only AFTER the nameserver cutover; empty runs on the deterministic run.app URL."
  type        = string
  default     = ""
}

variable "alert_email" {
  type    = string
  default = "support@tryearful.com"
}

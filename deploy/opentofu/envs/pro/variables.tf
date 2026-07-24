variable "state_bucket" {
  description = "The bootstrap-created state bucket (earful-tofu-state-<sfx>)."
  type        = string
}

variable "custom_domain" {
  description = "app.tryearful.com — set only AFTER the nameserver cutover."
  type        = string
  default     = ""
}

variable "email_sender" {
  description = "console (boot value) or brevo. Flip to brevo ONLY after the BREVO_API_KEY secret has a version (README step 9) — the revision mounts it and will not start otherwise."
  type        = string
  default     = "console"

  validation {
    condition     = contains(["console", "brevo"], var.email_sender)
    error_message = "email_sender must be console or brevo."
  }
}

variable "google_client_id" {
  description = "OAuth client ID for Google sign-in; empty keeps the button hidden. Requires the GOOGLE_CLIENT_SECRET secret to have a version first."
  type        = string
  default     = ""
}

variable "lock_retention" {
  description = "IRREVERSIBLE. Flip to true right after the first export + restore drill (runbook)."
  type        = bool
  default     = false
}

variable "alert_email" {
  type    = string
  default = "support@tryearful.com"
}

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

# --- AI (M5, M6, M10, M11) --------------------------------------------
#
# Which backend and which models serve which operation is configuration.
# Everything defaults to off: an environment with no model named runs
# with the AI features absent, which the product handles by not offering
# them.

variable "ai_provider" {
  description = "Text AI backend: none | openai | vertex."
  type        = string
  default     = "none"
}

variable "transcribe_provider" {
  description = "Voice backend: none | vertex | openai | whisper-cli."
  type        = string
  default     = "none"
}

# The ids below are what europe-west4 actually offers, verified against
# the live publisher list on 2026-07-25 and exercised end to end by
# internal/ai's integration test. Gemini 3.x is deliberately NOT used:
# it resolves only at Vertex's `global` location, and the EU pin outranks
# model recency (ADR-0011). Upgrade when 3.x reaches an EU region.
variable "ai_model" {
  description = "Default model for every AI operation. Must be available in var.region (ADR-0011)."
  type        = string
  default     = "gemini-2.5-flash"
}

variable "ai_model_analyze" {
  description = "Model for Insight Summaries, which want a stronger tier than question drafting. Empty falls back to ai_model."
  type        = string
  default     = "gemini-2.5-pro"
}

# Set here rather than left on the application default, because the first
# way to discover an unset budget is a breaker tripping mid-run. €2/day is
# ~€60/month at the cap, which is the top of PLAN.md Appendix C's AI line
# — a ceiling, not a forecast: real early usage is a few cents a day.
# Raise it deliberately when the breaker starts tripping for real reasons.
variable "ai_daily_budget_eur" {
  description = "Global daily AI breaker: every AI endpoint refuses once the day's estimated spend reaches this."
  type        = number
  default     = 2
}

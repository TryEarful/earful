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

variable "ai_model" {
  description = "Default model for every AI operation (verify the id against the live publisher list before setting)."
  type        = string
  default     = ""
}

variable "ai_model_analyze" {
  description = "Model for Insight Summaries, which want a stronger tier than question drafting. Empty falls back to ai_model."
  type        = string
  default     = ""
}

variable "state_bucket" {
  description = "The bootstrap-created state bucket (earful-tofu-state-<sfx>) — needed to read bootstrap's outputs."
  type        = string
}

variable "custom_domain" {
  description = "Staging hostname; set only AFTER the nameserver cutover. Empty runs on the deterministic run.app URL."
  type        = string
  default     = ""
}

# No default: alerts and the /trust contact both come from here, and
# neither should fall back to an address belonging to whoever published
# the repository. Set it in this root's tfvars.
variable "alert_email" {
  description = "Where alerts are delivered, and the contact shown on /trust."
  type        = string
}

# --- AI (M5, M6, M10, M11) --------------------------------------------
#
# Which backend and which models serve which operation is configuration.
# Everything defaults to off: an environment with no model named runs
# with the AI features absent, which the product handles by not offering
# them.

# "scripted" is deterministic canned output with no model behind it. The
# app refuses it in production (it would misrepresent invented content as
# AI output) and allows it here, where there are no real respondents.
# Validation because a typo would otherwise reach a startup probe rather
# than the plan: the app rejects an unknown provider at boot, and a
# rollout that fails there is a much later and more confusing place to
# find out than `tofu plan`.

variable "ai_provider" {
  description = "Text AI backend: none | openai | vertex | scripted."
  type        = string
  default     = "none"

  validation {
    condition     = contains(["none", "openai", "vertex", "scripted"], var.ai_provider)
    error_message = "ai_provider must be one of: none, openai, vertex, scripted."
  }
}

variable "transcribe_provider" {
  description = "Voice backend: none | vertex | openai | whisper-cli | scripted."
  type        = string
  default     = "none"

  validation {
    condition = contains(
      ["none", "vertex", "openai", "whisper-cli", "scripted"],
      var.transcribe_provider
    )
    error_message = "transcribe_provider must be one of: none, vertex, openai, whisper-cli, scripted."
  }
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
# way to discover an unset budget is a breaker tripping mid-run. Staging's
# job is to exercise the AI paths for the smoke gate, and a gate run costs
# a fraction of a cent -- €1/day is far more than that and still a bill
# that cannot surprise anyone.
variable "ai_daily_budget_eur" {
  description = "Global daily AI breaker: every AI endpoint refuses once the day's estimated spend reaches this."
  type        = number
  default     = 1
}

# The instance's own identity. No defaults: these are facts about one
# deployment, and a value committed here becomes the value every clone
# of this repository quietly inherits. Set them in this root's tfvars.

variable "email_from" {
  description = "From address for outgoing mail."
  type        = string
}

variable "hosting_region" {
  description = "Where this instance runs, as stated on /trust — a cloud region, a country, a rack; whatever is true."
  type        = string
}

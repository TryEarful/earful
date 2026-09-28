variable "state_bucket" {
  description = "The bootstrap-created state bucket (earful-tofu-state-<sfx>)."
  type        = string
}

variable "custom_domain" {
  description = "Production hostname; set only AFTER the nameserver cutover."
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

# "scripted" is absent from both lists on purpose: it invents content,
# and the app refuses to boot with it when APP_ENV is production. Naming
# it here as a rejected value would suggest it were a choice.

variable "ai_provider" {
  description = "Text AI backend: none | openai | vertex."
  type        = string
  default     = "none"

  validation {
    condition     = contains(["none", "openai", "vertex"], var.ai_provider)
    error_message = "ai_provider must be one of: none, openai, vertex."
  }
}

variable "transcribe_provider" {
  description = "Voice backend: none | vertex | openai | whisper-cli."
  type        = string
  default     = "none"

  validation {
    condition     = contains(["none", "vertex", "openai", "whisper-cli"], var.transcribe_provider)
    error_message = "transcribe_provider must be one of: none, vertex, openai, whisper-cli."
  }
}

# Where AI calls are processed, independently of where the service runs.
# "eu" is Vertex's jurisdictional multi-region: its endpoint guarantees
# ML processing inside EU member states, which is the promise /trust
# makes (ADR-0013). The global endpoint is never used (ADR-0011).
variable "ai_location" {
  description = "Vertex location for every AI call: the eu multi-region, or a single EU region. Never global (ADR-0011, ADR-0013)."
  type        = string
  default     = "eu"

  validation {
    condition     = var.ai_location != "global" && var.ai_location != ""
    error_message = "ai_location must be the eu multi-region or a single EU region, never global."
  }
}

# The ids below have documented ML processing in var.ai_location on
# Google's per-model data-residency table, checked on 2026-09-28, and are
# exercised end to end by internal/ai's integration test. The 2.5 family
# retires on 2026-10-20. Only the Flash tier of Gemini 3.x has EU
# processing, so Insight Summaries run on the same model as everything
# else; a stronger EU-resident tier is a tfvars change when one appears.
variable "ai_model" {
  description = "Default model for every AI operation. Must have ML processing in var.ai_location (ADR-0013)."
  type        = string
  default     = "gemini-3.8-flash"
}

variable "ai_model_analyze" {
  description = "Model for Insight Summaries, when a stronger tier than question drafting exists in var.ai_location. Empty falls back to ai_model."
  type        = string
  default     = "gemini-3.8-flash"
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

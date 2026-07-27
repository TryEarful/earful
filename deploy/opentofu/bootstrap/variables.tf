variable "billing_account" {
  description = "Billing account ID (XXXXXX-XXXXXX-XXXXXX) all four projects link to. Must be EUR-denominated for the €-budget thresholds to mean what PLAN.md says."
  type        = string
}

variable "suffix" {
  description = "Short globally-unique suffix for project IDs (they share the global GCP namespace), e.g. a random 4-6 lowercase chars."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]{2,8}$", var.suffix))
    error_message = "suffix must be 2-8 lowercase alphanumerics (it lands inside project IDs)."
  }
}

variable "stg_suffix" {
  description = "Overrides suffix for the staging project id only, so staging can be rebuilt in place without recreating ops, pro or backups. Null uses var.suffix."
  type        = string
  default     = null

  validation {
    condition     = var.stg_suffix == null || can(regex("^[a-z0-9]{2,8}$", var.stg_suffix))
    error_message = "stg_suffix must be 2-8 lowercase alphanumerics (it lands inside a project ID)."
  }
}

variable "region" {
  description = "Home region for everything regional. EU residency is a product promise (ADR-0007)."
  type        = string
  default     = "europe-west4"
}

variable "org_id" {
  description = "Optional GCP organization ID. Leave null for org-less (personal) projects."
  type        = string
  default     = null
}

variable "folder_id" {
  description = "Optional folder ID (mutually exclusive with org_id)."
  type        = string
  default     = null
}

# No default: a committed address is the one every clone of this
# repository sends its billing alerts to.
variable "support_email" {
  description = "Where budget alerts land."
  type        = string
}

variable "github_repo" {
  description = "GitHub repository (owner/name) allowed to assume the deploy service accounts via Workload Identity Federation."
  type        = string
  default     = "TryEarful/earful"
}

variable "create_budgets" {
  description = "Billing budgets need billing-account IAM the applying identity may lack; set false to skip them and configure budgets manually (PLAN.md M1-T5 thresholds)."
  type        = bool
  default     = true
}

# The ESP's sending-domain records (verification code, DKIM selectors,
# DMARC), as its domain-authentication API returns them.
#
# REQUIRED rather than defaulted, for the same reason a default was once
# tempting: these records are what make outgoing mail authenticate, and
# a tfvars file recreated without them computes an empty map and DELETES
# them — silently, because destroying a DNS record raises nothing. A
# required variable turns that into a plan-time error instead, and keeps
# one deployment's records out of a repository other people clone.
#
# Two shapes worth knowing, because neither matches the vendor's own doc
# example: authentication returns TWO DKIM CNAMEs, and they sit under
# the sending subdomain rather than the apex, since that is the domain
# the ESP signs with. SPF is no longer prescribed — aligned DKIM carries
# DMARC by itself — but adding it gives a second aligned path when the
# envelope-from is your own domain.
variable "mail_dns_records" {
  description = "ESP sending-domain records (verification, DKIM, DMARC) for the mail subdomain."
  type = list(object({
    name    = string # relative, e.g. "mail" or "brevo1._domainkey.mail"
    type    = string # TXT, CNAME, ...
    ttl     = optional(number, 300)
    rrdatas = list(string)
  }))
}

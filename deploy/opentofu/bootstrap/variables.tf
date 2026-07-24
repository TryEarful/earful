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

variable "support_email" {
  description = "Where budget alerts land."
  type        = string
  default     = "support@tryearful.com"
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

variable "mail_dns_records" {
  description = "Brevo sending-domain records for mail.tryearful.com (verification code, DKIM, DMARC), from Brevo's domain-authentication API. Committed as the default — they are public DNS data, and a tfvars-only copy would be silently dropped (records deleted) if the gitignored tfvars were ever recreated from the README procedure."
  type = list(object({
    name    = string # relative, e.g. "mail" or "brevo._domainkey.mail"
    type    = string # TXT, CNAME, ...
    ttl     = optional(number, 300)
    rrdatas = list(string)
  }))
  default = [
    {
      name    = "mail"
      type    = "TXT"
      rrdatas = ["\"brevo-code:aaf0cf4968fce229262c911f75f66d5f\""]
    },
    {
      # Brevo signs DKIM with d=mail.tryearful.com, so verifiers resolve
      # <selector>._domainkey under the subdomain — not the apex.
      name    = "brevo1._domainkey.mail"
      type    = "CNAME"
      rrdatas = ["b1.mail-tryearful-com.dkim.brevo.com."]
    },
    {
      name    = "brevo2._domainkey.mail"
      type    = "CNAME"
      rrdatas = ["b2.mail-tryearful-com.dkim.brevo.com."]
    },
    {
      name    = "_dmarc.mail"
      type    = "TXT"
      rrdatas = ["\"v=DMARC1; p=none; rua=mailto:rua@dmarc.brevo.com\""]
    },
  ]
}

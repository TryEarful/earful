# PLAN.md's budget rule: €100/mo combined operating target with alerts at
# €50/€80/€100, and €200 as the hard cap. GCP cannot enforce a true cap —
# the €200 budget's alert plus the runbook's "budget kill" procedure
# (docs/runbook.md) is the cap's implementation.
#
# Budgets are billing-account-scoped: only the human operator holds
# billing IAM, which is why they live in bootstrap and not the env stacks.
# If the applying identity lacks billing.budgets.* permissions, set
# create_budgets=false and configure the same numbers by hand.

resource "google_monitoring_notification_channel" "budget_email" {
  count    = var.create_budgets ? 1 : 0
  provider = google.quota_ops

  project      = google_project.ops.project_id
  display_name = "Earful budget alerts"
  type         = "email"

  labels = {
    email_address = local.support_email
  }

  depends_on = [google_project_service.apis]
}

locals {
  budget_project_numbers = [
    "projects/${google_project.ops.number}",
    "projects/${google_project.stg.number}",
    "projects/${google_project.pro.number}",
    "projects/${google_project.backups.number}",
  ]
}

resource "google_billing_budget" "operating_target" {
  count    = var.create_budgets ? 1 : 0
  provider = google.quota_ops

  billing_account = var.billing_account
  display_name    = "Earful operating target (€100)"

  depends_on = [google_project_service.apis]

  budget_filter {
    projects = local.budget_project_numbers
  }

  amount {
    specified_amount {
      currency_code = "EUR"
      units         = "100"
    }
  }

  threshold_rules {
    threshold_percent = 0.5 # €50
  }
  threshold_rules {
    threshold_percent = 0.8 # €80
  }
  threshold_rules {
    threshold_percent = 1.0 # €100
  }

  all_updates_rule {
    monitoring_notification_channels = [google_monitoring_notification_channel.budget_email[0].id]
    disable_default_iam_recipients   = false
  }
}

resource "google_billing_budget" "hard_cap" {
  count    = var.create_budgets ? 1 : 0
  provider = google.quota_ops

  billing_account = var.billing_account
  display_name    = "Earful HARD CAP (€200) — runbook: budget kill"

  depends_on = [google_project_service.apis]

  budget_filter {
    projects = local.budget_project_numbers
  }

  amount {
    specified_amount {
      currency_code = "EUR"
      units         = "200"
    }
  }

  threshold_rules {
    threshold_percent = 1.0
  }

  all_updates_rule {
    monitoring_notification_channels = [google_monitoring_notification_channel.budget_email[0].id]
    disable_default_iam_recipients   = false
  }
}

# M9-T6 / ADR-0008: immutable rolling DB exports — the backup an attacker
# with app-project admin still can't destroy.
#
#   Cloud Scheduler (pro, daily) ──OAuth──▶ Cloud Workflows (pro)
#     └─ computes a dated object name (Scheduler can't) and calls the
#        SQL Admin export API as earful-backup@pro (export+get only,
#        NO database credentials anywhere in this path)
#          └─▶ Cloud SQL writes gs://earful-backups-…/earful-pro-DATE.sql.gz
#
# The bucket lives in the separate backups project. Its 30-day retention
# policy — once var.lock_retention flips to true, which the runbook does
# right after the first successful export+restore drill — is LOCKED:
# irreversible, and no credential in the system (project owner included)
# can delete a younger-than-30d object. The lifecycle rule deletes
# objects at 31 days, so the rolling window manages itself. The Cloud SQL
# service agent that writes exports holds a custom create-only role: it
# cannot read, list, overwrite, or delete anything.
#
# No versioning: GCS refuses versioning+retention together (and retention
# is the guarantee required). stg is deliberately not exported (cost; its
# data is disposable).

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
    google-beta = {
      source = "hashicorp/google-beta"
    }
  }
}

resource "google_storage_bucket" "exports" {
  project = var.backups_project
  name    = "${var.backups_project}-sql-exports"
  # EU multi-region: survives a regional outage, stays inside the EU
  # promise, costs cents at this size.
  location = "EU"

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  retention_policy {
    retention_period = 2592000 # 30 days
    is_locked        = var.lock_retention
  }

  lifecycle_rule {
    condition {
      age = 31
    }
    action {
      type = "Delete"
    }
  }
}

# --- write path: the INSTANCE's own service account ---
#
# Two constraints, both established by the export API's behaviour rather
# than by its documentation. First, the grant target: each Cloud
# SQL instance has its own service account (serviceAccountEmailAddress);
# binding the project-level Cloud SQL service agent instead yields the
# export API's misleading 412 "does not have the required permissions
# for the bucket". Second, a deliberate deviation from ADR-0008's
# create-only role: the export API documents objectAdmin, and narrower
# grants were refused. This does NOT weaken the guarantee — once
# lock_retention flips, GCS refuses deletes and overwrites of young
# objects regardless of IAM; the LOCKED retention policy is the actual
# immutability mechanism. The owner-cannot-delete AC is unchanged.
resource "google_storage_bucket_iam_member" "sql_instance_writes" {
  bucket = google_storage_bucket.exports.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${var.sql_instance_sa_email}"
}

# --- trigger path: scheduler → workflow → export API ---

resource "google_service_account" "backup" {
  project      = var.pro_project
  account_id   = "earful-backup"
  display_name = "Daily SQL export trigger"
}

resource "google_project_iam_custom_role" "sql_exporter" {
  project     = var.pro_project
  role_id     = "earfulSqlExporter"
  title       = "Earful SQL exporter"
  description = "May trigger instance exports and read instance metadata; may not read data, connect, or administer."
  permissions = [
    "cloudsql.instances.export",
    "cloudsql.instances.get",
  ]
}

resource "google_project_iam_member" "backup_exports" {
  project = var.pro_project
  role    = google_project_iam_custom_role.sql_exporter.id
  member  = "serviceAccount:${google_service_account.backup.email}"
}

# Scheduler's OAuth target requires the SA it runs as to be able to
# invoke workflows in the project.
resource "google_project_iam_member" "backup_invokes_workflows" {
  project = var.pro_project
  role    = "roles/workflows.invoker"
  member  = "serviceAccount:${google_service_account.backup.email}"
}

resource "google_workflows_workflow" "export" {
  project         = var.pro_project
  region          = var.region
  name            = "earful-sql-export"
  service_account = google_service_account.backup.email

  # Workflows exists solely because Scheduler bodies are static: something
  # has to stamp today's date into the object name (create-only + retention
  # means yesterday's name can never be reused).
  source_contents = <<-EOT
    main:
      steps:
        - export:
            call: http.post
            args:
              url: https://sqladmin.googleapis.com/v1/projects/${var.pro_project}/instances/${var.sql_instance_name}/export
              auth:
                type: OAuth2
              body:
                exportContext:
                  fileType: SQL
                  databases: ["earful"]
                  uri: $${"gs://${google_storage_bucket.exports.name}/earful-pro-" + text.substring(time.format(sys.now()), 0, 10) + ".sql.gz"}
            result: operation
        - done:
            return: $${operation.body.name}
  EOT
}

resource "google_cloud_scheduler_job" "daily_export" {
  project   = var.pro_project
  region    = var.region
  name      = "earful-daily-sql-export"
  schedule  = "17 3 * * *" # 03:17 UTC, off the :00 stampede, after the 02:00 Cloud SQL backup
  time_zone = "UTC"

  http_target {
    http_method = "POST"
    uri         = "https://workflowexecutions.googleapis.com/v1/${google_workflows_workflow.export.id}/executions"

    oauth_token {
      service_account_email = google_service_account.backup.email
    }
  }
}

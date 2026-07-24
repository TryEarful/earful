# Cloud SQL Postgres 16, sized per ADR-0007's budget stance (zonal,
# shared-core where possible, outside the SLA and fine with it).
#
# Connectivity: PUBLIC IP with ZERO authorized networks. That is not an
# exposure — with no authorized networks, the only way in is the Cloud
# SQL connector path (Cloud Run's built-in /cloudsql unix socket or the
# auth proxy), which requires cloudsql.client IAM. Private-IP-only would
# force a paid VPC connector for no security gain at this size.

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
    random = {
      source = "hashicorp/random"
    }
  }
}

resource "google_sql_database_instance" "main" {
  project          = var.project
  region           = var.region
  name             = var.instance_name
  database_version = "POSTGRES_16"

  # Belt (API-level) and suspenders (tofu-level) on pro; both off on stg.
  deletion_protection = var.deletion_protection

  settings {
    # Shared-core tiers only exist in the ENTERPRISE edition; the API's
    # default (ENTERPRISE_PLUS) rejects them.
    edition           = "ENTERPRISE"
    tier              = var.tier
    availability_type = "ZONAL"
    disk_type         = "PD_SSD"
    disk_size         = 10
    disk_autoresize   = true
    # Hard ceiling on autogrowth: without it, a storage-abuse flood (or a
    # runaway write) could grow the disk — and the bill — without bound.
    # 50 GB is far above this workload's real needs at MVP scale; the
    # >80% disk alert fires long before it, and the purge job reclaims.
    disk_autoresize_limit = 50

    deletion_protection_enabled = var.deletion_protection

    ip_configuration {
      ipv4_enabled = true
      # No authorized_networks blocks — connector/IAM path only.
    }

    backup_configuration {
      enabled                        = true
      start_time                     = "02:00"
      point_in_time_recovery_enabled = var.pitr_enabled
      transaction_log_retention_days = var.pitr_enabled ? 7 : null

      backup_retention_settings {
        retained_backups = var.retained_backups
      }
    }
  }
}

resource "google_sql_database" "app" {
  project  = var.project
  instance = google_sql_database_instance.main.name
  name     = "earful"
}

# special=false keeps the password URL-safe, so the DSN below needs no
# encoding dance; 32 alphanumerics is ~190 bits.
resource "random_password" "db" {
  length  = 32
  special = false
}

resource "google_sql_user" "app" {
  project  = var.project
  instance = google_sql_database_instance.main.name
  name     = "earful"
  password = random_password.db.result
}

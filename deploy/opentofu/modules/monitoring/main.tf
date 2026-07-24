# Per-environment observability (M1-T4 + M9-T2): a public uptime check on
# /healthz (which pings the DB, so "up" means actually-serving), the
# alert-policy set, and the two log-based metrics the AI alerts count.
# Every policy notifies the same email channel; test-fire procedures per
# policy live in docs/runbook.md.

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

resource "google_monitoring_notification_channel" "email" {
  project      = var.project
  display_name = "Earful alerts (${var.env_name})"
  type         = "email"

  labels = {
    email_address = var.alert_email
  }
}

resource "google_monitoring_uptime_check_config" "healthz" {
  project      = var.project
  display_name = "earful /healthz (${var.env_name})"
  timeout      = "10s"
  period       = "60s"

  http_check {
    # /health, not /healthz: the GFE swallows /healthz on public Cloud
    # Run URLs (Google-branded 404); the app serves the same DB-ping
    # handler on both paths.
    path         = "/health"
    port         = 443
    use_ssl      = true
    validate_ssl = true
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project
      host       = var.host
    }
  }

  # Host changes force replacement, and the uptime alert policy holds a
  # reference to the check id — create the successor first so the policy
  # can move over before the old check is deleted (the API refuses to
  # delete a check with policies still attached).
  lifecycle {
    create_before_destroy = true
  }
}

resource "google_monitoring_alert_policy" "uptime" {
  project      = var.project
  display_name = "Uptime: /healthz failing (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "healthz check failing"
    condition_threshold {
      filter          = "metric.type=\"monitoring.googleapis.com/uptime_check/check_passed\" AND metric.label.check_id=\"${google_monitoring_uptime_check_config.healthz.uptime_check_id}\" AND resource.type=\"uptime_url\""
      comparison      = "COMPARISON_GT"
      threshold_value = 1
      duration        = "300s"

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_NEXT_OLDER"
        cross_series_reducer = "REDUCE_COUNT_FALSE"
        group_by_fields      = ["resource.label.host"]
      }

      trigger {
        count = 1
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "The DB ping behind /healthz is failing from multiple regions. Runbook: docs/runbook.md → 'Service down'."
  }
}

resource "google_monitoring_alert_policy" "error_rate" {
  project      = var.project
  display_name = "5xx rate elevated (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "5xx responses"
    condition_threshold {
      filter          = "metric.type=\"run.googleapis.com/request_count\" AND resource.type=\"cloud_run_revision\" AND resource.label.service_name=\"${var.service_name}\" AND metric.label.response_code_class=\"5xx\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0.05 # ≈ 3 errors/min sustained
      duration        = "300s"

      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "Sustained server errors. Check recent deploy first — rollback procedure: docs/runbook.md → 'Rollback'."
  }
}

resource "google_monitoring_alert_policy" "latency_p95" {
  project      = var.project
  display_name = "p95 latency > 2s (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "p95 request latency"
    condition_threshold {
      filter          = "metric.type=\"run.googleapis.com/request_latencies\" AND resource.type=\"cloud_run_revision\" AND resource.label.service_name=\"${var.service_name}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 2000 # ms
      duration        = "600s"

      aggregations {
        alignment_period     = "300s"
        per_series_aligner   = "ALIGN_PERCENTILE_95"
        cross_series_reducer = "REDUCE_MEAN"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "Requests are slow. Usual suspects at this size: Cloud SQL CPU (see its alert), cold-start pile-up, or a slow query from a recent change."
  }
}

resource "google_monitoring_alert_policy" "sql_disk" {
  project      = var.project
  display_name = "Cloud SQL disk > 80% (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "disk utilization"
    condition_threshold {
      filter          = "metric.type=\"cloudsql.googleapis.com/database/disk/utilization\" AND resource.type=\"cloudsql_database\" AND resource.label.database_id=\"${var.project}:${var.sql_instance_name}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0.8
      duration        = "600s"

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_MEAN"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "Disk autoresize is on, so this firing means growth worth investigating (or autoresize hit a limit). Check abuse_log/revisions table sizes — the purge job (M8) trims them."
  }
}

resource "google_monitoring_alert_policy" "sql_cpu" {
  project      = var.project
  display_name = "Cloud SQL CPU > 90% (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "cpu utilization"
    condition_threshold {
      filter          = "metric.type=\"cloudsql.googleapis.com/database/cpu/utilization\" AND resource.type=\"cloudsql_database\" AND resource.label.database_id=\"${var.project}:${var.sql_instance_name}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0.9
      duration        = "900s"

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_MEAN"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "Sustained DB CPU saturation. On shared-core tiers this is also the 'time to size up' signal (ADR-0007 accepts that trade)."
  }
}

# --- AI cost-control alerts (M9-T2), counting the app's own log lines ---

# internal/ai/meter.go logs this exact Error line when the global € breaker
# trips; the string here must stay byte-identical to it.
resource "google_logging_metric" "breaker_tripped" {
  project = var.project
  name    = "ai_breaker_tripped"
  filter  = "resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"${var.service_name}\" AND jsonPayload.message=\"AI budget breaker tripped — all AI endpoints disabled until tomorrow\""
}

resource "google_monitoring_alert_policy" "breaker_tripped" {
  project      = var.project
  display_name = "AI budget breaker TRIPPED (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "breaker log line seen"
    condition_threshold {
      filter          = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.breaker_tripped.name}\" AND resource.type=\"cloud_run_revision\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0
      duration        = "0s"

      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_SUM"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "The global daily € AI budget is spent; every AI endpoint now refuses until the UTC day rolls over. Runbook: docs/runbook.md → 'AI breaker trip'."
  }
}

# internal/ai/meter.go logs one "ai usage recorded" Info line per AI call
# (kind + counts only). A silent day is normal; hundreds an hour is not.
resource "google_logging_metric" "ai_usage" {
  project = var.project
  name    = "ai_usage_recorded"
  filter  = "resource.type=\"cloud_run_revision\" AND resource.labels.service_name=\"${var.service_name}\" AND jsonPayload.message=\"ai usage recorded\""
}

resource "google_monitoring_alert_policy" "ai_usage_anomaly" {
  project      = var.project
  display_name = "AI usage anomaly: >500 calls/hour (${var.env_name})"
  combiner     = "OR"

  conditions {
    display_name = "ai calls per hour"
    condition_threshold {
      filter          = "metric.type=\"logging.googleapis.com/user/${google_logging_metric.ai_usage.name}\" AND resource.type=\"cloud_run_revision\""
      comparison      = "COMPARISON_GT"
      threshold_value = 500
      duration        = "0s"

      aggregations {
        alignment_period   = "3600s"
        per_series_aligner = "ALIGN_SUM"
      }
    }
  }

  notification_channels = [google_monitoring_notification_channel.email.id]

  documentation {
    content = "AI call volume is far above organic use — likely abuse burning quota ahead of the breaker. Runbook: docs/runbook.md → 'AI breaker trip' (same levers)."
  }
}

# The Cloud Run v2 service + the earful-migrate job (same image, args
# ["migrate"]). Tofu owns everything about them EXCEPT the image: after
# the seed deploy, .github/workflows/deploy.yml updates images via
# gcloud, so both resources ignore image (and the client bookkeeping
# gcloud writes) — a later `tofu apply` won't roll the app back to :seed.
#
# max_instance_count = 1 is deliberate twofold: the per-IP rate limiters
# are in-memory (a second instance would halve them), and one instance is
# plenty inside the €100/mo envelope. Revisit alongside a shared-state
# limiter if the product outgrows it.

terraform {
  required_providers {
    google = {
      source = "hashicorp/google"
    }
  }
}

resource "google_cloud_run_v2_service" "app" {
  project  = var.project
  location = var.region
  name     = var.service_name
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    service_account       = var.service_account_email
    execution_environment = "EXECUTION_ENVIRONMENT_GEN1"

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [var.sql_connection_name]
      }
    }

    containers {
      image = var.image

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        cpu_idle          = true
        startup_cpu_boost = true
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      dynamic "env" {
        for_each = var.env
        content {
          name  = env.key
          value = env.value
        }
      }

      dynamic "env" {
        for_each = var.secret_env
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = env.value
              version = "latest"
            }
          }
        }
      }

      # /healthz pings the DB, so a revision only takes traffic once it
      # can actually serve. No liveness probe on purpose: a transient DB
      # blip should surface as 503s + an uptime alert, not as Cloud Run
      # restart-looping a healthy process.
      startup_probe {
        http_get {
          path = "/healthz"
        }
        period_seconds    = 3
        timeout_seconds   = 3
        failure_threshold = 10
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].containers[0].image,
      template[0].revision,
      template[0].labels,
      template[0].annotations,
      client,
      client_version,
      labels,
    ]
  }
}

resource "google_cloud_run_v2_service_iam_member" "public" {
  project  = var.project
  location = var.region
  name     = google_cloud_run_v2_service.app.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

resource "google_cloud_run_v2_job" "migrate" {
  project  = var.project
  location = var.region
  name     = "${var.service_name}-migrate"

  template {
    template {
      service_account = var.service_account_email
      max_retries     = 1

      volumes {
        name = "cloudsql"
        cloud_sql_instance {
          instances = [var.sql_connection_name]
        }
      }

      containers {
        # Image ENTRYPOINT is /earful; args swap the default CMD "serve"
        # for "migrate" (embedded goose behind the pg advisory lock —
        # idempotent, concurrency-safe by design).
        image = var.image
        args  = ["migrate"]

        volume_mounts {
          name       = "cloudsql"
          mount_path = "/cloudsql"
        }

        dynamic "env" {
          for_each = var.env
          content {
            name  = env.key
            value = env.value
          }
        }

        dynamic "env" {
          for_each = var.secret_env
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = env.value
                version = "latest"
              }
            }
          }
        }
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].template[0].containers[0].image,
      template[0].labels,
      template[0].annotations,
      client,
      client_version,
      labels,
    ]
  }
}

# M8-T2: retention, on a schedule.
#
# The same image and the same binary as the service — `earful purge` —
# so what runs nightly in production is exactly what a developer runs
# with `make purge`. Retention promises that depend on someone
# remembering to run something are not promises.
resource "google_cloud_run_v2_job" "purge" {
  count    = var.enable_purge ? 1 : 0
  project  = var.project
  location = var.region
  name     = "${var.service_name}-purge"

  template {
    template {
      service_account = var.service_account_email
      # A purge that fails is retried once; a purge that fails twice
      # should page a human rather than thrash the database.
      max_retries = 1
      timeout     = "900s"

      volumes {
        name = "cloudsql"
        cloud_sql_instance {
          instances = [var.sql_connection_name]
        }
      }

      containers {
        image = var.image
        args  = ["purge"]

        volume_mounts {
          name       = "cloudsql"
          mount_path = "/cloudsql"
        }

        dynamic "env" {
          for_each = var.env
          content {
            name  = env.key
            value = env.value
          }
        }

        dynamic "env" {
          for_each = var.secret_env
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = env.value
                version = "latest"
              }
            }
          }
        }
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].template[0].containers[0].image,
      template[0].labels,
      template[0].annotations,
      client,
      client_version,
      labels,
    ]
  }
}

# 04:07 UTC: after the nightly database export (03:17) so a backup exists
# of the data about to be erased, and off the top of the hour.
resource "google_cloud_scheduler_job" "purge" {
  count     = var.enable_purge ? 1 : 0
  project   = var.project
  region    = var.region
  name      = "${var.service_name}-purge"
  schedule  = "7 4 * * *"
  time_zone = "UTC"

  http_target {
    http_method = "POST"
    uri         = "https://${var.region}-run.googleapis.com/apis/run.googleapis.com/v1/namespaces/${var.project}/jobs/${google_cloud_run_v2_job.purge[0].name}:run"

    oauth_token {
      service_account_email = var.scheduler_service_account_email
    }
  }
}

# The scheduler's identity may start this job and nothing else.
resource "google_cloud_run_v2_job_iam_member" "purge_invoker" {
  count    = var.enable_purge ? 1 : 0
  project  = var.project
  location = var.region
  name     = google_cloud_run_v2_job.purge[0].name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${var.scheduler_service_account_email}"
}

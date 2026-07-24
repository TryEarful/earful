# One shared Docker repository: staging deploys push here, production
# promotes the exact digest staging's smoke gate approved — same bytes,
# no rebuild. Cleanup keeps the last 10 images (rollback window) and
# drops anything older than 30 days beyond those.
resource "google_artifact_registry_repository" "earful" {
  project       = google_project.ops.project_id
  location      = var.region
  repository_id = "earful"
  format        = "DOCKER"

  cleanup_policy_dry_run = false

  cleanup_policies {
    id     = "keep-recent"
    action = "KEEP"
    most_recent_versions {
      keep_count = 10
    }
  }

  cleanup_policies {
    id     = "delete-old"
    action = "DELETE"
    condition {
      older_than = "2592000s" # 30 days
    }
  }

  depends_on = [google_project_service.apis]
}
